package auth

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// RequirePlatformAdmin extracts the auth context and enforces the
// platform-admin flag (users.admin, the Speakeasy-staff marker). The returned
// logger is pre-tagged with the actor for audit/error lines. Platform-tier
// resources have no project or organization to scope an RBAC grant to, so
// handlers curating them gate inline on this flag; route every such handler
// through this function so none can accidentally skip the gate.
func RequirePlatformAdmin(ctx context.Context, logger *slog.Logger) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, logger, oops.C(oops.CodeUnauthorized)
	}

	logger = logger.With(attr.SlogUserID(authCtx.UserID))

	if !authCtx.IsAdmin {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "platform admin required").LogError(ctx, logger)
	}

	return authCtx, logger, nil
}
