package admin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func seedPaygCustomer(t *testing.T, ctx context.Context, conn testrepo.DBTX, id, slug, customerID string) {
	t.Helper()
	seedOrg(t, ctx, conn, orgFixture{id: id, name: slug, slug: slug, accountType: "payg"})
	require.NoError(t, usagerepo.New(conn).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID: id, StripeCustomerID: conv.ToPGText(customerID),
	}))
	_, err := usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID: id, TumMonthlyTokenLimit: pgtype.Int8{Int64: 9001, Valid: true},
		AlertEmail: conv.ToPGText("billing@example.test"), BillingCycleAnchorDay: 17,
		TunneledMcpServerLimit: pgtype.Int4{Int32: 23, Valid: true},
	})
	require.NoError(t, err)
}

func subscriptionFor(customerID, subscriptionID string, anchor time.Time) *stripeclient.SubscriptionState {
	return &stripeclient.SubscriptionState{
		ID:                 subscriptionID,
		CustomerID:         customerID,
		Status:             "active",
		BillingCycleAnchor: anchor,
	}
}

func TestGetStripeSubscriptionCandidate_ReturnsDetailsWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	anchor := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	fake.subscriptionByID = subscriptionFor("cus_preview_sub", "sub_preview_1", anchor)
	seedPaygCustomer(t, ctx, conn, "org_sub_preview", "sub-preview", "cus_preview_sub")

	result, err := svc.GetStripeSubscriptionCandidate(ctx, &gen.GetStripeSubscriptionCandidatePayload{
		OrganizationID: "org_sub_preview", StripeSubscriptionID: "sub_preview_1",
	})
	require.NoError(t, err)
	require.Equal(t, "sub_preview_1", result.ID)
	require.Equal(t, "cus_preview_sub", result.CustomerID)
	require.Equal(t, "active", result.Status)
	require.Equal(t, 1, fake.subscriptionLookupCount())

	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_preview")
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
}

func TestSetStripeSubscription_RecordsSubscriptionAndAnchorWithoutAuditEvent(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	anchor := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	fake.subscriptionByID = subscriptionFor("cus_set_sub", "sub_set_1", anchor)
	seedPaygCustomer(t, ctx, conn, "org_set_sub", "set-sub", "cus_set_sub")
	before, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_set_sub")
	require.NoError(t, err)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "session-set-sub", Email: "operator@example.test", OIDCSubject: "oidc-set-sub", Name: "Test Operator", HD: "example.test",
	})
	countBefore, err := audittest.AuditLogCount(ctx, conn)
	require.NoError(t, err)

	result, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
		OrganizationID: "org_set_sub", StripeSubscriptionID: "sub_set_1",
	})
	require.NoError(t, err)
	require.Equal(t, new("cus_set_sub"), result.StripeCustomerID)
	require.Equal(t, new("sub_set_1"), result.StripeSubscriptionID)
	require.Equal(t, "payg", result.AccountType)

	after, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_set_sub")
	require.NoError(t, err)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, "sub_set_1", after.StripeSubscriptionID.String)
	require.True(t, after.StripeBillingCycleAnchor.Time.Equal(anchor))
	require.EqualValues(t, 15, after.BillingCycleAnchorDay)
	require.Equal(t, before.TumMonthlyTokenLimit, after.TumMonthlyTokenLimit)
	require.Equal(t, before.AlertEmail, after.AlertEmail)
	require.Equal(t, before.TunneledMcpServerLimit, after.TunneledMcpServerLimit)

	countAfter, err := audittest.AuditLogCount(ctx, conn)
	require.NoError(t, err)
	require.Equal(t, countBefore, countAfter)
	require.Equal(t, 2, fake.subscriptionLookupCount())
}

