package usage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type paygBillingSummaryTestInstance struct {
	service    *Service
	db         *pgxpool.Pool
	clickhouse driver.Conn
	stripe     *checkoutStripeClient
	orgID      string
	projectID  uuid.UUID
	start      time.Time
	end        time.Time
}

func newPaygBillingSummaryTestInstance(t *testing.T) *paygBillingSummaryTestInstance {
	t.Helper()

	orgID := "org-summary-" + uuid.NewString()[:8]
	service, db, clickhouse, projectID := newTUMTestService(t, orgID)
	require.NoError(t, orgrepo.New(db).SetAccountType(t.Context(), orgrepo.SetAccountTypeParams{
		GramAccountType: "payg",
		ID:              orgID,
	}))
	require.NoError(t, repo.New(db).CreateStripeSubscriptionBillingMetadataFixture(t.Context(), repo.CreateStripeSubscriptionBillingMetadataFixtureParams{
		OrganizationID:       orgID,
		StripeCustomerID:     pgtype.Text{String: "cus_summary", Valid: true},
		StripeSubscriptionID: pgtype.Text{String: "sub_summary", Valid: true},
	}))

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -3)
	end := today.AddDate(0, 1, 0)
	materializePaygSummaryKey(t, db, orgID, openrouter.KeyTypeChat, 'c', start.Add(-time.Hour))
	materializePaygSummaryKey(t, db, orgID, openrouter.KeyTypeInternal, 'i', start.Add(-time.Hour))
	stripe := newCheckoutStripeClient()
	stripe.subscriptionState = &stripeclient.SubscriptionState{
		ID:                           "sub_summary",
		CustomerID:                   "cus_summary",
		Status:                       "active",
		CurrentPeriodStart:           start,
		CurrentPeriodEnd:             end,
		TrialStart:                   time.Time{},
		TrialEnd:                     time.Time{},
		CancelAtPeriodEnd:            false,
		CancelAt:                     time.Time{},
		CanceledAt:                   time.Time{},
		LatestInvoiceID:              "in_summary",
		LatestInvoiceStatus:          "paid",
		LatestInvoiceAmountRemaining: 0,
		PaymentFailed:                false,
	}
	service.stripeClient = stripe

	return &paygBillingSummaryTestInstance{
		service:    service,
		db:         db,
		clickhouse: clickhouse,
		stripe:     stripe,
		orgID:      orgID,
		projectID:  projectID,
		start:      start,
		end:        end,
	}
}

func (ti *paygBillingSummaryTestInstance) context(t *testing.T, grants ...authz.Grant) context.Context {
	t.Helper()
	sessionID := "session-summary"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: ti.orgID,
		AccountType:          "enterprise",
		UserID:               "user-summary",
		SessionID:            &sessionID,
	})
	return authztest.WithExactGrants(t, ctx, grants...)
}

func materializePaygSummaryKey(t *testing.T, db *pgxpool.Pool, orgID string, keyType openrouter.KeyType, hashRune rune, createdAt time.Time) {
	t.Helper()
	_, err := openrouterrepo.New(db).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        strings.Repeat(string(hashRune), 64),
		MonthlyCredits: 0,
	})
	require.NoError(t, err)
	require.NoError(t, repo.New(db).SetOpenRouterAPIKeyCreatedAtFixture(t.Context(), repo.SetOpenRouterAPIKeyCreatedAtFixtureParams{
		CreatedAt:      pgtype.Timestamptz{Time: createdAt, InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: orgID,
		KeyType:        string(keyType),
	}))
}

func setPaygSummaryKeysCreatedAt(t *testing.T, db *pgxpool.Pool, orgID string, createdAt time.Time) {
	t.Helper()
	require.NoError(t, repo.New(db).SetOpenRouterAPIKeysCreatedAtFixture(t.Context(), repo.SetOpenRouterAPIKeysCreatedAtFixtureParams{
		CreatedAt:      pgtype.Timestamptz{Time: createdAt, InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: orgID,
	}))
}

func upsertPaygSummarySpend(t *testing.T, db *pgxpool.Pool, orgID string, day time.Time, amount string) {
	t.Helper()
	upsertPaygSummarySpendForKey(t, db, orgID, openrouter.KeyTypeChat, day, amount)
}

func upsertPaygSummarySpendForKey(t *testing.T, db *pgxpool.Pool, orgID string, keyType openrouter.KeyType, day time.Time, amount string) {
	t.Helper()
	var spend pgtype.Numeric
	require.NoError(t, spend.Scan(amount))
	require.NoError(t, repo.New(db).UpsertOpenRouterDailySpendFixture(t.Context(), repo.UpsertOpenRouterDailySpendFixtureParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
		Day:            pgtype.Date{Time: day, InfinityModifier: pgtype.Finite, Valid: true},
		SpendUsd:       spend,
	}))
}

