package usage

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func configureStripeSubscription(t *testing.T, ti *stripeCheckoutTestInstance, cancelAtPeriodEnd bool) {
	t.Helper()

	require.NoError(t, repo.New(ti.db).CreateStripeSubscriptionBillingMetadataFixture(t.Context(), repo.CreateStripeSubscriptionBillingMetadataFixtureParams{
		OrganizationID:       ti.orgID,
		StripeCustomerID:     pgtype.Text{String: "cus_test", Valid: true},
		StripeSubscriptionID: pgtype.Text{String: "sub_test", Valid: true},
	}))
	ti.stripe.subscriptionState = &stripeclient.SubscriptionState{
		ID:                           "sub_test",
		CustomerID:                   "cus_test",
		Status:                       "active",
		CurrentPeriodStart:           time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
		CurrentPeriodEnd:             time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC),
		TrialStart:                   time.Time{},
		TrialEnd:                     time.Time{},
		CancelAtPeriodEnd:            cancelAtPeriodEnd,
		CancelAt:                     time.Time{},
		CanceledAt:                   time.Time{},
		LatestInvoiceID:              "in_test",
		LatestInvoiceStatus:          "paid",
		LatestInvoiceAmountRemaining: 0,
		PaymentFailed:                false,
	}
}

func TestGetStripeSubscriptionRequiresOrganizationRead(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)

	_, err := ti.service.GetStripeSubscription(ti.context(t), &gen.GetStripeSubscriptionPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetStripeSubscriptionReturnsLiveState(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)

	result, err := ti.service.GetStripeSubscription(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetStripeSubscriptionPayload{},
	)
	require.NoError(t, err)
	require.Equal(t, "active", result.Status)
	require.Equal(t, "2026-08-15T00:00:00Z", result.CurrentPeriodStart)
	require.Equal(t, "2026-09-15T00:00:00Z", result.CurrentPeriodEnd)
	require.False(t, result.CancelAtPeriodEnd)
	require.False(t, result.PaymentFailed)
	require.Nil(t, result.TrialStart)
	require.Nil(t, result.TrialEnd)
}

func TestGetStripeSubscriptionReturnsNotFoundWithoutSubscription(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	_, err := ti.service.GetStripeSubscription(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetStripeSubscriptionPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestCreateStripePortalSessionRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	_, err := ti.service.CreateStripePortalSession(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.CreateStripePortalSessionPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, ti.stripe.portalInputs)
}

func TestCancelStripeSubscriptionRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	_, err := ti.service.CancelStripeSubscription(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.CancelStripeSubscriptionPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, ti.stripe.subscriptionUpdates)
}

func TestResumeStripeSubscriptionRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, true)
	_, err := ti.service.ResumeStripeSubscription(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.ResumeStripeSubscriptionPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, ti.stripe.subscriptionUpdates)
}

func TestCreateStripePortalSessionUsesBillingReturnURLAndAudits(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	baseline, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripePortal)
	require.NoError(t, err)

	portalURL, err := ti.service.CreateStripePortalSession(ti.adminContext(t), &gen.CreateStripePortalSessionPayload{})
	require.NoError(t, err)
	require.Equal(t, "https://billing.stripe.test/session", portalURL)
	require.Len(t, ti.stripe.portalInputs, 1)
	require.Equal(t, "cus_test", ti.stripe.portalInputs[0].CustomerID)
	require.Equal(t, "https://app.example.test/"+ti.orgSlug+"/billing", ti.stripe.portalInputs[0].ReturnURL)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripePortal)
	require.NoError(t, err)
	require.Equal(t, baseline+1, after)
}

func TestCreateStripePortalSessionRejectsPortalCustomerMismatch(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	ti.stripe.portalCustomerIDOverride = "cus_other"
	baseline, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripePortal)
	require.NoError(t, err)

	_, err = ti.service.CreateStripePortalSession(ti.adminContext(t), &gen.CreateStripePortalSessionPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnavailable)
	after, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripePortal)
	require.NoError(t, err)
	require.Equal(t, baseline, after)
}

