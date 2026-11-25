package externalmcp

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestServiceClearRegistryCacheWithoutLegacyClient(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	storage := newRegistryTestCache()
	registryURL := "https://registry.example.test"
	keys := []string{listCacheKey(registryURL, uuid.New()), fmt.Sprintf("registry:details:%s:%s:server", registryCacheSchemaVersion, registryURL)}
	for _, key := range keys {
		require.NoError(t, storage.Set(ctx, key, "cached", time.Hour))
	}
	require.NoError(t, storage.Set(ctx, "unrelated", "keep", time.Hour))
	service := &Service{logger: testenv.NewLogger(t), cacheImpl: storage}
	require.NoError(t, service.clearRegistryCache(ctx, registryURL))
	for _, key := range keys {
		var value string
		require.Error(t, storage.Get(ctx, key, &value))
	}
	var value string
	require.NoError(t, storage.Get(ctx, "unrelated", &value))
	require.Equal(t, "keep", value)
}
