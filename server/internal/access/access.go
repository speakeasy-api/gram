package access

import "context"

// Check describes a single scope/resource authorization requirement.
type Check struct {
	Scope      Scope
	ResourceID string
}

// Require enforces that every check is satisfied by the grants in context.
func Require(ctx context.Context, checks ...Check) error {
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
