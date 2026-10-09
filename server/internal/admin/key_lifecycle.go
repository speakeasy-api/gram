package admin

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
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
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	state, err := s.liveStripeSubscriptionForOrganization(lookupCtx, organization, organization.StripeSubscriptionID.String)
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

// Repair removes stale restrictions; it never grants a new tier entitlement.
func (s *Service) requireTrialDemotionRepairEligibility(ctx context.Context, org repo.AdminGetOrganizationRow) error {
	switch org.AccountType {
	case string(billing.TierPayg):
		return s.requireExistingBillingEligibility(ctx, org)
	case string(billing.TierEnterprise):
		if org.TrialTier.String != "enterprise" || org.TrialConvertedAt.Valid {
			return nil
		}
	}
	return oops.E(oops.CodeConflict, nil, "Trial demotion repair requires verified PAYG billing or a non-trial enterprise entitlement; use the account lifecycle workflow first")
}
