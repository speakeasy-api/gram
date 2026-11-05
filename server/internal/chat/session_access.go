package chat

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SessionAccess holds the per-chat gate so services that reveal a session's
// content under another name, such as a risk finding's matched value, answer
// the ownership question exactly as the chat service does.
type SessionAccess struct {
	logger *slog.Logger
	repo   *repo.Queries
	authz  *authz.Engine
}

func NewSessionAccess(logger *slog.Logger, db repo.DBTX, authzEngine *authz.Engine) *SessionAccess {
	return &SessionAccess{
		logger: logger,
		repo:   repo.New(db),
		authz:  authzEngine,
	}
}

// AnswersForSession reports whether the caller may read the session with no
// chat:read grant: they own it, or own or edit the agent behind an agent-only
// session. It records no denial and, like the chat gate, admits the runtime.
func (a *SessionAccess) AnswersForSession(ctx context.Context, chatID uuid.UUID) (bool, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return false, nil
	}
	// Impersonation bypasses RBAC, so the owner rule answers nothing for a
	// support or legacy-impersonated session, as loadAuthorizedChat refuses.
	if authz.CheckSensitiveContentAccess(ctx) != nil {
		return false, nil
	}

	chat, err := a.repo.GetChat(ctx, repo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, oops.E(oops.CodeUnexpected, err, "failed to load chat").LogError(ctx, a.logger)
	}

	noGrant := false
	err = a.authorizeChatAccess(ctx, authCtx, chat, chatAccessRead, &noGrant)
	var shared *oops.ShareableError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &shared) && shared.Code == oops.CodeUnexpected:
		return false, err
	default:
		return false, nil
	}
}