func TestCreateStripePortalSessionPropagatesStripeFailure(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	ti.stripe.portalError = errors.New("Stripe unavailable")

	_, err := ti.service.CreateStripePortalSession(ti.adminContext(t), &gen.CreateStripePortalSessionPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestCancelStripeSubscriptionSchedulesPeriodEndAndAudits(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	baseline, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCancelStripeSubscription)
	require.NoError(t, err)

	result, err := ti.service.CancelStripeSubscription(ti.adminContext(t), &gen.CancelStripeSubscriptionPayload{})
	require.NoError(t, err)
	require.True(t, result.CancelAtPeriodEnd)
	require.Equal(t, []stripeclient.SetSubscriptionCancelAtPeriodEndInput{{SubscriptionID: "sub_test", CancelAtPeriodEnd: true}}, ti.stripe.subscriptionUpdates)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCancelStripeSubscription)
	require.NoError(t, err)
	require.Equal(t, baseline+1, after)
}

func TestResumeStripeSubscriptionRemovesPeriodEndCancellationAndAudits(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, true)
	baseline, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataResumeStripeSubscription)
	require.NoError(t, err)

	result, err := ti.service.ResumeStripeSubscription(ti.adminContext(t), &gen.ResumeStripeSubscriptionPayload{})
	require.NoError(t, err)
	require.False(t, result.CancelAtPeriodEnd)
	require.Equal(t, []stripeclient.SetSubscriptionCancelAtPeriodEndInput{{SubscriptionID: "sub_test", CancelAtPeriodEnd: false}}, ti.stripe.subscriptionUpdates)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataResumeStripeSubscription)
	require.NoError(t, err)
	require.Equal(t, baseline+1, after)
}

func TestCancelStripeSubscriptionRetriesAppliedChangeAndAudits(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, true)
	baseline, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCancelStripeSubscription)
	require.NoError(t, err)

	result, err := ti.service.CancelStripeSubscription(ti.adminContext(t), &gen.CancelStripeSubscriptionPayload{})
	require.NoError(t, err)
	require.True(t, result.CancelAtPeriodEnd)
	require.Equal(t, []stripeclient.SetSubscriptionCancelAtPeriodEndInput{{SubscriptionID: "sub_test", CancelAtPeriodEnd: true}}, ti.stripe.subscriptionUpdates)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCancelStripeSubscription)
	require.NoError(t, err)
	require.Equal(t, baseline+1, after)
}

func TestStripeSubscriptionLifecycleRejectsTerminalState(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"canceled", "unpaid", "incomplete", "incomplete_expired", "paused"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()

			for _, operation := range []struct {
				name string
				run  func(*stripeCheckoutTestInstance) error
			}{
				{name: "cancel", run: func(ti *stripeCheckoutTestInstance) error {
					_, err := ti.service.CancelStripeSubscription(ti.adminContext(t), &gen.CancelStripeSubscriptionPayload{})
					return err
				}},
				{name: "resume", run: func(ti *stripeCheckoutTestInstance) error {
					_, err := ti.service.ResumeStripeSubscription(ti.adminContext(t), &gen.ResumeStripeSubscriptionPayload{})
					return err
				}},
			} {
				t.Run(operation.name, func(t *testing.T) {
					ti := newStripeCheckoutTestInstance(t)
					configureStripeSubscription(t, ti, operation.name == "resume")
					ti.stripe.subscriptionState.Status = status

					err := operation.run(ti)
					requireOopsCode(t, err, oops.CodeConflict)
					require.Empty(t, ti.stripe.subscriptionUpdates)
				})
			}
		})
	}
}

func TestCancelStripeSubscriptionRejectsRemoteIdentityMismatch(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	ti.stripe.subscriptionState.CustomerID = "cus_other"

	_, err := ti.service.CancelStripeSubscription(ti.adminContext(t), &gen.CancelStripeSubscriptionPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.Empty(t, ti.stripe.subscriptionUpdates)
}

func TestCancelStripeSubscriptionPropagatesStripeFailure(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	ti.stripe.subscriptionError = errors.New("Stripe unavailable")

	_, err := ti.service.CancelStripeSubscription(ti.adminContext(t), &gen.CancelStripeSubscriptionPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestCancelStripeSubscriptionRejectsLocalIdentityChangeAfterStripeUpdate(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	baseline, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCancelStripeSubscription)
	require.NoError(t, err)
	ti.stripe.afterSubscriptionUpdate = func() {
		require.NoError(t, repo.New(ti.db).SetStripeSubscriptionFixture(t.Context(), repo.SetStripeSubscriptionFixtureParams{
			StripeSubscriptionID: pgtype.Text{String: "sub_replaced", Valid: true},
			OrganizationID:       ti.orgID,
		}))
	}

	_, err = ti.service.CancelStripeSubscription(ti.adminContext(t), &gen.CancelStripeSubscriptionPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
	after, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCancelStripeSubscription)
	require.NoError(t, err)
	require.Equal(t, baseline, after)
}
