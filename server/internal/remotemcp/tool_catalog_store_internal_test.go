package remotemcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tooldisposition"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestToolCatalogStore_UncachedOptionReadsPastTheCache(t *testing.T) {
	t.Parallel()

	cached := tooldisposition.New(testenv.NewLogger(t), nil, cache.NoopCache)
	manager := &ProxyManager{toolDispositions: cached}

	require.Same(t, cached, manager.toolCatalogStore(newProxyBuildOptions(nil)), "proxies read through the cache by default")

	store := manager.toolCatalogStore(newProxyBuildOptions([]BuildOption{WithUncachedToolCatalog()}))
	require.IsType(t, &tooldisposition.Uncached{}, store, "sign-in and consent proxies read Postgres")
}

func TestToolCatalogStore_UncachedOptionKeepsAStoreWithoutAnUncachedView(t *testing.T) {
	t.Parallel()

	fake := &fakeToolCatalogStore{catalog: map[string]string{}}
	manager := &ProxyManager{toolDispositions: fake}

	require.Same(t, fake, manager.toolCatalogStore(newProxyBuildOptions([]BuildOption{WithUncachedToolCatalog()})))
}
