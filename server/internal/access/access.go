package access

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// Check describes a single scope/resource authorization requirement.
type Check struct {
	Scope      Scope
	ResourceID string
}

// Require enforces that every check is satisfied by the grants in context.
func Require(ctx context.Context, checks ...Check) error {
	if !isEnterpriseOrg(ctx) || isAPIKeyAuth(ctx) {
		return nil
	}

	if len(checks) == 0 {
		return ErrNoChecks
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {

		return ErrMissingGrants
	}

	for _, check := range checks {
		if err := validateCheck(check); err != nil {
			return err
		}

		if !grants.hasAccess(check.Scope, check.ResourceID) {
			return Denied(check.Scope, check.ResourceID)
		}
	}

	return nil
}

// RequireAny enforces that at least one check is satisfied by the grants in context.
func RequireAny(ctx context.Context, checks ...Check) error {
	if !isEnterpriseOrg(ctx) || isAPIKeyAuth(ctx) {
		return nil
	}

	if len(checks) == 0 {
		return ErrNoChecks
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {

		return ErrMissingGrants
	}

	for _, check := range checks {
		if err := validateCheck(check); err != nil {
			return err
		}
	}

	for _, check := range checks {
		if grants.hasAccess(check.Scope, check.ResourceID) {
			return nil
		}
	}

	return Denied(checks[0].Scope, checks[0].ResourceID)
}

// Filter returns the subset of candidate resource IDs allowed for the scope.
func Filter(ctx context.Context, scope Scope, resourceIDs []string) ([]string, error) {
	if !isEnterpriseOrg(ctx) || isAPIKeyAuth(ctx) {
		return resourceIDs, nil
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {

		return nil, ErrMissingGrants
	}

	allowed := make([]string, 0, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		if err := validateCheck(Check{Scope: scope, ResourceID: resourceID}); err != nil {
			return nil, err
		}

		if grants.hasAccess(scope, resourceID) {
			allowed = append(allowed, resourceID)
		}
	}

	return allowed, nil
}

// isEnterpriseOrg reports whether the request context belongs to an enterprise
// account, for which RBAC is enforced.
func isEnterpriseOrg(ctx context.Context) bool {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	return ok && authCtx != nil && authCtx.AccountType == "enterprise"
}

// isAPIKeyAuth reports whether the request was authenticated via an API key.
// API keys have their own permissions model and are not subject to RBAC grants.
func isAPIKeyAuth(ctx context.Context) bool {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	return ok && authCtx != nil && authCtx.APIKeyID != ""
}

func validateCheck(check Check) error {
	switch check.ResourceID {
	case "":
		return InvalidCheck(check.Scope, check.ResourceID)
	case WildcardResource:
		return InvalidCheck(check.Scope, check.ResourceID)
	default:
		return nil
	}
}
