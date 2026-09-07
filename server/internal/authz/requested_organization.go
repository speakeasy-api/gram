package authz

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// RequireUserOrganizationScope resolves a user's principals and grants in the
// requested organization's namespace before evaluating an organization scope.
// It intentionally does not use grants prepared for the active organization.
func (e *Engine) RequireUserOrganizationScope(ctx context.Context, organizationID, userID string, scope Scope) error {
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		authCtx, _ := contextvalues.GetAuthContext(ctx)
		if authCtx.APIKeyID != "" {
			// API keys are bound to their authenticated organization outside RBAC.
			// This helper cannot authorize a requested organization for them.
			return oops.C(oops.CodeForbidden)
		}
		return nil
	}

	principals, err := ResolveUserPrincipals(ctx, e.db, organizationID, userID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve requested organization principals").LogError(ctx, e.logger, attr.SlogOrganizationID(organizationID), attr.SlogUserID(userID))
	}

	grants, err := LoadGrants(ctx, e.db, organizationID, principals)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load requested organization grants").LogError(ctx, e.logger, attr.SlogOrganizationID(organizationID), attr.SlogUserID(userID))
	}

	return e.EvaluateLoadedGrants(ctx, grants, Check{
		Scope:         scope,
		ResourceKind:  "",
		ResourceID:    organizationID,
		Dimensions:    nil,
		selectorMatch: selectorMatchNormal,
	})
}
