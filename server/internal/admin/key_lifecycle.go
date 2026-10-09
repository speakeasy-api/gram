package admin

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// requireExistingBillingEligibility verifies the linked subscription, not just a
// Stripe customer. Callers must supply the canonical organization's locked local
// identity (or recheck that identity under lock before committing a mutation).
// It intentionally does not require the current tier to already be PAYG.
func (s *Service) requireExistingBillingEligibility(ctx context.Context, organization repo.AdminGetOrganizationRow) error {
	if !organization.StripeCustomerID.Valid || organization.StripeCustomerID.String == "" || !organization.StripeSubscriptionID.Valid || organization.StripeSubscriptionID.String == "" {
		return oops.E(oops.CodeConflict, nil, "An existing linked Stripe customer and subscription are required")
	}
	if s.billing == nil {
		return oops.E(oops.CodeUnavailable, nil, "Live billing verification is unavailable")
	}
	state, err := s.liveStripeSubscriptionForOrganization(ctx, organization, organization.StripeSubscriptionID.String)
	if err != nil {
		return err
	}
	// Match checkout activation's active/trialing/past_due policy.
	switch state.Status {
	case "active", "trialing", "past_due":
		return nil
	default:
		return oops.E(oops.CodeConflict, nil, "Stripe subscription is not eligible for activation")
	}
}
