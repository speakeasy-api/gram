package memory

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	recallDefaultLimit = 8
	recallMinLimit     = 1
	recallMaxLimit     = 32
)

type recallInput struct {
	Query string   `json:"query" jsonschema:"Natural-language query used to find similar memories."`
	Limit *int     `json:"limit,omitempty" jsonschema:"Maximum number of memories to return. Defaults to 8; min 1; max 32."`
	Tags  []string `json:"tags,omitempty" jsonschema:"Restrict matches to memories carrying any of these tags."`
}

type recallEntry struct {
	ID        string   `json:"id"`
	Content   string   `json:"content"`
	Score     float64  `json:"score"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
	// Source is a compact provenance line for tracing where the memory was
	// written, e.g. "from slack user U123 (slack:T123:C456:789.012),
	// 2026-06-12". The parenthesised value is the origin thread's correlation
	// id, which encodes the source conversation uniformly across surfaces. It
	// records the conversational context of the write — not necessarily the
	// true origin of the fact, which the assistant may have picked up
	// elsewhere mid-turn. Absent when the memory has no recorded provenance.
	Source *string `json:"source,omitempty"`
}

// formatSource renders provenance as a compact attribution line: the source
// kind, the speaking user when one exists (slack/dashboard), the origin
// thread's correlation id in parentheses, and the write date. For automated
// surfaces: "from cron (cron:0d9e...), 2026-06-12".
func formatSource(r memory.RecallResult) *string {
	if r.SourceKind == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("from ")
	b.WriteString(*r.SourceKind)
	if r.SourceUserID != nil {
		b.WriteString(" user ")
		b.WriteString(*r.SourceUserID)
	}
	if r.SourceCorrelationID != nil {
		b.WriteString(" (")
		b.WriteString(*r.SourceCorrelationID)
		b.WriteString(")")
	}
	if r.SourceTimestamp != nil {
		b.WriteString(", ")
		b.WriteString(r.SourceTimestamp.UTC().Format(time.DateOnly))
	}
	s := b.String()
	return &s
}

// RecallTool implements gram://memory/recall.
type RecallTool struct {
	svc        Service
	descriptor core.ToolDescriptor
}

func NewRecallTool(svc Service) *RecallTool {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := false

	return &RecallTool{
		svc: svc,
		descriptor: core.ToolDescriptor{
			SourceSlug:  platformtools.SourceMemory,
			HandlerName: handlerRecall,
			Name:        platformtools.ToolNameMemoryRecall,
			Description: "Recall relevant memories for the current assistant. Returns scored, optionally tag-filtered matches sorted by relevance.",
			InputSchema: core.BuildInputSchema[recallInput](
				core.WithPropertyNumberRange("limit", float64(recallMinLimit), float64(recallMaxLimit)),
			),
			Variables:   nil,
			Annotations: memoryToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
	}
}

func (t *RecallTool) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *RecallTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "memory tools require an assistant principal")
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.E(oops.CodeUnauthorized, nil, "memory tools require a project auth context")
	}

	var input recallInput
	if err := readPayload(payload, &input); err != nil {
		return err
	}

	limit := recallDefaultLimit
	if input.Limit != nil {
		limit = *input.Limit
	}

	results, err := t.svc.Recall(
		ctx,
		principal.AssistantID,
		authCtx.ActiveOrganizationID,
		input.Query,
		limit,
		input.Tags,
	)
	if err != nil {
		return fmt.Errorf("recall memories: %w", err)
	}

	out := make([]recallEntry, 0, len(results))
	for _, r := range results {
		out = append(out, recallEntry{
			ID:        r.ID.String(),
			Content:   r.Content,
			Score:     r.Score,
			Tags:      r.Tags,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano),
			Source:    formatSource(r),
		})
	}

	return writeJSON(wr, out)
}
