package access

import "context"

// Check describes a single scope/resource authorization requirement.
type Check struct {
	Scope      Scope
	ResourceID string
}

// Require enforces that every check is satisfied by the grants in context.
func Require(ctx context.Context, checks ...Check) error {
	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {
		return ErrMissingGrants
	}

	for _, check := range checks {
		if check.ResourceID == "" {
			return InvalidCheck(check.Scope)
		}

		if !grants.hasAccess(check.Scope, check.ResourceID) {
			return Denied(check.Scope, check.ResourceID)
		}
	}

	return nil
}

// Filter returns the subset of candidate resource IDs allowed for the scope.
// For example, if listTools returns [toolA, toolB, toolC] and the caller only
// has mcp:connect grants for toolA and toolB, Filter returns [toolA, toolB].
func Filter(ctx context.Context, scope Scope, resourceIDs []string) ([]string, error) {
	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {
		return nil, ErrMissingGrants
	}

	allowed := make([]string, 0, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		if resourceID == "" {
			return nil, InvalidCheck(scope)
		}

		if grants.hasAccess(scope, resourceID) {
			allowed = append(allowed, resourceID)
		}
	}

	return allowed, nil
}
