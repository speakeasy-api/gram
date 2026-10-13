package feature

import "context"

// SSOSignInEnforced reports whether FlagSSOSignInEnforcement is on for an
// organization, evaluated with the organization id as the distinct id so
// every caller agrees. It fails closed to today's behaviour.
func SSOSignInEnforced(ctx context.Context, flags Provider, organizationID string) bool {
	if flags == nil || organizationID == "" {
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, FlagSSOSignInEnforcement, organizationID, nil, nil)
	return err == nil && enabled
}
