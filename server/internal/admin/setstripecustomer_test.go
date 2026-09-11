package admin

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func enableStripeCustomerLookup(svc *Service) *fakeBillingOperations {
	fake := &fakeBillingOperations{}
	svc.billing = fake
	return fake
}

func TestGetStripeCustomer_ReturnsDetailsWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	fake.customer = &stripeclient.CustomerDetails{
		ID:          "cus_preview_1",
		Name:        "Preview Customer",
		Email:       "billing@example.test",
		Description: "Confirmed by an admin",
		LiveMode:    true,
	}
	seedOrg(t, ctx, conn, orgFixture{id: "org_customer_preview", name: "Customer Preview", slug: "customer-preview"})

	result, err := svc.GetStripeCustomer(ctx, &gen.GetStripeCustomerPayload{
		OrganizationID: "org_customer_preview", StripeCustomerID: "cus_preview_1",
	})
	require.NoError(t, err)
	require.Equal(t, "cus_preview_1", result.ID)
	require.Equal(t, new("Preview Customer"), result.Name)
	require.Equal(t, new("billing@example.test"), result.Email)
	require.Equal(t, new("Confirmed by an admin"), result.Description)
	require.True(t, result.Livemode)
	require.Equal(t, 1, fake.customerLookupCount())

	_, err = usagerepo.New(conn).GetBillingMetadata(ctx, "org_customer_preview")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestGetStripeCustomer_RejectsExistingIdentityBeforeProviderLookup(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_preview_ineligible", name: "Preview Ineligible", slug: "preview-ineligible"})
	require.NoError(t, usagerepo.New(conn).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID: "org_preview_ineligible", StripeCustomerID: conv.ToPGText("cus_existing_preview"),
	}))

	_, err := svc.GetStripeCustomer(ctx, &gen.GetStripeCustomerPayload{
		OrganizationID: "org_preview_ineligible", StripeCustomerID: "cus_candidate_preview",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Zero(t, fake.customerLookupCount())
}

func TestGetStripeCustomer_RejectsSubscriptionWithoutCustomerBeforeProviderLookup(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_preview_subscription", name: "Preview Subscription", slug: "preview-subscription"})
	_, err := usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID: "org_preview_subscription", TumMonthlyTokenLimit: pgtype.Int8{}, AlertEmail: pgtype.Text{},
		BillingCycleAnchorDay: 1, TunneledMcpServerLimit: pgtype.Int4{},
	})
	require.NoError(t, err)
	require.NoError(t, usagerepo.New(conn).SetStripeSubscriptionFixture(ctx, usagerepo.SetStripeSubscriptionFixtureParams{
		OrganizationID: "org_preview_subscription", StripeSubscriptionID: conv.ToPGText("sub_existing_preview"),
	}))

	_, err = svc.GetStripeCustomer(ctx, &gen.GetStripeCustomerPayload{
		OrganizationID: "org_preview_subscription", StripeCustomerID: "cus_candidate_preview",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Zero(t, fake.customerLookupCount())
}

func TestSetStripeCustomer_LookupFailureDoesNotWrite(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	fake.customerErr = oops.E(oops.CodeGatewayError, errors.New("stripe upstream failed"), "Stripe customer lookup failed")
	seedOrg(t, ctx, conn, orgFixture{id: "org_customer_lookup_failure", name: "Lookup Failure", slug: "lookup-failure"})

	_, err := svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_customer_lookup_failure", StripeCustomerID: "cus_lookup_failure",
	})
	requireOopsCode(t, err, oops.CodeGatewayError)
	require.Equal(t, 1, fake.customerLookupCount())
	_, err = usagerepo.New(conn).GetBillingMetadata(ctx, "org_customer_lookup_failure")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSetStripeCustomer_CreatesBillingMetadataWithoutAuditEvent(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_set_customer", name: "Set Customer", slug: "set-customer"})
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "session-set-customer", Email: "operator@example.test", OIDCSubject: "oidc-set-customer", Name: "Test Operator", HD: "example.test",
	})
	countBefore, err := audittest.AuditLogCount(ctx, conn)
	require.NoError(t, err)

	result, err := svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_set_customer", StripeCustomerID: "cus_admin_set_1",
	})
	require.NoError(t, err)
	require.Equal(t, "org_set_customer", result.ID)
	require.Equal(t, new("cus_admin_set_1"), result.StripeCustomerID)
	require.Nil(t, result.StripeSubscriptionID)

	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_set_customer")
	require.NoError(t, err)
	require.Equal(t, "cus_admin_set_1", metadata.StripeCustomerID.String)

	countAfter, err := audittest.AuditLogCount(ctx, conn)
	require.NoError(t, err)
	require.Equal(t, countBefore, countAfter)
}

