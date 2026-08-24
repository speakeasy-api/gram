package usage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// Mid-month so fixtures can include a completed day in the current month and
// an excluded "today" without depending on the wall clock.
var inferenceSpendHistoryInstant = time.Date(2026, time.August, 21, 15, 0, 0, 0, time.UTC)

func inferenceSpendHistoryContext(t *testing.T, organizationID string, grants ...authz.Grant) context.Context {
	t.Helper()
	sessionID := "session-inference-spend-history"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: organizationID,
		AccountType:          "enterprise",
		UserID:               "user-inference-spend-history",
		SessionID:            &sessionID,
	})
	return authztest.WithExactGrants(t, ctx, grants...)
}

func TestGetInferenceSpendHistoryRequiresOrganizationRead(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-history-forbidden"
	service, _, _, _ := newTUMTestService(t, organizationID)
	_, err := service.GetInferenceSpendHistory(
		inferenceSpendHistoryContext(t, organizationID),
		&gen.GetInferenceSpendHistoryPayload{},
	)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetInferenceSpendHistoryIncludesCurrentMonthWhenEmpty(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-history-empty"
	service, _, _, _ := newTUMTestService(t, organizationID)
	ctx := inferenceSpendHistoryContext(t, organizationID, authz.NewGrant(authz.ScopeOrgRead, organizationID))

	now := inferenceSpendHistoryInstant
	currentMonth := startOfUTCMonth(now)
	result, err := service.inferenceSpendHistoryAt(ctx, now)
	require.NoError(t, err)
	require.Len(t, result.Months, 1)

	assert.Equal(t, currentMonth.Format(time.RFC3339), result.Months[0].MonthStart)
	assert.Equal(t, currentMonth.AddDate(0, 1, 0).Format(time.RFC3339), result.Months[0].MonthEnd)
	assert.Equal(t, zeroInferenceSpendUSD, result.Months[0].SpendUsd)
	assert.Nil(t, result.Months[0].RecordedThrough)
	assert.True(t, result.Months[0].Current)
	assert.Empty(t, result.Months[0].KeySpend)
}

func TestGetInferenceSpendHistoryGroupsCompletedDaysByCalendarMonth(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-history-months"
	service, db, _, _ := newTUMTestService(t, organizationID)

	now := inferenceSpendHistoryInstant
	today := startOfUTCDay(now)
	currentMonth := startOfUTCMonth(now)
	previousMonth := currentMonth.AddDate(0, -1, 0)
	upsertPaygSummarySpendForKey(t, db, organizationID, openrouter.KeyTypeChat, currentMonth, "1.200000")
	upsertPaygSummarySpendForKey(t, db, organizationID, openrouter.KeyTypeInternal, currentMonth, "0.300000")
	upsertPaygSummarySpendForKey(t, db, organizationID, openrouter.KeyTypeChat, previousMonth, "4.000000")
	upsertPaygSummarySpendForKey(t, db, organizationID, openrouter.KeyTypeInternal, previousMonth.AddDate(0, 0, 1), "0.500000")
	upsertPaygSummarySpendForKey(t, db, organizationID, openrouter.KeyType("future"), previousMonth, "100.000000")
	upsertPaygSummarySpendForKey(t, db, organizationID, openrouter.KeyTypeChat, today, "9.000000")

	ctx := inferenceSpendHistoryContext(t, organizationID, authz.NewGrant(authz.ScopeOrgRead, organizationID))
	result, err := service.inferenceSpendHistoryAt(ctx, now)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Months), 2)

	assert.Equal(t, currentMonth.Format(time.RFC3339), result.Months[0].MonthStart)
	assert.True(t, result.Months[0].Current)
	assert.Equal(t, "1.500000", result.Months[0].SpendUsd)

	previous := result.Months[1]
	assert.Equal(t, previousMonth.Format(time.RFC3339), previous.MonthStart)
	assert.False(t, previous.Current)
	assert.Equal(t, "4.500000", previous.SpendUsd)
	require.Len(t, previous.KeySpend, 2)
	assert.Equal(t, string(openrouter.KeyTypeChat), previous.KeySpend[0].KeyType)
	assert.Equal(t, "4.000000", previous.KeySpend[0].SpendUsd)
	assert.Equal(t, string(openrouter.KeyTypeInternal), previous.KeySpend[1].KeyType)
	assert.Equal(t, "0.500000", previous.KeySpend[1].SpendUsd)
	require.NotNil(t, previous.RecordedThrough)
	assert.Equal(t, previousMonth.AddDate(0, 0, 1).Format(time.DateOnly), *previous.RecordedThrough)
}

func TestGetInferenceSpendHistoryOmitsMonthsWithoutDurableRows(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-history-gaps"
	service, db, _, _ := newTUMTestService(t, organizationID)

	now := inferenceSpendHistoryInstant
	currentMonth := startOfUTCMonth(now)
	olderMonth := currentMonth.AddDate(0, -2, 0)
	upsertPaygSummarySpendForKey(t, db, organizationID, openrouter.KeyTypeChat, olderMonth, "2.000000")

	ctx := inferenceSpendHistoryContext(t, organizationID, authz.NewGrant(authz.ScopeOrgRead, organizationID))
	result, err := service.inferenceSpendHistoryAt(ctx, now)
	require.NoError(t, err)
	require.Len(t, result.Months, 2)
	assert.True(t, result.Months[0].Current)
	assert.Equal(t, zeroInferenceSpendUSD, result.Months[0].SpendUsd)
	assert.Equal(t, olderMonth.Format(time.RFC3339), result.Months[1].MonthStart)
	assert.Equal(t, "2.000000", result.Months[1].SpendUsd)
}
