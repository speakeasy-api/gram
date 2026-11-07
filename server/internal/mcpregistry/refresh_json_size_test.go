package mcpregistry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCatalogRefreshCanonicalJSONSizeBoundary(t *testing.T) {
	t.Parallel()
	ctx, _, db := newTestService(t)
	tx := refreshBoundsTx(t, ctx, db)
	// PostgreSQL adds one separator space. This compact payload's canonical
	// jsonb text is exactly the database's 64 MiB storage bound.
	accepted := json.RawMessage(`{"v":"` + strings.Repeat("x", refreshStorageJSONLimit-9) + `"}`)
	require.Len(t, accepted, refreshStorageJSONLimit-1)
	require.NoError(t, refreshSaveState(ctx, tx, "canonical-boundary", accepted))
	var storedBytes int
	require.NoError(t, tx.QueryRow(ctx, `SELECT octet_length(state::text) FROM mcp_catalog_refresh_state WHERE source_identity = 'canonical-boundary'`).Scan(&storedBytes)) //nolint:glint // notestingrawsql: independently verify the real schema boundary without loading the large stored JSON into the test process.
	require.Equal(t, refreshStorageJSONLimit, storedBytes)

	// One more content byte still passes the compact transport guard, but must
	// be rejected before INSERT/UPDATE can abort the transaction with CHECK 23514.
	rejected := json.RawMessage(`{"v":"` + strings.Repeat("x", refreshStorageJSONLimit-8) + `"}`)
	require.Len(t, rejected, refreshStorageJSONLimit)
	const expected = "catalog refresh: catalog refresh canonical JSON exceeds 67108864 bytes"
	require.EqualError(t, refreshSaveState(ctx, tx, "canonical-boundary", rejected), expected)
	id := uuid.New()
	require.EqualError(t, refreshSavePlan(ctx, tx, id, "source", "owner", time.Now().Add(time.Hour), rejected), expected)
	require.NoError(t, refreshSavePlan(ctx, tx, id, "source", "owner", time.Now().Add(time.Hour), json.RawMessage(`{}`)))
	require.EqualError(t, refreshSaveReceipt(ctx, tx, id, rejected), expected)
	_, receipt, _, err := refreshLoadPlan(ctx, tx, id, "source", "owner")
	require.NoError(t, err, "canonical size rejection must not abort the transaction")
	require.Empty(t, receipt)
	require.NoError(t, tx.QueryRow(ctx, `SELECT octet_length(state::text) FROM mcp_catalog_refresh_state WHERE source_identity = 'canonical-boundary'`).Scan(&storedBytes)) //nolint:glint // notestingrawsql: assert rejected writes preserved the last accepted state.
	require.Equal(t, refreshStorageJSONLimit, storedBytes)
}
