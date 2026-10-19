package usage

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
)

func TestGetStripeCheckoutStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"open", "complete", "expired"} {
		for _, payment := range []string{"paid", "unpaid", "no_payment_required"} {
			t.Run(status+"/"+payment, func(t *testing.T) {
				t.Parallel()
				ti := newStripeCheckoutTestInstance(t)
				configureStripeSubscription(t, ti, false)
				ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{ID: "cs_test", CustomerID: "cus_test", OrganizationID: ti.orgID, Status: status, PaymentStatus: payment}
				result, err := ti.service.GetStripeCheckoutStatus(ti.context(t, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID), authz.NewGrant(authz.ScopeOrgRead, ti.orgID)), &gen.GetStripeCheckoutStatusPayload{SessionID: "cs_test"})
				require.NoError(t, err)
				require.Equal(t, status, result.Status)
				require.Equal(t, payment, result.PaymentStatus)
				require.False(t, result.BillingReady, "missing expanded subscription is not ready")
			})
		}
	}
}

func TestGetStripeCheckoutStatusRejectsInvalidAndForeignSessions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, id, customer, org string
		code                    oops.Code
	}{
		{"missing", "", "cus_test", "own", oops.CodeBadRequest},
		{"invalid", "not-session", "cus_test", "own", oops.CodeBadRequest},
		{"unknown", "cs_missing", "cus_test", "own", oops.CodeNotFound},
		{"foreign customer", "cs_test", "cus_other", "own", oops.CodeNotFound},
		{"foreign metadata", "cs_test", "cus_test", "other", oops.CodeNotFound},
		{"missing metadata", "cs_test", "cus_test", "", oops.CodeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ti := newStripeCheckoutTestInstance(t)
			configureStripeSubscription(t, ti, false)
			org := tc.org
			if org == "own" {
				org = ti.orgID
			}
			ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{ID: "cs_test", CustomerID: tc.customer, OrganizationID: org, Status: "complete", PaymentStatus: "paid"}
			_, err := ti.service.GetStripeCheckoutStatus(ti.context(t, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID), authz.NewGrant(authz.ScopeOrgRead, ti.orgID)), &gen.GetStripeCheckoutStatusPayload{SessionID: tc.id})
			requireOopsCode(t, err, tc.code)
		})
	}
}

func TestGetStripeCheckoutStatusRequiresAdminAndRead(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	_, err := ti.service.GetStripeCheckoutStatus(ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)), &gen.GetStripeCheckoutStatusPayload{SessionID: "cs_test"})
	requireOopsCode(t, err, oops.CodeForbidden)
	configureStripeSubscription(t, ti, false)
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{ID: "cs_test", CustomerID: "cus_test", OrganizationID: ti.orgID, Status: "open", PaymentStatus: "unpaid"}
	// Organization admin implies read unless read is explicitly blocked.
	_, err = ti.service.GetStripeCheckoutStatus(ti.context(t, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID)), &gen.GetStripeCheckoutStatusPayload{SessionID: "cs_test"})
	require.NoError(t, err)
	_, err = ti.service.GetStripeCheckoutStatus(ti.context(t, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID), authz.NewGrant(authz.ScopeOrgBlockedRead, ti.orgID)), &gen.GetStripeCheckoutStatusPayload{SessionID: "cs_test"})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.GetStripeCheckoutStatus(t.Context(), &gen.GetStripeCheckoutStatusPayload{SessionID: "cs_test"})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, status, payment, subscription, customer string
		ready                                         bool
	}{
		{"observed trial", "complete", "unpaid", "trialing", "cus_test", true},
		{"pending active payment", "complete", "unpaid", "active", "cus_test", false},
		{"paid active", "complete", "paid", "active", "cus_test", true},
		{"no payment required", "complete", "no_payment_required", "trialing", "cus_test", true},
		{"open trial", "open", "unpaid", "trialing", "cus_test", false},
		{"expired trial", "expired", "unpaid", "trialing", "cus_test", false},
		{"foreign subscription", "complete", "unpaid", "trialing", "cus_other", false},
		{"unexpanded subscription", "complete", "paid", "", "", false},
		{"canceled subscription", "complete", "paid", "canceled", "cus_test", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ti := newStripeCheckoutTestInstance(t)
			// An existing locally active subscription must not make this checkout ready.
			configureStripeSubscription(t, ti, false)
			before, err := repo.New(ti.service.db).GetBillingMetadata(t.Context(), ti.orgID)
			require.NoError(t, err)
			ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{
				ID: "cs_test", CustomerID: "cus_test", OrganizationID: ti.orgID,
				Status: tc.status, PaymentStatus: tc.payment, SubscriptionID: "sub_returned",
				SubscriptionStatus: tc.subscription, SubscriptionCustomerID: tc.customer,
				BillingCycleAnchor: time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
			}
			result, err := ti.service.GetStripeCheckoutStatus(ti.context(t, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID), authz.NewGrant(authz.ScopeOrgRead, ti.orgID)), &gen.GetStripeCheckoutStatusPayload{SessionID: "cs_test"})
			require.NoError(t, err)
			require.Equal(t, tc.ready, result.BillingReady)
			require.Equal(t, tc.payment, result.PaymentStatus)
			after, err := repo.New(ti.service.db).GetBillingMetadata(t.Context(), ti.orgID)
			require.NoError(t, err)
			require.Equal(t, before, after, "status must remain read-only")
		})
	}
}
