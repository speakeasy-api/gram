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

func TestGetStripeCheckoutStatusOpenPaid(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatus(t, "open", "paid")
}

func TestGetStripeCheckoutStatusOpenUnpaid(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatus(t, "open", "unpaid")
}

func TestGetStripeCheckoutStatusOpenNoPaymentRequired(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatus(t, "open", "no_payment_required")
}

func TestGetStripeCheckoutStatusCompletePaid(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatus(t, "complete", "paid")
}

func TestGetStripeCheckoutStatusCompleteUnpaid(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatus(t, "complete", "unpaid")
}

func TestGetStripeCheckoutStatusCompleteNoPaymentRequired(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatus(t, "complete", "no_payment_required")
}

func TestGetStripeCheckoutStatusExpiredPaid(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatus(t, "expired", "paid")
}

func TestGetStripeCheckoutStatusExpiredUnpaid(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatus(t, "expired", "unpaid")
}

func TestGetStripeCheckoutStatusExpiredNoPaymentRequired(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatus(t, "expired", "no_payment_required")
}

func testGetStripeCheckoutStatus(t *testing.T, status, payment string) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{ID: "cs_test", CustomerID: "cus_test", OrganizationID: ti.orgID, Status: status, PaymentStatus: payment}
	result, err := ti.service.GetStripeCheckoutStatus(ti.context(t, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID), authz.NewGrant(authz.ScopeOrgRead, ti.orgID)), &gen.GetStripeCheckoutStatusPayload{SessionID: "cs_test"})
	require.NoError(t, err)
	require.Equal(t, status, result.Status)
	require.Equal(t, payment, result.PaymentStatus)
	require.False(t, result.BillingReady, "missing expanded subscription is not ready")
}

func TestGetStripeCheckoutStatusRejectsInvalidAndForeignSessionsMissing(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusRejectsInvalidAndForeignSessions(t, "", "cus_test", "own", oops.CodeBadRequest)
}

func TestGetStripeCheckoutStatusRejectsInvalidAndForeignSessionsInvalid(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusRejectsInvalidAndForeignSessions(t, "not-session", "cus_test", "own", oops.CodeBadRequest)
}

func TestGetStripeCheckoutStatusRejectsInvalidAndForeignSessionsUnknown(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusRejectsInvalidAndForeignSessions(t, "cs_missing", "cus_test", "own", oops.CodeNotFound)
}

func TestGetStripeCheckoutStatusRejectsInvalidAndForeignSessionsForeignCustomer(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusRejectsInvalidAndForeignSessions(t, "cs_test", "cus_other", "own", oops.CodeNotFound)
}

func TestGetStripeCheckoutStatusRejectsInvalidAndForeignSessionsForeignMetadata(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusRejectsInvalidAndForeignSessions(t, "cs_test", "cus_test", "other", oops.CodeNotFound)
}

func TestGetStripeCheckoutStatusRejectsInvalidAndForeignSessionsMissingMetadata(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusRejectsInvalidAndForeignSessions(t, "cs_test", "cus_test", "", oops.CodeNotFound)
}

func testGetStripeCheckoutStatusRejectsInvalidAndForeignSessions(t *testing.T, tcId string, tcCustomer string, tcOrg string, tcCode oops.Code) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	org := tcOrg
	if org == "own" {
		org = ti.orgID
	}
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{ID: "cs_test", CustomerID: tcCustomer, OrganizationID: org, Status: "complete", PaymentStatus: "paid"}
	_, err := ti.service.GetStripeCheckoutStatus(ti.context(t, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID), authz.NewGrant(authz.ScopeOrgRead, ti.orgID)), &gen.GetStripeCheckoutStatusPayload{SessionID: tcId})
	requireOopsCode(t, err, tcCode)
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

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscriptionObservedTrial(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t, "complete", "unpaid", "trialing", "cus_test", true)
}

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscriptionPendingActivePayment(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t, "complete", "unpaid", "active", "cus_test", false)
}

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscriptionPaidActive(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t, "complete", "paid", "active", "cus_test", true)
}

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscriptionNoPaymentRequired(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t, "complete", "no_payment_required", "trialing", "cus_test", true)
}

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscriptionOpenTrial(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t, "open", "unpaid", "trialing", "cus_test", false)
}

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscriptionExpiredTrial(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t, "expired", "unpaid", "trialing", "cus_test", false)
}

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscriptionForeignSubscription(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t, "complete", "unpaid", "trialing", "cus_other", false)
}

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscriptionUnexpandedSubscription(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t, "complete", "paid", "", "", false)
}

func TestGetStripeCheckoutStatusReadinessUsesReturnedSubscriptionCanceledSubscription(t *testing.T) {
	t.Parallel()
	testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t, "complete", "paid", "canceled", "cus_test", false)
}

func testGetStripeCheckoutStatusReadinessUsesReturnedSubscription(t *testing.T, tcStatus string, tcPayment string, tcSubscription string, tcCustomer string, tcReady bool) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	// An existing locally active subscription must not make this checkout ready.
	configureStripeSubscription(t, ti, false)
	before, err := repo.New(ti.service.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{
		ID: "cs_test", CustomerID: "cus_test", OrganizationID: ti.orgID,
		Status: tcStatus, PaymentStatus: tcPayment, SubscriptionID: "sub_returned",
		SubscriptionStatus: tcSubscription, SubscriptionCustomerID: tcCustomer,
		BillingCycleAnchor: time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
	}
	result, err := ti.service.GetStripeCheckoutStatus(ti.context(t, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID), authz.NewGrant(authz.ScopeOrgRead, ti.orgID)), &gen.GetStripeCheckoutStatusPayload{SessionID: "cs_test"})
	require.NoError(t, err)
	require.Equal(t, tcReady, result.BillingReady)
	require.Equal(t, tcPayment, result.PaymentStatus)
	after, err := repo.New(ti.service.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, before, after, "status must remain read-only")
}
