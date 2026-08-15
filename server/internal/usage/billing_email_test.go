package usage

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func billingEmailAdminContext(t *testing.T, organizationID string) context.Context {
	t.Helper()
	email := "admin@example.test"
	sessionID := "session-billing-email-admin"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: organizationID,
		AccountType:          string(billing.TierEnterprise),
		UserID:               "user-billing-email-admin",
		Email:                &email,
		SessionID:            &sessionID,
	})
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, organizationID))
}

func setTestOrganizationAccountType(t *testing.T, db repo.DBTX, organizationID string, accountType billing.Tier) {
	t.Helper()
	require.NoError(t, orgrepo.New(db).SetAccountType(t.Context(), orgrepo.SetAccountTypeParams{
		GramAccountType: string(accountType),
		ID:              organizationID,
	}))
}

func TestBillingEmail_GetReturnsNullWithoutMetadata(t *testing.T) {
	t.Parallel()

	organizationID := "org-billing-email-empty"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)

	result, err := service.GetBillingEmail(billingEmailAdminContext(t, organizationID), &gen.GetBillingEmailPayload{})

	require.NoError(t, err)
	require.Nil(t, result.Email)
}

func TestBillingEmail_SetPreservesMetadataAndAuditsBeforeAfter(t *testing.T) {
	t.Parallel()

	organizationID := "org-billing-email-update"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	queries := repo.New(db)
	before, err := queries.UpsertBillingMetadata(t.Context(), repo.UpsertBillingMetadataParams{
		OrganizationID:         organizationID,
		TumMonthlyTokenLimit:   pgtype.Int8{Int64: 123456, Valid: true},
		AlertEmail:             pgtype.Text{String: "old@example.test", Valid: true},
		BillingCycleAnchorDay:  17,
		TunneledMcpServerLimit: pgtype.Int4{Int32: 9, Valid: true},
	})
	require.NoError(t, err)
	baseline, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionBillingMetadataUpdate)
	require.NoError(t, err)

	newEmail := "new@example.test"
	result, err := service.SetBillingEmail(billingEmailAdminContext(t, organizationID), &gen.SetBillingEmailPayload{Email: &newEmail})

	require.NoError(t, err)
	require.Equal(t, &newEmail, result.Email)
	after, err := queries.GetBillingMetadata(t.Context(), organizationID)
	require.NoError(t, err)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, before.TumMonthlyTokenLimit, after.TumMonthlyTokenLimit)
	require.Equal(t, before.TunneledMcpServerLimit, after.TunneledMcpServerLimit)
	require.Equal(t, before.BillingCycleAnchorDay, after.BillingCycleAnchorDay)
	require.Equal(t, before.StripeCustomerID, after.StripeCustomerID)
	require.Equal(t, newEmail, after.AlertEmail.String)
	require.True(t, after.AlertEmail.Valid)
	got, err := service.GetBillingEmail(billingEmailAdminContext(t, organizationID), &gen.GetBillingEmailPayload{})
	require.NoError(t, err)
	require.Equal(t, &newEmail, got.Email)

	auditCount, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionBillingMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, baseline+1, auditCount)
	record, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionBillingMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, "user-billing-email-admin", record.ActorID)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "old@example.test", beforeSnapshot["alert_email"])
	require.Equal(t, newEmail, afterSnapshot["alert_email"])
	require.Equal(t, beforeSnapshot["tum_monthly_token_limit"], afterSnapshot["tum_monthly_token_limit"])
	require.Equal(t, beforeSnapshot["tunneled_mcp_server_limit"], afterSnapshot["tunneled_mcp_server_limit"])
	require.Equal(t, beforeSnapshot["billing_cycle_anchor_day"], afterSnapshot["billing_cycle_anchor_day"])
}

func TestBillingEmail_SetNullClearsEmail(t *testing.T) {
	t.Parallel()

	organizationID := "org-billing-email-clear"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	_, err := repo.New(db).UpsertBillingMetadata(t.Context(), repo.UpsertBillingMetadataParams{
		OrganizationID:        organizationID,
		AlertEmail:            pgtype.Text{String: "billing@example.test", Valid: true},
		BillingCycleAnchorDay: 1,
	})
	require.NoError(t, err)

	result, err := service.SetBillingEmail(billingEmailAdminContext(t, organizationID), &gen.SetBillingEmailPayload{})

	require.NoError(t, err)
	require.Nil(t, result.Email)
	row, err := repo.New(db).GetBillingMetadata(t.Context(), organizationID)
	require.NoError(t, err)
	require.False(t, row.AlertEmail.Valid)
}

func TestBillingEmail_RejectsNonAdmin(t *testing.T) {
	t.Parallel()

	organizationID := "org-billing-email-member"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID))

	_, getErr := service.GetBillingEmail(ctx, &gen.GetBillingEmailPayload{})
	_, setErr := service.SetBillingEmail(ctx, &gen.SetBillingEmailPayload{})

	for _, err := range []error{getErr, setErr} {
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeForbidden, shareable.Code)
	}
}

func TestBillingEmail_UsesAuthoritativeAccountType(t *testing.T) {
	t.Parallel()

	organizationID := "org-billing-email-enterprise"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierEnterprise)
	ctx := billingEmailAdminContext(t, organizationID)

	_, getErr := service.GetBillingEmail(ctx, &gen.GetBillingEmailPayload{})
	_, setErr := service.SetBillingEmail(ctx, &gen.SetBillingEmailPayload{})

	for _, err := range []error{getErr, setErr} {
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeForbidden, shareable.Code)
	}
}
