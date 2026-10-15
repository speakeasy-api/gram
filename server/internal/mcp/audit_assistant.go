package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
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
// regardless of the tool's outcome. A call made with a principal credential is
// attributed to the credential's agent or workload, with the user it acts for;
// any other call to the user stamped on the auth context by the assistant
// token authorizer.
// The assistant itself is the subject. Tool calls run outside any database transaction, so
// the pool is used directly and a failed audit write is logged but never
// fails the tool call.
func recordAssistantToolCallAudit(
	ctx context.Context,
	logger *slog.Logger,
	auditLogger *audit.Logger,
	db *pgxpool.Pool,
	in assistantToolCallAudit,
) {
	var actor urn.Principal
	var actorDisplayName *string
	var authorizer *urn.Principal
	if credential, ok := principalcredential.FromContext(ctx); ok {
		actor = credential.Credential.Principal
		if credential.Credential.AuthorizerUserID != "" {
			authorizer = new(urn.NewPrincipal(urn.PrincipalTypeUser, credential.Credential.AuthorizerUserID))
		}
	} else {
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil || authCtx.UserID == "" {
			logger.WarnContext(ctx, "skipping assistant tool call audit log: no auth context",
				attr.SlogToolName(in.toolName))
			return
		}
		actor = urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
		actorDisplayName = authCtx.Email
	}

	err := auditLogger.LogAssistantToolCall(ctx, db, audit.LogAssistantToolCallEvent{
		OrganizationID:   in.organizationID,
		ProjectID:        in.projectID,
		Actor:            actor,
		ActorDisplayName: actorDisplayName,
		ActorSlug:        nil,
		AssistantURN:     urn.NewAssistant(in.principal.AssistantID),
		Thread:           in.principal.ThreadID,
		Chat:             in.chatID,
		ToolsetSlug:      in.toolsetSlug,
		ToolName:         in.toolName,
		ToolURN:          in.toolURN,
		Params:           in.params,
		Authorizer:       authorizer,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to record assistant tool call audit log",
			attr.SlogError(err), attr.SlogToolName(in.toolName))
	}
}
