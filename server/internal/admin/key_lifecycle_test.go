package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/stretchr/testify/require"
)

func TestExistingBillingEligibility(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"active", "trialing", "past_due", "canceled", "unpaid", "incomplete", "incomplete_expired", "paused", "", "unknown"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			org := repo.AdminGetOrganizationRow{StripeCustomerID: pgtype.Text{String: "cus_test", Valid: true}, StripeSubscriptionID: pgtype.Text{String: "sub_test", Valid: true}}
			svc := &Service{billing: &fakeBillingOperations{subscriptionByID: &stripeclient.SubscriptionState{ID: "sub_test", CustomerID: "cus_test", Status: status, BillingCycleAnchor: time.Now()}}}
			err := svc.requireExistingBillingEligibility(t.Context(), org)
			if status == "active" || status == "trialing" || status == "past_due" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	for _, bad := range []string{"customer only", "no customer", "wrong customer", "wrong subscription", "no anchor", "provider failure"} {
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			org := repo.AdminGetOrganizationRow{StripeCustomerID: pgtype.Text{String: "cus_test", Valid: true}, StripeSubscriptionID: pgtype.Text{String: "sub_test", Valid: true}}
			state := &stripeclient.SubscriptionState{ID: "sub_test", CustomerID: "cus_test", Status: "active", BillingCycleAnchor: time.Now()}
			fake := &fakeBillingOperations{subscriptionByID: state}
			switch bad {
			case "customer only":
				org.StripeSubscriptionID = pgtype.Text{}
			case "no customer":
				org.StripeCustomerID = pgtype.Text{}
			case "wrong customer":
				state.CustomerID = "cus_other"
			case "wrong subscription":
				state.ID = "sub_other"
			case "no anchor":
				state.BillingCycleAnchor = time.Time{}
			case "provider failure":
				fake.subscriptionErr = errors.New("unavailable")
			}
			svc := &Service{billing: fake}
			require.Error(t, svc.requireExistingBillingEligibility(t.Context(), org))
		})
	}
}

// Embed the shared fake and inspect only the live request's deadline.
type deadlineBillingOperations struct {
	*fakeBillingOperations
	deadline time.Time
}

func (f *deadlineBillingOperations) GetStripeSubscriptionByID(ctx context.Context, id string) (*stripeclient.SubscriptionState, error) {
	f.deadline, _ = ctx.Deadline()
	return f.fakeBillingOperations.GetStripeSubscriptionByID(ctx, id)
}
func TestExistingBillingEligibilityBoundsLookup(t *testing.T) {
	t.Parallel()
	org := repo.AdminGetOrganizationRow{StripeCustomerID: pgtype.Text{String: "cus_test", Valid: true}, StripeSubscriptionID: pgtype.Text{String: "sub_test", Valid: true}}
	fake := &deadlineBillingOperations{fakeBillingOperations: &fakeBillingOperations{subscriptionByID: &stripeclient.SubscriptionState{ID: "sub_test", CustomerID: "cus_test", Status: "active", BillingCycleAnchor: time.Now()}}}
	svc := &Service{billing: fake}
	start := time.Now()
	require.NoError(t, svc.requireExistingBillingEligibility(t.Context(), org))
	require.False(t, fake.deadline.IsZero())
	require.WithinDuration(t, start.Add(5*time.Second), fake.deadline, time.Second)
}

func TestTrialDemotionRepairEligibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tier, trialTier string
		converted, allowed    bool
	}{
		{"free", "free", "", false, false},
		{"pro", "pro", "", false, false},
		{"verified payg", "payg", "pro", false, true},
		{"enterprise", "enterprise", "", false, true},
		{"enterprise trial", "enterprise", "enterprise", false, false},
		{"converted enterprise", "enterprise", "enterprise", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			org := repo.AdminGetOrganizationRow{AccountType: tc.tier, TrialTier: pgtype.Text{String: tc.trialTier, Valid: tc.trialTier != ""}, TrialConvertedAt: pgtype.Timestamptz{Time: time.Now(), Valid: tc.converted}, StripeCustomerID: pgtype.Text{String: "cus_test", Valid: true}, StripeSubscriptionID: pgtype.Text{String: "sub_test", Valid: true}}
			svc := &Service{billing: &fakeBillingOperations{subscriptionByID: &stripeclient.SubscriptionState{ID: "sub_test", CustomerID: "cus_test", Status: "active", BillingCycleAnchor: time.Now()}}}
			err := svc.requireTrialDemotionRepairEligibility(t.Context(), org)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tc.allowed, svc.inferenceKeyCauseDiagnostics(t.Context(), org, []string{"trial_demotion"}, true)[0].Removable)
		})
	}
}
