package admin

import (
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
