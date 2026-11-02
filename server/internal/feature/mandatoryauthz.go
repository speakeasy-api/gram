package feature

import "context"

// MandatoryAuthzShadow reports whether the organization shadows the mandatory
// server and auth client check. Any evaluation failure keeps it off.
func MandatoryAuthzShadow(ctx context.Context, flags Provider, organizationID string) bool {
	if flags == nil || organizationID == "" {
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, FlagMandatoryAuthzShadow, organizationID, nil, nil)
	return err == nil && enabled
}