func TestGetPaygBillingSummaryRequiresOrganizationRead(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	_, err := ti.service.GetPaygBillingSummary(ti.context(t), &gen.GetPaygBillingSummaryPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetPaygBillingSummaryForOrganizationUsesExplicitID(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	result, err := ti.service.GetPaygBillingSummaryForOrganization(t.Context(), ti.orgID)
	require.NoError(t, err)
	assert.Equal(t, ti.start.Format(time.RFC3339), result.PeriodStart)
}

func TestGetPaygBillingSummaryReturnsExactCycleAlignedEstimate(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	insertObservedClaudeAggregateRow(t, ti.clickhouse, ti.projectID.String(), ti.start.Add(time.Hour), 1_000_000)

	upsertPaygSummarySpend(t, ti.db, ti.orgID, ti.start.AddDate(0, 0, -1), "99.000000")
	upsertPaygSummarySpend(t, ti.db, ti.orgID, ti.start, "1.200000")
	upsertPaygSummarySpend(t, ti.db, ti.orgID, ti.start.AddDate(0, 0, 1), "2.345678")
	upsertPaygSummarySpend(t, ti.db, ti.orgID, ti.start.AddDate(0, 0, 2), "0.000001")
	upsertPaygSummarySpendForKey(t, ti.db, ti.orgID, openrouter.KeyTypeInternal, ti.start, "0.500000")
	upsertPaygSummarySpendForKey(t, ti.db, ti.orgID, openrouter.KeyTypeInternal, ti.start.AddDate(0, 0, 1), "0.250000")
	upsertPaygSummarySpendForKey(t, ti.db, ti.orgID, openrouter.KeyType("future"), ti.start, "100.000000")

	ctx := ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID))
	result, err := ti.service.GetPaygBillingSummary(ctx, &gen.GetPaygBillingSummaryPayload{})
	require.NoError(t, err)
	assert.Equal(t, ti.start.Format(time.RFC3339), result.PeriodStart)
	assert.Equal(t, ti.end.Format(time.RFC3339), result.PeriodEnd)
	assert.Equal(t, int64(1_000_000), result.TumTokens)
	assert.Equal(t, "0.00000035", result.TumUnitPriceUsd)
	assert.Equal(t, "0.35000000", result.TumCostUsd)
	assert.Equal(t, "4.295678", result.OtherInferenceSpendUsd)
	if assert.NotNil(t, result.RecordedThrough) {
		assert.Equal(t, ti.start.AddDate(0, 0, 1).Format(time.DateOnly), *result.RecordedThrough)
	}
	assert.Equal(t, "4.64567800", result.EstimatedTotalUsd)
}

func TestGetPaygBillingSummaryRequiresSpendForEveryApplicableMaterializedKey(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	upsertPaygSummarySpend(t, ti.db, ti.orgID, ti.start, "1.000000")

	ctx := ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID))
	result, err := ti.service.GetPaygBillingSummary(ctx, &gen.GetPaygBillingSummaryPayload{})
	require.NoError(t, err)
	assert.Nil(t, result.RecordedThrough)
	assert.Equal(t, "0.000000", result.OtherInferenceSpendUsd)

	upsertPaygSummarySpendForKey(t, ti.db, ti.orgID, openrouter.KeyTypeInternal, ti.start, "0.000000")
	result, err = ti.service.GetPaygBillingSummary(ctx, &gen.GetPaygBillingSummaryPayload{})
	require.NoError(t, err)
	if assert.NotNil(t, result.RecordedThrough) {
		assert.Equal(t, ti.start.Format(time.DateOnly), *result.RecordedThrough)
	}
	assert.Equal(t, "1.000000", result.OtherInferenceSpendUsd)
}

func TestGetPaygBillingSummaryCostsUsesCompletedDaysWithinPeriod(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	periodStart := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	completedBefore := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	setPaygSummaryKeysCreatedAt(t, ti.db, ti.orgID, periodStart.Add(-time.Hour))
	upsertPaygSummarySpend(t, ti.db, ti.orgID, periodStart.AddDate(0, 0, -1), "99.000000")
	upsertPaygSummarySpend(t, ti.db, ti.orgID, periodStart, "1.200000")
	upsertPaygSummarySpend(t, ti.db, ti.orgID, completedBefore.AddDate(0, 0, -1), "2.345678")
	upsertPaygSummarySpend(t, ti.db, ti.orgID, completedBefore, "88.000000")
	upsertPaygSummarySpend(t, ti.db, ti.orgID, periodEnd, "77.000000")
	upsertPaygSummarySpendForKey(t, ti.db, ti.orgID, openrouter.KeyTypeInternal, periodStart, "0.500000")
	upsertPaygSummarySpendForKey(t, ti.db, ti.orgID, openrouter.KeyTypeInternal, completedBefore.AddDate(0, 0, -1), "0.250000")
	upsertPaygSummarySpendForKey(t, ti.db, ti.orgID, openrouter.KeyType("future"), periodStart, "100.000000")

	costs, err := repo.New(ti.db).GetPaygBillingSummaryCosts(t.Context(), repo.GetPaygBillingSummaryCostsParams{
		TumTokens:        0,
		TumUnitPriceUsd:  TUMUnitPriceUSD,
		OrganizationID:   ti.orgID,
		BillableKeyTypes: openrouter.BillableKeyTypeStrings(),
		PeriodStart:      finiteTimestamptz(periodStart),
		PeriodEnd:        finiteTimestamptz(periodEnd),
		CompletedBefore:  finiteTimestamptz(completedBefore),
	})
	require.NoError(t, err)
	assert.Equal(t, "4.295678", costs.OtherInferenceSpendUsd)
	assert.True(t, costs.RecordedThrough.Valid)
	assert.Equal(t, completedBefore.AddDate(0, 0, -1), costs.RecordedThrough.Time.UTC())
}

