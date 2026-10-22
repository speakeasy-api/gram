package authz

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// CheckSensitiveContentAccess denies sensitive customer content to impersonated
// sessions, independently of their RBAC grants. The demo organization contains
// fabricated content and retains access; callers must still enforce normal RBAC.
func CheckSensitiveContentAccess(ctx context.Context) error {
	if !contextvalues.IsSupportSession(ctx) && !contextvalues.IsLegacyImpersonatedSession(ctx) {
		return nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if ok && authCtx != nil && authCtx.ActiveOrganizationID == constants.DemoOrganizationID {
		return nil
	}
	return oops.E(oops.CodeForbidden, nil, "sensitive content cannot be accessed while impersonating an organization")
}
