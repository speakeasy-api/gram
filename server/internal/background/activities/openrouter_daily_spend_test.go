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
	result, err := act.DoWithResult(ctx, activities.CollectOpenRouterDailySpendArgs{StartDay: startDay, EndDay: endDay})
	require.NoError(t, err)
	require.Empty(t, result.ReadyOrganizationIDs)

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

func TestCollectOpenRouterDailySpend_RejectsUnrepresentableSpend(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		dbName  string
		spend   string
		errText string
	}{
		"fractional scale": {
			dbName:  "openrouter_daily_spend_fractional_scale",
			spend:   "0.0000001",
			errText: "more than 6 non-zero fractional digits",
		},
		"integer precision": {
			dbName:  "openrouter_daily_spend_integer_precision",
			spend:   "100000000",
			errText: "exceeds NUMERIC(14,6) precision",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			conn, orgID := setupOpenRouterDailySpendTest(t, test.dbName)
			chatHash := createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeChat, 'e')
			startDay := utcDay(0)
			endDay := utcDay(1)

			client := &mockOpenRouterSpendClient{}
			client.On("GetDailySpend", mock.Anything, chatHash, startDay, endDay).
				Return(openrouter.DailySpendResult{
					Days:   []openrouter.DailySpendDay{{Day: startDay, SpendUSD: test.spend}},
					Source: openrouter.DailySpendSourceAnalytics,
				}, nil).Once()

			act := activities.NewCollectOpenRouterDailySpend(testenv.NewLogger(t), conn, client)
			result, err := act.DoWithResult(ctx, activities.CollectOpenRouterDailySpendArgs{StartDay: startDay, EndDay: endDay})
			require.NoError(t, err)
			require.Empty(t, result.ReadyOrganizationIDs, test.errText)

			rows, queryErr := backgroundrepo.New(conn).ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
				OrganizationID: orgID,
				KeyType:        string(openrouter.KeyTypeChat),
			})
			require.NoError(t, queryErr)
			require.Empty(t, rows)
			client.AssertExpectations(t)
		})
	}
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

func TestCollectOpenRouterDailySpend_RecoversStaleInvoiceDayBeyond96Hours(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, orgID := setupOpenRouterDailySpendTest(t, "openrouter_daily_spend_stale_recovery")
	keyHash := createOpenRouterSpendTarget(t, conn, orgID, openrouter.KeyTypeChat, '1')
	queries := backgroundrepo.New(conn)

	invoiceDay := utcDay(-7)
	require.NoError(t, queries.SetOpenRouterAPIKeyCreatedAtFixture(ctx, backgroundrepo.SetOpenRouterAPIKeyCreatedAtFixtureParams{
		CreatedAt:      pgtype.Timestamptz{Time: invoiceDay, InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: orgID,
		KeyType:        "chat",
	}))
	require.NoError(t, queries.CreateStripeInvoiceFixture(ctx, backgroundrepo.CreateStripeInvoiceFixtureParams{
		StripeInvoiceID:      "in_stale",
		OrganizationID:       pgtype.Text{String: orgID, Valid: true},
		StripeCustomerID:     "cus_stale",
		StripeSubscriptionID: "sub_stale",
		ServicePeriodStart:   pgtype.Timestamptz{Time: invoiceDay, InfinityModifier: pgtype.Finite, Valid: true},
		ServicePeriodEnd:     pgtype.Timestamptz{Time: invoiceDay.AddDate(0, 0, 1), InfinityModifier: pgtype.Finite, Valid: true},
		InvoiceState:         "open",
		FinalizedAt:          pgtype.Timestamptz{Time: invoiceDay.AddDate(0, 0, 1), InfinityModifier: pgtype.Finite, Valid: true},
	}))
	var stale pgtype.Numeric
	require.NoError(t, stale.Scan("1"))
	require.NoError(t, queries.UpsertOpenRouterDailySpend(ctx, backgroundrepo.UpsertOpenRouterDailySpendParams{
		TargetOrganizationID: orgID,
		TargetKeyType:        string(openrouter.KeyTypeChat),
		TargetDay:            pgtype.Date{Time: invoiceDay, Valid: true},
		TargetSpendUsd:       stale,
	}))

	client := &mockOpenRouterSpendClient{}
	client.On("GetDailySpend", mock.Anything, keyHash, invoiceDay, utcDay(0)).
		Return(openrouter.DailySpendResult{
			Days:   []openrouter.DailySpendDay{{Day: invoiceDay, SpendUSD: "2.5"}},
			Source: openrouter.DailySpendSourceAnalytics,
		}, nil).Once()
	act := activities.NewCollectOpenRouterDailySpend(testenv.NewLogger(t), conn, client)
	result, err := act.DoWithResult(ctx, activities.CollectOpenRouterDailySpendArgs{StartDay: utcDay(-4), EndDay: utcDay(0)})
	require.NoError(t, err)
	require.Equal(t, []string{orgID}, result.ReadyOrganizationIDs)
	rows, err := queries.ListOpenRouterDailySpend(ctx, backgroundrepo.ListOpenRouterDailySpendParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.Equal(t, "2.500000", numericString(t, rows[0].SpendUsd))
	client.AssertExpectations(t)
}

func TestCollectOpenRouterDailySpend_ReturnsHealthyOrganizationWhenPeerFails(t *testing.T) {
	t.Parallel()
	conn, failedOrgID := setupOpenRouterDailySpendTest(t, "openrouter_daily_spend_org_isolation")
	failedHash := createOpenRouterSpendTarget(t, conn, failedOrgID, openrouter.KeyTypeChat, '3')
	healthyOrgID := "org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: healthyOrgID, Name: "Healthy", Slug: healthyOrgID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	healthyHash := createOpenRouterSpendTarget(t, conn, healthyOrgID, openrouter.KeyTypeChat, '4')
	startDay := utcDay(0)
	endDay := utcDay(1)
	client := &mockOpenRouterSpendClient{}
	client.On("GetDailySpend", mock.Anything, failedHash, startDay, endDay).
		Return(openrouter.DailySpendResult{}, errors.New("one org unavailable")).Once()
	client.On("GetDailySpend", mock.Anything, healthyHash, startDay, endDay).
		Return(openrouter.DailySpendResult{Source: openrouter.DailySpendSourceAnalytics}, nil).Once()

	result, err := activities.NewCollectOpenRouterDailySpend(testenv.NewLogger(t), conn, client).
		DoWithResult(t.Context(), activities.CollectOpenRouterDailySpendArgs{StartDay: startDay, EndDay: endDay})
	require.NoError(t, err)
	require.Equal(t, []string{healthyOrgID}, result.ReadyOrganizationIDs)
	client.AssertExpectations(t)
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
