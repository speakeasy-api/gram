package activities_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	backgroundrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

type mockOpenRouterSpendClient struct {
	mock.Mock
}

func (m *mockOpenRouterSpendClient) GetDailySpend(
	ctx context.Context,
	keyHash string,
	startDay time.Time,
	endDay time.Time,
) (openrouter.DailySpendResult, error) {
	args := m.Called(ctx, keyHash, startDay, endDay)
	result, _ := args.Get(0).(openrouter.DailySpendResult)
	return result, args.Error(1)
}

func setupOpenRouterDailySpendTest(t *testing.T, dbName string) (*pgxpool.Pool, string) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Spend Test Organization",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	return conn, orgID
}

func createOpenRouterSpendTarget(t *testing.T, conn *pgxpool.Pool, orgID string, keyType openrouter.KeyType, hashRune rune) string {
	t.Helper()

	keyHash := strings.Repeat(string(hashRune), 64)
	_, err := openrouterrepo.New(conn).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
		Key:            pgtype.Text{},
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        keyHash,
		MonthlyCredits: 0,
	})
	require.NoError(t, err)
	return keyHash
}

func utcDay(offset int) time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, offset)
}

func numericString(t *testing.T, value pgtype.Numeric) string {
	t.Helper()
	encoded, err := value.Value()
	require.NoError(t, err)
	text, ok := encoded.(string)
	require.True(t, ok)
	return text
}

func TestCollectOpenRouterDailySpend_StoresBothKeyTypesZerosAndRestatements(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, orgID := setupOpenRouterDailySpendTest(t, "openrouter_daily_spend_restatements")
	chatHash := createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeChat, 'a')
	internalHash := createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeInternal, 'b')

	err := openrouterrepo.New(conn).DisableOpenRouterAPIKey(ctx, openrouterrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	})
	require.NoError(t, err)

	startDay := utcDay(0)
	endDay := utcDay(1)
	client := &mockOpenRouterSpendClient{}
	client.On("GetDailySpend", mock.Anything, chatHash, startDay, endDay).
		Return(openrouter.DailySpendResult{
			Days:   []openrouter.DailySpendDay{{Day: startDay, SpendUSD: "1.250001"}},
			Source: openrouter.DailySpendSourceAnalytics,
		}, nil).Twice()
	client.On("GetDailySpend", mock.Anything, internalHash, startDay, endDay).
		Return(openrouter.DailySpendResult{Days: nil, Source: openrouter.DailySpendSourceActivity}, nil).Times(3)

	act := activities.NewCollectOpenRouterDailySpend(testenv.NewLogger(t), conn, client)
	args := activities.CollectOpenRouterDailySpendArgs{StartDay: startDay, EndDay: endDay}
	require.NoError(t, act.Do(ctx, args))

	queries := backgroundrepo.New(conn)
	chatRows, err := queries.ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.Len(t, chatRows, 1)
	require.Equal(t, "1.250001", numericString(t, chatRows[0].SpendUsd))

	internalRows, err := queries.ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	})
	require.NoError(t, err)
	require.Len(t, internalRows, 1, "an absent upstream row must be persisted as zero")
	require.Equal(t, "0", numericString(t, internalRows[0].SpendUsd))

	firstUpdatedAt := chatRows[0].UpdatedAt.Time
	require.NoError(t, act.Do(ctx, args))
	chatRows, err = queries.ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.Equal(t, firstUpdatedAt, chatRows[0].UpdatedAt.Time, "an identical replay must not touch updated_at")

	client.On("GetDailySpend", mock.Anything, chatHash, startDay, endDay).
		Return(openrouter.DailySpendResult{
			Days:   []openrouter.DailySpendDay{{Day: startDay, SpendUSD: "2.500002"}},
			Source: openrouter.DailySpendSourceAnalytics,
		}, nil).Once()
	require.NoError(t, act.Do(ctx, args))
	chatRows, err = queries.ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.Equal(t, "2.500002", numericString(t, chatRows[0].SpendUsd))
	require.True(t, chatRows[0].UpdatedAt.Time.After(firstUpdatedAt), "a restatement must advance updated_at")
	client.AssertExpectations(t)
}

func TestCollectOpenRouterDailySpend_ContinuesAfterUpstreamFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, orgID := setupOpenRouterDailySpendTest(t, "openrouter_daily_spend_partial_failure")
	chatHash := createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeChat, 'c')
	internalHash := createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeInternal, 'd')
	startDay := utcDay(0)
	endDay := utcDay(1)

	client := &mockOpenRouterSpendClient{}
	client.On("GetDailySpend", mock.Anything, chatHash, startDay, endDay).
		Return(openrouter.DailySpendResult{}, errors.New("management API unavailable")).Once()
	client.On("GetDailySpend", mock.Anything, internalHash, startDay, endDay).
		Return(openrouter.DailySpendResult{
			Days:   []openrouter.DailySpendDay{{Day: startDay, SpendUSD: "0.75"}},
			Source: openrouter.DailySpendSourceActivity,
		}, nil).Once()

	act := activities.NewCollectOpenRouterDailySpend(testenv.NewLogger(t), conn, client)
	err := act.Do(ctx, activities.CollectOpenRouterDailySpendArgs{StartDay: startDay, EndDay: endDay})
	require.ErrorContains(t, err, "management API unavailable")

	queries := backgroundrepo.New(conn)
	chatRows, err := queries.ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.Empty(t, chatRows)
	internalRows, err := queries.ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	})
	require.NoError(t, err)
	require.Len(t, internalRows, 1)
	require.Equal(t, "0.750000", numericString(t, internalRows[0].SpendUsd))
	client.AssertExpectations(t)
}

