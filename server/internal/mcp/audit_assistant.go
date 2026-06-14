package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// assistantToolCallAudit captures everything needed to write the durable
// audit trail entry for an assistant-initiated tool call.
type assistantToolCallAudit struct {
	organizationID string
	projectID      uuid.UUID
	principal      contextvalues.AssistantPrincipal
	chatID         string
	toolsetSlug    string
	toolName       string
	toolURN        urn.Tool
	params         json.RawMessage
}

// recordAssistantToolCallAudit writes an audit log entry for a tool call made
// by an assistant runtime. It is invoked on dispatch — after tool resolution
// succeeds and before the tool executes — so the trail records the attempt
// regardless of the tool's outcome. The actor is the assistant's owning user
// (stamped on the auth context by the assistant token authorizer, matching
// the precedent set by assistant-initiated trigger mutations); the assistant
// itself is the subject. Tool calls run outside any database transaction, so
// the pool is used directly and a failed audit write is logged but never
// fails the tool call.
func recordAssistantToolCallAudit(
	ctx context.Context,
	logger *slog.Logger,
	auditLogger *audit.Logger,
	db *pgxpool.Pool,
	in assistantToolCallAudit,
) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" {
		logger.WarnContext(ctx, "skipping assistant tool call audit log: no auth context",
			attr.SlogToolName(in.toolName))
		return
	}

	err := auditLogger.LogAssistantToolCall(ctx, db, audit.LogAssistantToolCallEvent{
		OrganizationID:   in.organizationID,
		ProjectID:        in.projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		AssistantURN:     urn.NewAssistant(in.principal.AssistantID),
		Thread:           in.principal.ThreadID,
		Chat:             in.chatID,
		ToolsetSlug:      in.toolsetSlug,
		ToolName:         in.toolName,
		ToolURN:          in.toolURN,
		Params:           in.params,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to record assistant tool call audit log",
			attr.SlogError(err), attr.SlogToolName(in.toolName))
	}
}
