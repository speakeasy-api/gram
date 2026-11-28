package feature

import "context"

// SelfServeTrialRequiresPaymentMethod reports whether
// FlagSelfServeTrialRequiresPaymentMethod is on for an organization, evaluated
// with the organization id as the distinct id so every caller agrees. It fails
// open to starting trials without a card, which is the behaviour the flag
// switches off.
func SelfServeTrialRequiresPaymentMethod(ctx context.Context, flags Provider, organizationID string) bool {
	if flags == nil || organizationID == "" {
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, FlagSelfServeTrialRequiresPaymentMethod, organizationID, nil, nil)
	return err == nil && enabled
}
