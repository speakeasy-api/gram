package feature

import "context"

// SSOSignInEnforced reports whether FlagSSOSignInEnforcement is on for an
// organization, evaluated with the organization id as distinct id and its
// slug as the organization group. It fails closed to today's behaviour.
func SSOSignInEnforced(ctx context.Context, flags Provider, organizationID, organizationSlug string) bool {
	if flags == nil || organizationID == "" {
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, FlagSSOSignInEnforcement, organizationID, OrgProjectGroups(organizationSlug, ""), nil)
	return err == nil && enabled
}
