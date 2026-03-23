package access

import "context"

type Check struct {
	Scope      Scope
	ResourceID string
}

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
