package tooldisposition

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type recordingCache struct {
	cache.Cache
	mu      sync.Mutex
	sets    []string
	deletes []string
}

func (c *recordingCache) Set(_ context.Context, key string, _ any, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sets = append(c.sets, key)
	return nil
}

func (c *recordingCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletes = append(c.deletes, key)
	return nil
}

func TestCacheUsesProjectQualifiedGeneration(t *testing.T) {
	t.Parallel()

	backend := &recordingCache{Cache: cache.NoopCache, sets: []string{}, deletes: []string{}}
	resolver := New(testenv.NewLogger(t), nil, backend)
	projectID := uuid.New()
	otherProjectID := uuid.New()
	serverID := uuid.New()
	key := cacheKey(projectID.String(), serverID.String())
	generation := resolver.generationFor(key)

	generation.mu.Lock()
	resolveGeneration := generation.value
	generation.mu.Unlock()

	require.NoError(t, resolver.Invalidate(t.Context(), projectID, serverID))
	resolver.storeResolved(t.Context(), generation, resolveGeneration, serverTools{
		ProjectID:    projectID.String(),
		MCPServerID:  serverID.String(),
		Dispositions: map[string]string{"write": "destructive"},
		Annotations:  nil,
	})

	otherKey := cacheKey(otherProjectID.String(), serverID.String())
	otherGeneration := resolver.generationFor(otherKey)
	otherGeneration.mu.Lock()
	otherResolveGeneration := otherGeneration.value
	otherGeneration.mu.Unlock()
	resolver.storeResolved(t.Context(), otherGeneration, otherResolveGeneration, serverTools{
		ProjectID:    otherProjectID.String(),
		MCPServerID:  serverID.String(),
		Dispositions: map[string]string{"read": "read_only"},
		Annotations:  nil,
	})

	backend.mu.Lock()
	defer backend.mu.Unlock()
	require.Equal(t, []string{key + ":"}, backend.deletes)
	require.Equal(t, []string{otherKey + ":"}, backend.sets)
}