func TestSetStripeSubscription_RejectsCustomerMismatchWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	fake.subscriptionByID = subscriptionFor("cus_other", "sub_mismatch", time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC))
	seedPaygCustomer(t, ctx, conn, "org_sub_mismatch", "sub-mismatch", "cus_owner")

	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
		OrganizationID: "org_sub_mismatch", StripeSubscriptionID: "sub_mismatch",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_mismatch")
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	require.EqualValues(t, 17, metadata.BillingCycleAnchorDay)
}

func TestSetStripeSubscription_RejectsMissingAnchorWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	fake.subscriptionByID = subscriptionFor("cus_no_anchor", "sub_no_anchor", time.Time{})
	seedPaygCustomer(t, ctx, conn, "org_sub_no_anchor", "sub-no-anchor", "cus_no_anchor")

	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
		OrganizationID: "org_sub_no_anchor", StripeSubscriptionID: "sub_no_anchor",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_no_anchor")
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
}

func TestSetStripeSubscription_LookupFailureDoesNotWrite(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	fake.subscriptionErr = oops.E(oops.CodeGatewayError, errors.New("stripe upstream failed"), "Stripe subscription lookup failed")
	seedPaygCustomer(t, ctx, conn, "org_sub_lookup", "sub-lookup", "cus_lookup")

	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
		OrganizationID: "org_sub_lookup", StripeSubscriptionID: "sub_lookup",
	})
	requireOopsCode(t, err, oops.CodeGatewayError)
	require.Equal(t, 1, fake.subscriptionLookupCount())
	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_lookup")
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
}

func TestStripeSubscriptionEndpoints_RejectIneligibleOrganizationsBeforeLookup(t *testing.T) { //nolint:tparallel // shared fake lookup count is read after every case
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_sub_free", name: "Free", slug: "sub-free", accountType: "free"})
	require.NoError(t, usagerepo.New(conn).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID: "org_sub_free", StripeCustomerID: conv.ToPGText("cus_free"),
	}))
	_, err := usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID: "org_sub_free", TumMonthlyTokenLimit: pgtype.Int8{}, AlertEmail: pgtype.Text{},
		BillingCycleAnchorDay: 1, TunneledMcpServerLimit: pgtype.Int4{},
	})
	require.NoError(t, err)
	seedOrg(t, ctx, conn, orgFixture{id: "org_sub_nocustomer", name: "No Customer", slug: "sub-nocustomer", accountType: "payg"})
	seedPaygCustomer(t, ctx, conn, "org_sub_existing", "sub-existing", "cus_existing_sub")
	require.NoError(t, usagerepo.New(conn).SetStripeSubscriptionFixture(ctx, usagerepo.SetStripeSubscriptionFixtureParams{
		OrganizationID: "org_sub_existing", StripeSubscriptionID: conv.ToPGText("sub_already"),
	}))

	cases := []struct {
		name           string
		organizationID string
	}{
		{name: "not payg", organizationID: "org_sub_free"},
		{name: "missing customer", organizationID: "org_sub_nocustomer"},
		{name: "existing subscription", organizationID: "org_sub_existing"},
	}
	for _, tc := range cases { //nolint:paralleltest // parent asserts the shared lookup count after the cases
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
				OrganizationID: tc.organizationID, StripeSubscriptionID: "sub_blocked",
			})
			requireOopsCode(t, err, oops.CodeConflict)
			_, err = svc.GetStripeSubscriptionCandidate(ctx, &gen.GetStripeSubscriptionCandidatePayload{
				OrganizationID: tc.organizationID, StripeSubscriptionID: "sub_blocked",
			})
			requireOopsCode(t, err, oops.CodeConflict)
		})
	}
	require.Zero(t, fake.subscriptionLookupCount())
	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_free")
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	metadata, err = usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_existing")
	require.NoError(t, err)
	require.Equal(t, "sub_already", metadata.StripeSubscriptionID.String)
}