func TestGetPaygBillingSummaryRejectsTrialingSubscription(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	ti.stripe.subscriptionState.Status = "trialing"

	_, err := ti.service.GetPaygBillingSummary(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetPaygBillingSummaryPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestGetPaygBillingSummaryAcceptsPastDueSubscription(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	ti.stripe.subscriptionState.Status = "past_due"

	result, err := ti.service.GetPaygBillingSummary(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetPaygBillingSummaryPayload{},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.TumTokens)
	assert.Equal(t, "0.00000000", result.EstimatedTotalUsd)
}

func TestGetPaygBillingSummaryRejectsPreAnchorActiveSubscription(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	now := time.Now().UTC()
	futureAnchor := time.Date(now.Year(), now.Month(), now.Day()+2, 0, 0, 0, 0, time.UTC)
	ti.stripe.subscriptionState.CurrentPeriodStart = futureAnchor
	ti.stripe.subscriptionState.CurrentPeriodEnd = futureAnchor.AddDate(0, 1, 0)

	_, err := ti.service.GetPaygBillingSummary(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetPaygBillingSummaryPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestGetPaygBillingSummaryRejectsExpiredPeriod(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	ti.stripe.subscriptionState.CurrentPeriodStart = today.AddDate(0, -2, 0)
	ti.stripe.subscriptionState.CurrentPeriodEnd = today.AddDate(0, -1, 0)

	_, err := ti.service.GetPaygBillingSummary(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetPaygBillingSummaryPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestGetPaygBillingSummaryRejectsNonMidnightPeriod(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	ti.stripe.subscriptionState.CurrentPeriodStart = ti.start.Add(time.Hour)

	_, err := ti.service.GetPaygBillingSummary(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetPaygBillingSummaryPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestGetPaygBillingSummaryUsesExactPeriodBoundaries(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	insertObservedClaudeAggregateRow(t, ti.clickhouse, ti.projectID.String(), ti.start.Add(-time.Nanosecond), 10)
	insertObservedClaudeAggregateRow(t, ti.clickhouse, ti.projectID.String(), ti.start, 20)
	insertObservedClaudeAggregateRow(t, ti.clickhouse, ti.projectID.String(), ti.end.Add(-time.Nanosecond), 30)
	insertObservedClaudeAggregateRow(t, ti.clickhouse, ti.projectID.String(), ti.end, 40)

	ctx := ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID))
	result, err := ti.service.GetPaygBillingSummary(ctx, &gen.GetPaygBillingSummaryPayload{})
	require.NoError(t, err)
	assert.Equal(t, int64(50), result.TumTokens)
}

func TestGetPaygBillingSummaryRejectsUnsafeJSONTokenCount(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	insertObservedClaudeAggregateRow(t, ti.clickhouse, ti.projectID.String(), ti.start, maxJSONSafeInteger+1)

	_, err := ti.service.GetPaygBillingSummary(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetPaygBillingSummaryPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnexpected)
}

func TestGetPaygBillingSummaryRejectsUnsafeJSONTokenSum(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	insertObservedClaudeAggregateRow(t, ti.clickhouse, ti.projectID.String(), ti.start, maxJSONSafeInteger)
	insertObservedClaudeAggregateRow(t, ti.clickhouse, ti.projectID.String(), ti.start.AddDate(0, 0, 1), 1)

	_, err := ti.service.GetPaygBillingSummary(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetPaygBillingSummaryPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnexpected)
}

func TestGetPaygBillingSummaryRejectsNonPaygOrganization(t *testing.T) {
	t.Parallel()

	ti := newPaygBillingSummaryTestInstance(t)
	require.NoError(t, orgrepo.New(ti.db).SetAccountType(t.Context(), orgrepo.SetAccountTypeParams{
		GramAccountType: "free",
		ID:              ti.orgID,
	}))

	_, err := ti.service.GetPaygBillingSummary(
		ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)),
		&gen.GetPaygBillingSummaryPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}
