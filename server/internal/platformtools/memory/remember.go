package memory

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type rememberInput struct {
	Content string   `json:"content" jsonschema:"Memory content to persist for the current assistant. Max 8192 bytes."`
	Tags    []string `json:"tags,omitempty" jsonschema:"Optional tags applied to the memory for later filtered recall."`
}

type rememberOutput struct {
	ID           string  `json:"id"`
	CreatedAt    string  `json:"created_at"`
	Deduped      bool    `json:"deduped"`
	SupersededID *string `json:"superseded_id,omitempty"`
}

// RememberTool implements gram://memory/remember.
type RememberTool struct {
	svc        Service
	descriptor core.ToolDescriptor
}

func NewRememberTool(svc Service) *RememberTool {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := false

	return &RememberTool{
		svc: svc,
		descriptor: core.ToolDescriptor{
			SourceSlug:  platformtools.SourceMemory,
			HandlerName: handlerRemember,
			Name:        platformtools.ToolNameMemoryRemember,
			Description: "Persist a memory for the current assistant. Deduplicates against the nearest existing memory and may supersede a contradicting one. Requires assistant runtime auth.",
			InputSchema: core.BuildInputSchema[rememberInput](),
			Variables:   nil,
			Annotations: memoryToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
	}
}

func (t *RememberTool) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *RememberTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "memory tools require an assistant principal")
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "memory tools require a project auth context")
	}

	var input rememberInput
	if err := readPayload(payload, &input); err != nil {
		return err
	}

	result, err := t.svc.Remember(
		ctx,
		principal.AssistantID,
		*authCtx.ProjectID,
		authCtx.ActiveOrganizationID,
		input.Content,
		input.Tags,
	)
	if err != nil {
		return fmt.Errorf("remember memory: %w", err)
	}

	out := rememberOutput{
		ID:           result.ID.String(),
		CreatedAt:    result.CreatedAt.UTC().Format(time.RFC3339Nano),
		Deduped:      result.Deduped,
		SupersededID: nil,
	}
	if result.SupersededID != nil {
		s := result.SupersededID.String()
		out.SupersededID = &s
	}

	return writeJSON(wr, out)
}
