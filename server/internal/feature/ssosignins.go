package feature

import "context"

// SSOSignInEnforcementDistinctID is the fleet-wide distinct id the SSO
// sign-in enforcement flag is evaluated under.
const SSOSignInEnforcementDistinctID = "speakeasy-fleet"

// SSOSignInEnforced reports whether FlagSSOSignInEnforcement is on. It fails
// closed to today's behaviour.
func SSOSignInEnforced(ctx context.Context, flags Provider) bool {
	if flags == nil {
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, FlagSSOSignInEnforcement, SSOSignInEnforcementDistinctID, nil, nil)
	return err == nil && enabled
}