func TestCollectOpenRouterDailySpend_RollsBackOneKeysBatch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, orgID := setupOpenRouterDailySpendTest(t, "openrouter_daily_spend_atomic")
	chatHash := createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeChat, 'e')
	startDay := utcDay(0)
	endDay := utcDay(2)

	client := &mockOpenRouterSpendClient{}
	client.On("GetDailySpend", mock.Anything, chatHash, startDay, endDay).
		Return(openrouter.DailySpendResult{
			Days: []openrouter.DailySpendDay{
				{Day: startDay, SpendUSD: "1"},
				{Day: startDay.AddDate(0, 0, 1), SpendUSD: "999999999999999"},
			},
			Source: openrouter.DailySpendSourceAnalytics,
		}, nil).Once()

	act := activities.NewCollectOpenRouterDailySpend(testenv.NewLogger(t), conn, client)
	err := act.Do(ctx, activities.CollectOpenRouterDailySpendArgs{StartDay: startDay, EndDay: endDay})
	require.ErrorContains(t, err, "numeric field overflow")

	rows, queryErr := backgroundrepo.New(conn).ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, queryErr)
	require.Empty(t, rows, "a later day failure must roll back earlier days for the key")
	client.AssertExpectations(t)
}

func TestCollectOpenRouterDailySpend_ClampsToKeyCreationDay(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, orgID := setupOpenRouterDailySpendTest(t, "openrouter_daily_spend_creation_clamp")
	chatHash := createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeChat, 'f')
	requestedStart := utcDay(-1)
	createdDay := utcDay(0)
	endDay := utcDay(1)

	client := &mockOpenRouterSpendClient{}
	client.On("GetDailySpend", mock.Anything, chatHash, createdDay, endDay).
		Return(openrouter.DailySpendResult{Days: nil, Source: openrouter.DailySpendSourceAnalytics}, nil).Once()
	act := activities.NewCollectOpenRouterDailySpend(testenv.NewLogger(t), conn, client)
	require.NoError(t, act.Do(ctx, activities.CollectOpenRouterDailySpendArgs{StartDay: requestedStart, EndDay: endDay}))

	rows, err := backgroundrepo.New(conn).ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, createdDay, rows[0].Day.Time)
	client.AssertExpectations(t)
}

func TestCollectOpenRouterDailySpend_ReportsGapOlderThanRecoveryWindow(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, orgID := setupOpenRouterDailySpendTest(t, "openrouter_daily_spend_gap")
	createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeChat, '1')
	queries := backgroundrepo.New(conn)

	firstDay := utcDay(-6)
	for _, day := range []time.Time{firstDay, firstDay.AddDate(0, 0, 2)} {
		var spend pgtype.Numeric
		require.NoError(t, spend.Scan("1"))
		require.NoError(t, queries.UpsertOpenRouterDailySpend(ctx, backgroundrepo.UpsertOpenRouterDailySpendParams{
			TargetOrganizationID: orgID,
			TargetKeyType:        string(openrouter.KeyTypeChat),
			TargetDay:            pgtype.Date{Time: day, Valid: true},
			TargetSpendUsd:       spend,
		}))
	}

	client := &mockOpenRouterSpendClient{}
	act := activities.NewCollectOpenRouterDailySpend(testenv.NewLogger(t), conn, client)
	err := act.Do(ctx, activities.CollectOpenRouterDailySpendArgs{StartDay: utcDay(-3), EndDay: utcDay(0)})
	require.ErrorContains(t, err, "has 1 daily spend gaps before recovery window")
	client.AssertNotCalled(t, "GetDailySpend", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestCollectOpenRouterDailySpend_NewKeyHasNoHistoricalGap(t *testing.T) {
	t.Parallel()
	conn, orgID := setupOpenRouterDailySpendTest(t, "openrouter_daily_spend_new_key")
	createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeChat, '2')
	client := &mockOpenRouterSpendClient{}
	act := activities.NewCollectOpenRouterDailySpend(testenv.NewLogger(t), conn, client)

	require.NoError(t, act.Do(t.Context(), activities.CollectOpenRouterDailySpendArgs{
		StartDay: utcDay(-3),
		EndDay:   utcDay(0),
	}))
	client.AssertNotCalled(t, "GetDailySpend", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
