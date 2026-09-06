package auth

import (
	"context"
	"log/slog"
	"strings"

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
	authCtx, logger, err := platformAdminContext(ctx, logger)
	if err != nil {
		return nil, logger, err
	}
	if !authCtx.IsAdmin {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "platform admin required").LogError(ctx, logger)
	}
	return authCtx, logger, nil
}

// PlatformAdminEntitlementReader checks the current durable users.admin value.
type PlatformAdminEntitlementReader interface {
	IsPlatformAdmin(context.Context, string) (bool, error)
}

// RequireFreshPlatformAdminSession authorizes incident-sensitive platform paths.
// It accepts only an ordinary validated Gram session, rejects impersonation and
// alternate credentials, and re-reads the durable platform-admin entitlement.
func RequireFreshPlatformAdminSession(ctx context.Context, logger *slog.Logger, reader PlatformAdminEntitlementReader) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, logger, err := platformAdminContext(ctx, logger)
	if err != nil {
		return nil, logger, err
	}
	if !contextvalues.HasValidatedGramSession(ctx) || authCtx.SessionID == nil || *authCtx.SessionID == "" || authCtx.UserID == "" || authCtx.Email == nil || strings.TrimSpace(*authCtx.Email) == "" {
		return nil, logger, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.APIKeyID != "" || authCtx.APIKeyName != "" || len(authCtx.APIKeyScopes) != 0 || authCtx.OrgWidePluginHooksKey {
		return nil, logger, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return nil, logger, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetOAuthClientID(ctx); ok {
		return nil, logger, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetActingSurface(ctx); ok {
		return nil, logger, oops.C(oops.CodeForbidden)
	}
	if contextvalues.IsSupportSession(ctx) || contextvalues.IsLegacyImpersonatedSession(ctx) {
		return nil, logger, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetRBACScopeOverride(ctx); ok {
		return nil, logger, oops.C(oops.CodeForbidden)
	}
	if reader == nil {
		return nil, logger, oops.E(oops.CodeUnavailable, nil, "service is temporarily unavailable")
	}
	isAdmin, err := reader.IsPlatformAdmin(ctx, authCtx.UserID)
	if err != nil {
		return nil, logger, oops.E(oops.CodeUnavailable, err, "service is temporarily unavailable")
	}
	if !isAdmin {
		return nil, logger, oops.C(oops.CodeForbidden)
	}
	return authCtx, logger, nil
}

func platformAdminContext(ctx context.Context, logger *slog.Logger) (*contextvalues.AuthContext, *slog.Logger, error) {
	if logger == nil {
		return nil, nil, oops.E(oops.CodeUnavailable, nil, "platform authorization logger is unavailable")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, logger, oops.C(oops.CodeUnauthorized)
	}
	return authCtx, logger.With(attr.SlogUserID(authCtx.UserID)), nil
}
