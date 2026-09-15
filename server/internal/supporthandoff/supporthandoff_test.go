package supporthandoff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"
)

type memoryCache struct {
	mu      sync.Mutex
	values  map[string][]byte
	expires map[string]time.Time
	now     time.Time
	lastKey string
}

func newMemoryCache() *memoryCache {
	return &memoryCache{values: map[string][]byte{}, expires: map[string]time.Time{}, now: time.Now()}
}

func (m *memoryCache) Get(_ context.Context, key string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.read(key, value, false)
}
func (m *memoryCache) GetAndDelete(_ context.Context, key string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.read(key, value, true)
}
func (m *memoryCache) read(key string, value any, remove bool) error {
	b, ok := m.values[key]
	if !ok || !m.now.Before(m.expires[key]) {
		delete(m.values, key)
		return redisCache.ErrCacheMiss
	}
	if remove {
		delete(m.values, key)
		delete(m.expires, key)
	}
	if err := json.Unmarshal(b, value); err != nil {
		return fmt.Errorf("decode cached value: %w", err)
	}
	return nil
}
func (m *memoryCache) Set(_ context.Context, key string, value any, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode cached value: %w", err)
	}
	m.values[key], m.expires[key], m.lastKey = b, m.now.Add(ttl), key
	return nil
}
func (m *memoryCache) Add(context.Context, string, time.Duration) (bool, error) {
	return false, errors.New("unused")
}
func (m *memoryCache) Update(context.Context, string, any) error { return errors.New("unused") }
func (m *memoryCache) Delete(context.Context, string) error      { return errors.New("unused") }
func (m *memoryCache) Expire(context.Context, string, time.Duration) error {
	return errors.New("unused")
}
func (m *memoryCache) ListAppend(context.Context, string, any, time.Duration) error {
	return errors.New("unused")
}
func (m *memoryCache) ListRange(context.Context, string, int64, int64, any) error {
	return errors.New("unused")
}
func (m *memoryCache) DeleteByPrefix(context.Context, string) error { return errors.New("unused") }

func TestConsumeRejectsMalformedTokensBeforeCacheLookup(t *testing.T) {
	t.Parallel()

	cache := newMemoryCache()
	store := NewStore(cache)
	for _, token := range []string{"not base64!", strings.Repeat("a", 44), strings.Repeat("a", 1_000_000)} {
		_, err := store.Consume(t.Context(), token)
		require.ErrorContains(t, err, "invalid token")
	}
	require.Empty(t, cache.lastKey)
}

func TestHandoffIsOpaqueOneTimeAndExpires(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cache := newMemoryCache()
	store := NewStore(cache)
	token, err := NewIssuer(store).Issue(ctx, "org_123")
	require.NoError(t, err)
	require.Len(t, token, 43)
	require.NotContains(t, cache.lastKey, token, "Redis key must contain only a token digest")
	require.True(t, strings.HasPrefix(cache.lastKey, "auth:support_handoff:"))

	grant, err := store.Consume(ctx, token)
	require.NoError(t, err)
	require.Equal(t, "org_123", grant.OrganizationID)
	_, err = store.Consume(ctx, token)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss)

	expiring, err := NewIssuer(store).Issue(ctx, "org_456")
	require.NoError(t, err)
	cache.now = cache.now.Add(GrantTTL)
	_, err = store.Consume(ctx, expiring)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss)
}
