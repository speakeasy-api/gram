package feature

import "context"

// SignInEnforced reports whether FlagSignInEnforcement is on for an
// organization, evaluated with the organization id as the distinct id so
// every caller agrees. It fails closed to today's behaviour.
func SignInEnforced(ctx context.Context, flags Provider, organizationID string) bool {
	if flags == nil || organizationID == "" {
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, FlagSignInEnforcement, organizationID, nil, nil)
	return err == nil && enabled
}
