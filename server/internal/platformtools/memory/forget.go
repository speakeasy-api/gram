package memory

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type forgetInput struct {
	Query string   `json:"query" jsonschema:"Natural-language description of the memory to forget."`
	Tags  []string `json:"tags,omitempty" jsonschema:"Restrict matches to memories carrying any of these tags."`
}

type forgetCandidate struct {
	ID         string  `json:"id"`
	Content    string  `json:"content"`
	Similarity float64 `json:"similarity"`
}

type forgetOutput struct {
	Forgotten  bool              `json:"forgotten"`
	ID         *string           `json:"id,omitempty"`
	Content    *string           `json:"content,omitempty"`
	Reason     string            `json:"reason"`
	Candidates []forgetCandidate `json:"candidates,omitempty"`
}

// ForgetTool implements gram://memory/forget.
type ForgetTool struct {
	svc        Service
	descriptor core.ToolDescriptor
}

func NewForgetTool(svc Service) *ForgetTool {
	readOnly := false
	destructive := true
	idempotent := false
	openWorld := false

	return &ForgetTool{
		svc: svc,
		descriptor: core.ToolDescriptor{
			SourceSlug:  platformtools.SourceMemory,
			HandlerName: handlerForget,
			Name:        platformtools.ToolNameMemoryForget,
			Description: "Forget a memory matching the given query for the current assistant. Returns no_match or ambiguous (with candidates) when the target cannot be uniquely identified.",
			InputSchema: core.BuildInputSchema[forgetInput](),
			Variables:   nil,
			Annotations: memoryToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
			Hidden:      true,
		},
	}
}

func (t *ForgetTool) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *ForgetTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "memory tools require an assistant principal")
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "memory tools require a project auth context")
	}

	var input forgetInput
	if err := readPayload(payload, &input); err != nil {
		return err
	}

	result, err := t.svc.Forget(
		ctx,
		principal.AssistantID,
		*authCtx.ProjectID,
		authCtx.ActiveOrganizationID,
		input.Query,
		input.Tags,
	)
	if err != nil {
		return fmt.Errorf("forget memory: %w", err)
	}

	out := forgetOutput{
		Forgotten:  result.Forgotten,
		ID:         nil,
		Content:    nil,
		Reason:     result.Reason,
		Candidates: nil,
	}
	if result.ID != nil {
		s := result.ID.String()
		out.ID = &s
	}
	if result.Content != nil {
		out.Content = result.Content
	}
	if len(result.Candidates) > 0 {
		out.Candidates = make([]forgetCandidate, 0, len(result.Candidates))
		for _, c := range result.Candidates {
			out.Candidates = append(out.Candidates, forgetCandidate{
				ID:         c.ID.String(),
				Content:    c.Content,
				Similarity: c.Similarity,
			})
		}
	}

	return writeJSON(wr, out)
}
