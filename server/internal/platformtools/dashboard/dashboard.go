// Package dashboard implements the platform_dashboard_send_message egress tool
// that delivers an assistant's reply to the Gram dashboard.
//
// Unlike Slack egress (an external API call), delivery here is a row in
// assistant_dashboard_messages — the sidebar's conversation log — which the
// browser polls. The tool resolves its target chat from the assistant
// principal's thread id, so the model never has to supply a destination.
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const handlerSendMessage = "send_message"

const roleAssistant = "assistant"

type sendMessageInput struct {
	Message string `json:"message" jsonschema:"The reply to show the user in the Gram dashboard, as Markdown."`
}

type sendMessageOutput struct {
	ID          string `json:"id"`
	DeliveredAt string `json:"delivered_at"`
}

// SendMessageTool implements platform_dashboard_send_message.
type SendMessageTool struct {
	db         *pgxpool.Pool
	descriptor core.ToolDescriptor
}

func NewSendMessageTool(db *pgxpool.Pool) *SendMessageTool {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := false

	return &SendMessageTool{
		db: db,
		descriptor: core.ToolDescriptor{
			SourceSlug:  platformtools.SourceDashboard,
			HandlerName: handlerSendMessage,
			Name:        platformtools.ToolNameDashboardSendMessage,
			Description: "Deliver your reply to the user in the Gram dashboard. Text responses are not shown to the user — call this tool to communicate. The reply is delivered to the current conversation automatically; you do not specify a recipient.",
			InputSchema: core.BuildInputSchema[sendMessageInput](),
			Variables:   nil,
			Annotations: toolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
	}
}

func (t *SendMessageTool) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *SendMessageTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "dashboard tools require an assistant principal")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "dashboard tools require a project auth context")
	}

	var input sendMessageInput
	if err := readPayload(payload, &input); err != nil {
		return err
	}
	if input.Message == "" {
		return oops.E(oops.CodeBadRequest, nil, "message is required")
	}

	q := assistantrepo.New(t.db)
	target, err := q.GetDashboardThreadTarget(ctx, assistantrepo.GetDashboardThreadTargetParams{
		ThreadID:  principal.ThreadID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("resolve dashboard thread target: %w", err)
	}

	// Stamp the reply with the conversation owner so reads scope to them — the
	// assistant replies on behalf of the user who owns the thread.
	row, err := q.InsertDashboardMessage(ctx, assistantrepo.InsertDashboardMessageParams{
		ProjectID: *authCtx.ProjectID,
		ChatID:    target.ChatID,
		UserID:    target.UserID,
		Role:      roleAssistant,
		Content:   input.Message,
	})
	if err != nil {
		return fmt.Errorf("insert dashboard message: %w", err)
	}

	return writeJSON(wr, sendMessageOutput{
		ID:          row.ID.String(),
		DeliveredAt: row.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
	})
}

func readPayload(payload io.Reader, target any) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func writeJSON(wr io.Writer, value any) error {
	if err := json.NewEncoder(wr).Encode(value); err != nil {
		return fmt.Errorf("encode response body: %w", err)
	}
	return nil
}

func toolAnnotations(readOnly, destructive, idempotent, openWorld bool) *types.ToolAnnotations {
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}
