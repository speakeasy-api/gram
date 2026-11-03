package threadcontext

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Resolve selects a live thread in the caller's project.
// Assistant access uses the same resource-aware read check as sending an
// assistant message. The caller's identity and grants remain authoritative;
// selecting a thread does not authenticate as its assistant or creator.
func Resolve(ctx context.Context, db *pgxpool.Pool, authorizer *authz.Engine, threadID uuid.UUID) (contextvalues.AssistantPrincipal, string, error) {
	var empty contextvalues.AssistantPrincipal
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return empty, "", oops.C(oops.CodeUnauthorized)
	}
	if threadID == uuid.Nil {
		return empty, "", oops.E(oops.CodeBadRequest, nil, "thread_id must be a non-zero UUID")
	}
	if db == nil || authorizer == nil {
		return empty, "", oops.E(oops.CodeUnexpected, nil, "explicit thread selection is not configured")
	}
	thread, err := assistantrepo.New(db).LoadAssistantThreadForBootstrap(ctx, assistantrepo.LoadAssistantThreadForBootstrapParams{ThreadID: threadID, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, "", oops.E(oops.CodeNotFound, nil, "assistant thread not found")
	}
	if err != nil {
		return empty, "", oops.E(oops.CodeUnexpected, err, "load assistant thread")
	}
	if thread.OrganizationID != authCtx.ActiveOrganizationID {
		return empty, "", oops.E(oops.CodeNotFound, nil, "assistant thread not found")
	}
	if err := authorizer.Require(ctx, authz.AssistantCheck(authz.ScopeAssistantRead, thread.AssistantID.String(), authCtx.ProjectID.String())); err != nil {
		return empty, "", err
	}
	return contextvalues.AssistantPrincipal{AssistantID: thread.AssistantID, ThreadID: thread.ID}, thread.ChatID.String(), nil
}
