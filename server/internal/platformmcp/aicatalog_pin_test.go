package platformmcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAICatalogPinMiddlewareReadsOneSnapshot(t *testing.T) {
	t.Parallel()

	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_ai_catalog_pin")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	store := aivendorstest.NewStore()
	reg := newRegistrar(newTestMCPServer())
	reg.withAICatalog(store)
	logger := testenv.NewLogger(t)
	var first, refreshed, second *aivendors.Snapshot
	handler := aiCatalogPinMiddleware(reg)(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		first = reg.aiCatalogSnapshot(ctx)
		require.NoError(t, store.Refresh(ctx, db, logger, aitargets.ValidateCatalog))
		refreshed = store.Snapshot()
		second = reg.aiCatalogSnapshot(ctx)
		return nil, nil
	})
	_, err = handler(t.Context(), "tools/list", nil)
	require.NoError(t, err)
	require.NotSame(t, first, refreshed, "the refresh must publish a new snapshot")
	require.Same(t, first, second)
}
