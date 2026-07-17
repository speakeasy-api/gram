package aiintegrations

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
)

func TestUpsertWithTxCreatesConfigGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	result := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, &watermark)
	require.True(t, result.CreatedNewGeneration)
	require.NotNil(t, result.Row)
	require.Equal(t, result.Row.ID, result.Config.ID)
	require.Equal(t, watermark.UTC().Add(usagePollIntervalFor(ProviderCursor)), result.Config.NextPollAfter)
	require.Equal(t, watermark, result.Config.PollWatermarkAt)

	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func TestUpsertWithTxSettingsUpdateKeepsConfigGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, &watermark)

	updated := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "", false, false, nil, nil)
	require.False(t, updated.CreatedNewGeneration)
	require.Equal(t, created.Config.ID, updated.Config.ID)
	require.False(t, updated.Config.Enabled)

	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func TestUpsertWithTxKeyReplacementCreatesNewConfigGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, &watermark)

	replacedWatermark := time.Now().UTC().Add(-initialUsagePollLookback)
	replaced := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "new-cursor-key", true, true, nil, &replacedWatermark)
	require.True(t, replaced.CreatedNewGeneration)
	require.NotEqual(t, created.Config.ID, replaced.Config.ID)

	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
	require.Equal(t, int64(2), countAIIntegrationConfigs(t, ctx, conn, orgID, true))
}

func TestUpsertWithTxCreatesPrimarySyncDiscriminators(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	cursor := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, nil)
	cursorSync := getPrimarySyncDiscriminators(t, ctx, conn, cursor.Config)
	require.Equal(t, ProviderCursor, cursorSync.Schedule)
	require.Equal(t, "time", cursorSync.Kind)
	require.Equal(t, int64(1), countSyncRows(t, ctx, conn, cursor.Config))

	extOrgID := "org_ext_primary_sync"
	anthropic := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)
	anthropicSync := getPrimarySyncDiscriminators(t, ctx, conn, anthropic.Config)
	require.Equal(t, ProviderAnthropicCompliance, anthropicSync.Schedule)
	require.Equal(t, "cursor", anthropicSync.Kind)
	require.Equal(t, int64(1), countSyncRows(t, ctx, conn, anthropic.Config))
}

func TestEnsurePrimarySyncHandlesConcurrentFirstWriters(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, nil)
	require.NoError(t, repo.New(conn).DeleteSyncRowsForTest(ctx, repo.DeleteSyncRowsForTestParams{
		AiIntegrationConfigID: created.Config.ID,
		ProjectID:             created.Config.ProjectID,
	}))

	const writers = 8
	start := make(chan struct{})
	errs := make(chan error, writers)
	ids := make(chan uuid.UUID, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			<-start
			row, err := store.ensurePrimarySync(ctx, conn, created.Config.ID, created.Config.ProjectID)
			errs <- err
			ids <- row.ID
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	close(ids)

	for err := range errs {
		require.NoError(t, err)
	}
	var syncID uuid.UUID
	for id := range ids {
		require.NotEqual(t, uuid.Nil, id)
		if syncID == uuid.Nil {
			syncID = id
		}
		require.Equal(t, syncID, id)
	}
	require.Equal(t, int64(1), countSyncRows(t, ctx, conn, created.Config))
}

func TestRecordUsagePollFailureStoresErrorAsData(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, &watermark)

	message := `cursor rejected the configured api key'; DROP TABLE ai_integration_syncs; -- <script>alert(1)</script>`
	require.NoError(t, store.RecordUsagePollFailure(ctx, created.Config.ID, ProviderCursor, time.Now(), errors.New(message)))

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
	externalOrganizationID *string,
	resetPollWatermarkAt *time.Time,
) UpsertResult {
	t.Helper()

	var result UpsertResult
	require.NoError(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var err error
		result, err = store.upsertWithTx(ctx, tx, orgID, provider, apiKey, apiKeySupplied, enabled, externalOrganizationID, nil, resetPollWatermarkAt)
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

func getPrimarySyncDiscriminators(t *testing.T, ctx context.Context, conn *pgxpool.Pool, cfg Config) repo.GetPrimarySyncDiscriminatorsForTestRow {
	t.Helper()

	row, err := repo.New(conn).GetPrimarySyncDiscriminatorsForTest(ctx, repo.GetPrimarySyncDiscriminatorsForTestParams{
		AiIntegrationConfigID: cfg.ID,
		ProjectID:             cfg.ProjectID,
	})
	require.NoError(t, err)
	return row
}

func countSyncRows(t *testing.T, ctx context.Context, conn *pgxpool.Pool, cfg Config) int64 {
	t.Helper()

	count, err := repo.New(conn).CountSyncRowsForTest(ctx, repo.CountSyncRowsForTestParams{
		AiIntegrationConfigID: cfg.ID,
		ProjectID:             cfg.ProjectID,
	})
	require.NoError(t, err)
	return count
}