func TestStripeSubscriptionEndpoints_RejectInvalidIDsBeforeLookup(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	seedPaygCustomer(t, ctx, conn, "org_sub_invalid", "sub-invalid", "cus_invalid")

	for _, subscriptionID := range []string{"", "subscription_123", " sub_space", "sub_trailing ", "sub_bad-hyphen", "sub_" + strings.Repeat("a", 252)} {
		_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
			OrganizationID: "org_sub_invalid", StripeSubscriptionID: subscriptionID,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
		_, err = svc.GetStripeSubscriptionCandidate(ctx, &gen.GetStripeSubscriptionCandidatePayload{
			OrganizationID: "org_sub_invalid", StripeSubscriptionID: subscriptionID,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	require.Zero(t, fake.subscriptionLookupCount())
}

func TestSetStripeSubscription_RejectsMissingOrganizationBeforeLookup(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)

	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
		OrganizationID: "org_sub_missing", StripeSubscriptionID: "sub_missing_org",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Zero(t, fake.subscriptionLookupCount())
}

func TestSetStripeSubscription_RejectsSubscriptionOwnedByAnotherOrganization(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	anchor := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	fake.subscriptionByID = subscriptionFor("cus_contender", "sub_taken", anchor)
	seedPaygCustomer(t, ctx, conn, "org_sub_owner", "sub-owner", "cus_owner_taken")
	require.NoError(t, usagerepo.New(conn).SetStripeSubscriptionFixture(ctx, usagerepo.SetStripeSubscriptionFixtureParams{
		OrganizationID: "org_sub_owner", StripeSubscriptionID: conv.ToPGText("sub_taken"),
	}))
	seedPaygCustomer(t, ctx, conn, "org_sub_contender", "sub-contender", "cus_contender")

	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
		OrganizationID: "org_sub_contender", StripeSubscriptionID: "sub_taken",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	contender, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_contender")
	require.NoError(t, err)
	require.False(t, contender.StripeSubscriptionID.Valid)
	owner, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_owner")
	require.NoError(t, err)
	require.Equal(t, "sub_taken", owner.StripeSubscriptionID.String)
}

func TestSetStripeSubscription_RejectsRetryAfterSuccess(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	anchor := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	fake.subscriptionByID = subscriptionFor("cus_retry", "sub_retry", anchor)
	seedPaygCustomer(t, ctx, conn, "org_sub_retry", "sub-retry", "cus_retry")

	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
		OrganizationID: "org_sub_retry", StripeSubscriptionID: "sub_retry",
	})
	require.NoError(t, err)
	_, err = svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
		OrganizationID: "org_sub_retry", StripeSubscriptionID: "sub_retry",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t, 2, fake.subscriptionLookupCount())
}

func TestSetStripeSubscription_RejectsSubscriptionGoneAfterLock(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	anchor := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	fake.subscriptionByID = subscriptionFor("cus_gone", "sub_gone", anchor)
	fake.liveSubscriptionErr = oops.E(oops.CodeNotFound, stripeclient.ErrSubscriptionNotFound, "Stripe subscription not found")
	seedPaygCustomer(t, ctx, conn, "org_sub_gone", "sub-gone", "cus_gone")

	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
		OrganizationID: "org_sub_gone", StripeSubscriptionID: "sub_gone",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Equal(t, 2, fake.subscriptionLookupCount())
	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_gone")
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
}

func TestSetStripeSubscription_ConcurrentAssignmentsOnlyOneSucceeds(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	seedPaygCustomer(t, ctx, conn, "org_sub_race", "sub-race", "cus_race")
	fake.subscriptionCustomerID = "cus_race"

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, subscriptionID := range []string{"sub_race_a", "sub_race_b"} {
		go func() {
			ready.Done()
			<-start
			_, callErr := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{
				OrganizationID: "org_sub_race", StripeSubscriptionID: subscriptionID,
			})
			results <- callErr
		}()
	}
	ready.Wait()
	close(start)

	var successes, conflicts int
	for range 2 {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		requireOopsCode(t, err, oops.CodeConflict)
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_sub_race")
	require.NoError(t, err)
	require.Contains(t, []string{"sub_race_a", "sub_race_b"}, metadata.StripeSubscriptionID.String)
}
