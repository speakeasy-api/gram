package aiintegrations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
)

func TestInitialUsagePollWatermarkBackfillsOneHour(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 20, 12, 30, 0, 0, time.FixedZone("test", 2*60*60))

	require.Equal(t, now.UTC().Add(-time.Hour), initialUsagePollWatermark(now))
	require.Equal(t, now.UTC(), nextUsagePollAfter(initialUsagePollWatermark(now)))
}

func TestTruncateUsagePollError(t *testing.T) {
	t.Parallel()

	require.Empty(t, truncateUsagePollError(nil))
	require.Equal(t, "cursor unavailable", truncateUsagePollError(errors.New(" cursor unavailable ")))

	longErr := errors.New(strings.Repeat("x", maxUsagePollErrorMessage+1))
	require.Len(t, truncateUsagePollError(longErr), maxUsagePollErrorMessage)
}

func TestUpsertWithTxCreatesConfigGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := initialUsagePollWatermark(time.Now())
	result := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, &watermark)
	require.True(t, result.CreatedNewGeneration)
	require.NotNil(t, result.Row)
	require.Equal(t, result.Row.ID, result.Config.ID)
	require.Equal(t, nextUsagePollAfter(watermark), result.Config.NextPollAfter)
	require.Equal(t, watermark, result.Config.PollWatermarkAt)

	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func TestUpsertWithTxSettingsUpdateKeepsConfigGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := initialUsagePollWatermark(time.Now())
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, &watermark)

	updated := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "", false, false, nil)
	require.False(t, updated.CreatedNewGeneration)
	require.Equal(t, created.Config.ID, updated.Config.ID)
	require.False(t, updated.Config.Enabled)

	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func TestUpsertWithTxKeyReplacementCreatesNewConfigGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := initialUsagePollWatermark(time.Now())
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, &watermark)

	replacedWatermark := initialUsagePollWatermark(time.Now())
	replaced := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "new-cursor-key", true, true, &replacedWatermark)
	require.True(t, replaced.CreatedNewGeneration)
	require.NotEqual(t, created.Config.ID, replaced.Config.ID)

	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
	require.Equal(t, int64(2), countAIIntegrationConfigs(t, ctx, conn, orgID, true))
}

func TestRecordUsagePollFailureStoresErrorAsData(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := initialUsagePollWatermark(time.Now())
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, &watermark)

	message := `cursor rejected the configured api key'; DROP TABLE ai_integration_syncs; -- <script>alert(1)</script>`
	require.NoError(t, store.RecordUsagePollFailure(ctx, created.Config.ID, time.Now(), errors.New(message)))

	cfg, _, err := store.loadForOrgAndProviderRow(ctx, orgID, ProviderCursor)
	require.NoError(t, err)
	require.Equal(t, message, cfg.LastPollError)
	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func upsertConfigWithTx(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	store *Store,
	orgID string,
	provider string,
	apiKey string,
	apiKeySupplied bool,
	enabled bool,
	resetPollWatermarkAt *time.Time,
) UpsertResult {
	t.Helper()

	var result UpsertResult
	require.NoError(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var err error
		result, err = store.upsertWithTx(ctx, tx, orgID, provider, apiKey, apiKeySupplied, enabled, "", resetPollWatermarkAt)
		return err
	}))
	return result
}

func countAIIntegrationConfigs(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, includeDeleted bool) int64 {
	t.Helper()

	count, err := repo.New(conn).CountConfigsByOrganization(ctx, repo.CountConfigsByOrganizationParams{
		OrganizationID: orgID,
		IncludeDeleted: includeDeleted,
	})
	require.NoError(t, err)
	return count
}
