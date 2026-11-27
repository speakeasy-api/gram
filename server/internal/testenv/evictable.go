package testenv

import (
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/redisrole"
)

// NewEvictableCache returns the evictable-role adapter over client with the
// role guard in enforce mode, so a test fails when the code under test sends
// a sessions-role key through it.
func NewEvictableCache(t *testing.T, client *redis.Client) *cache.EvictableRedisAdapter {
	t.Helper()

	guard := redisrole.NewGuard(NewLogger(t), NewMeterProvider(t), redisrole.GuardEnforce)
	return cache.NewEvictableRedisAdapter(redisrole.EvictableClient{Client: client}, guard)
}

// NewRoleCache returns the auth- or ops-role adapter over client with the role
// guard in enforce mode, so a test fails when the code under test sends a key
// of another role, or of no declared family, through it.
func NewRoleCache(t *testing.T, client *redis.Client, role redisrole.Role) *cache.RoleRedisAdapter {
	t.Helper()

	guard := redisrole.NewGuard(NewLogger(t), NewMeterProvider(t), redisrole.GuardEnforce)
	return cache.NewRoleRedisAdapter(client, role, guard)
}

// evictableMemoryCache is the in-memory cache standing in for the evictable
// role in unit tests without Redis.
type evictableMemoryCache struct {
	*memoryCache
}

var _ cache.EvictableCache = (*evictableMemoryCache)(nil)

// EvictableRole implements [cache.EvictableCache].
func (*evictableMemoryCache) EvictableRole() {}

// NewEvictableMemoryCache returns an empty in-memory cache for code that
// takes the evictable-role cache.
func NewEvictableMemoryCache() cache.EvictableCache {
	return &evictableMemoryCache{memoryCache: &memoryCache{mu: sync.Mutex{}, entries: map[string][]byte{}}}
}
