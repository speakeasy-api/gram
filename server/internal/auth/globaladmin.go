package auth

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// RequireGlobalAdmin authorizes global operations using either a standalone admin
// context verified by the admin transport or the legacy dashboard platform-admin
// entitlement. It returns audit metadata, not a tenant credential.
func RequireGlobalAdmin(ctx context.Context, logger *slog.Logger) (string, *slog.Logger, error) {
	if a, ok := contextvalues.GetAdminAuthContext(ctx); ok {
		if a == nil || a.SessionID == "" || a.OIDCSubject == "" {
			return "", logger, oops.C(oops.CodeUnauthorized)
		}
		return a.Email, logger.With(attr.SlogAdminOIDCSubject(a.OIDCSubject), attr.SlogAuthSource("gram_admin")), nil
	}
	a, logger, err := RequirePlatformAdmin(ctx, logger)
	if err != nil {
		return "", logger, err
	}
	return conv.PtrValOrEmpty(a.Email, ""), logger, nil
}
