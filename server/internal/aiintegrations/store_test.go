package aiintegrations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
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
	ctx, conn, store, orgID := newStoreTestDB(t)

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	watermark := initialUsagePollWatermark(time.Now())
	result, err := store.upsertWithTx(ctx, tx, orgID, ProviderCursor, "cursor-key", true, true, &watermark)
	require.NoError(t, err)
	require.True(t, result.CreatedNewGeneration)
	require.NotNil(t, result.Row)
	require.Equal(t, result.Row.ID, result.Config.ID)
	require.Equal(t, nextUsagePollAfter(watermark), result.Config.NextPollAfter)
	require.Equal(t, watermark, result.Config.PollWatermarkAt)

	require.NoError(t, tx.Commit(ctx))
	require.Equal(t, 1, countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func TestUpsertWithTxSettingsUpdateKeepsConfigGeneration(t *testing.T) {
	ctx, conn, store, orgID := newStoreTestDB(t)

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	watermark := initialUsagePollWatermark(time.Now())
	created, err := store.upsertWithTx(ctx, tx, orgID, ProviderCursor, "cursor-key", true, true, &watermark)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	tx, err = conn.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	updated, err := store.upsertWithTx(ctx, tx, orgID, ProviderCursor, "", false, false, nil)
	require.NoError(t, err)
	require.False(t, updated.CreatedNewGeneration)
	require.Equal(t, created.Config.ID, updated.Config.ID)
	require.False(t, updated.Config.Enabled)
	require.NoError(t, tx.Commit(ctx))

	require.Equal(t, 1, countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func TestUpsertWithTxKeyReplacementCreatesNewConfigGeneration(t *testing.T) {
	ctx, conn, store, orgID := newStoreTestDB(t)

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	watermark := initialUsagePollWatermark(time.Now())
	created, err := store.upsertWithTx(ctx, tx, orgID, ProviderCursor, "cursor-key", true, true, &watermark)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	tx, err = conn.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	replacedWatermark := initialUsagePollWatermark(time.Now())
	replaced, err := store.upsertWithTx(ctx, tx, orgID, ProviderCursor, "new-cursor-key", true, true, &replacedWatermark)
	require.NoError(t, err)
	require.True(t, replaced.CreatedNewGeneration)
	require.NotEqual(t, created.Config.ID, replaced.Config.ID)
	require.NoError(t, tx.Commit(ctx))

	require.Equal(t, 1, countAIIntegrationConfigs(t, ctx, conn, orgID, false))
	require.Equal(t, 2, countAIIntegrationConfigs(t, ctx, conn, orgID, true))
}

func TestRecordUsagePollFailureStoresErrorAsData(t *testing.T) {
	ctx, conn, store, orgID := newStoreTestDB(t)

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	watermark := initialUsagePollWatermark(time.Now())
	created, err := store.upsertWithTx(ctx, tx, orgID, ProviderCursor, "cursor-key", true, true, &watermark)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	message := `cursor rejected the configured api key'; DROP TABLE ai_integration_syncs; -- <script>alert(1)</script>`
	require.NoError(t, store.RecordUsagePollFailure(ctx, created.Config.ID, time.Now(), errors.New(message)))

	cfg, _, err := store.loadForOrgAndProviderRow(ctx, orgID, ProviderCursor)
	require.NoError(t, err)
	require.Equal(t, message, cfg.LastPollError)
	require.Equal(t, 1, countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func countAIIntegrationConfigs(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, includeDeleted bool) int {
	t.Helper()

	query := `SELECT count(*) FROM ai_integration_configs WHERE organization_id = $1`
	if !includeDeleted {
		query += ` AND deleted IS FALSE`
	}
	var count int
	require.NoError(t, conn.QueryRow(ctx, query, orgID).Scan(&count))
	return count
}