func TestSetStripeCustomer_PreservesExistingBillingSettings(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_preserve_billing", name: "Preserve Billing", slug: "preserve-billing"})
	before, err := usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID: "org_preserve_billing", TumMonthlyTokenLimit: pgtype.Int8{Int64: 9001, Valid: true},
		AlertEmail: conv.ToPGText("billing@example.test"), BillingCycleAnchorDay: 17,
		TunneledMcpServerLimit: pgtype.Int4{Int32: 23, Valid: true},
	})
	require.NoError(t, err)

	result, err := svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_preserve_billing", StripeCustomerID: "cus_preserve_1",
	})
	require.NoError(t, err)
	require.Equal(t, new("cus_preserve_1"), result.StripeCustomerID)

	after, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_preserve_billing")
	require.NoError(t, err)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, before.TumMonthlyTokenLimit, after.TumMonthlyTokenLimit)
	require.Equal(t, before.AlertEmail, after.AlertEmail)
	require.Equal(t, before.BillingCycleAnchorDay, after.BillingCycleAnchorDay)
	require.Equal(t, before.TunneledMcpServerLimit, after.TunneledMcpServerLimit)
}

func TestSetStripeCustomer_RejectsExistingCustomerIncludingSameValueRetry(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_existing_customer", name: "Existing Customer", slug: "existing-customer"})
	first, err := svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_existing_customer", StripeCustomerID: "cus_existing_1",
	})
	require.NoError(t, err)
	require.Equal(t, new("cus_existing_1"), first.StripeCustomerID)

	_, err = svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_existing_customer", StripeCustomerID: "cus_existing_1",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_existing_customer", StripeCustomerID: "cus_replacement_1",
	})
	requireOopsCode(t, err, oops.CodeConflict)

	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_existing_customer")
	require.NoError(t, err)
	require.Equal(t, "cus_existing_1", metadata.StripeCustomerID.String)
}

func TestSetStripeCustomer_RejectsSubscriptionWithoutCustomer(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_subscription_only", name: "Subscription Only", slug: "subscription-only"})
	_, err := usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID: "org_subscription_only", TumMonthlyTokenLimit: pgtype.Int8{}, AlertEmail: pgtype.Text{},
		BillingCycleAnchorDay: 1, TunneledMcpServerLimit: pgtype.Int4{},
	})
	require.NoError(t, err)
	require.NoError(t, usagerepo.New(conn).SetStripeSubscriptionFixture(ctx, usagerepo.SetStripeSubscriptionFixtureParams{
		OrganizationID: "org_subscription_only", StripeSubscriptionID: conv.ToPGText("sub_existing_1"),
	}))

	_, err = svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_subscription_only", StripeCustomerID: "cus_blocked_1",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_subscription_only")
	require.NoError(t, err)
	require.False(t, metadata.StripeCustomerID.Valid)
	require.Equal(t, "sub_existing_1", metadata.StripeSubscriptionID.String)
}

func TestSetStripeCustomer_ConcurrentAssignmentsOnlyOneSucceeds(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_customer_race", name: "Customer Race", slug: "customer-race"})

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, customerID := range []string{"cus_race_a", "cus_race_b"} {
		go func() {
			ready.Done()
			<-start
			_, callErr := svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
				OrganizationID: "org_customer_race", StripeCustomerID: customerID,
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

	metadata, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_customer_race")
	require.NoError(t, err)
	require.Contains(t, []string{"cus_race_a", "cus_race_b"}, metadata.StripeCustomerID.String)
}

func TestSetStripeCustomer_RejectsCustomerOwnedByAnotherOrganization(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_customer_owner", name: "Customer Owner", slug: "customer-owner"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_customer_contender", name: "Customer Contender", slug: "customer-contender"})
	_, err := svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_customer_owner", StripeCustomerID: "cus_unique_owner",
	})
	require.NoError(t, err)

	_, err = svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_customer_contender", StripeCustomerID: "cus_unique_owner",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = usagerepo.New(conn).GetBillingMetadata(ctx, "org_customer_contender")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestStripeCustomerEndpoints_RejectInvalidCustomerIDsBeforeProviderLookup(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	seedOrg(t, ctx, conn, orgFixture{id: "org_invalid_customer", name: "Invalid Customer", slug: "invalid-customer"})

	for _, customerID := range []string{"", "customer_123", " cus_space", "cus_trailing ", "cus_bad-hyphen", "cus_" + strings.Repeat("a", 252)} {
		_, err := svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
			OrganizationID: "org_invalid_customer", StripeCustomerID: customerID,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
		_, err = svc.GetStripeCustomer(ctx, &gen.GetStripeCustomerPayload{
			OrganizationID: "org_invalid_customer", StripeCustomerID: customerID,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	require.Zero(t, fake.customerLookupCount())
	_, err := usagerepo.New(conn).GetBillingMetadata(ctx, "org_invalid_customer")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSetStripeCustomer_RejectsMissingOrganizationBeforeProviderLookup(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)

	_, err := svc.SetStripeCustomer(ctx, &gen.SetStripeCustomerPayload{
		OrganizationID: "org_missing_customer", StripeCustomerID: "cus_missing_org",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Zero(t, fake.customerLookupCount())
}
