package mcp

import (
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/redisrole"
)

// RedisRoles is the Redis state the MCP service keeps, by role. The process
// wiring builds one client and adapter per role; until a role has its own
// instance they share one client.
type RedisRoles struct {
	// Auth holds OAuth challenges, grants, consent, refresh replay and the
	// other sign-in state.
	Auth cache.Cache

	// AuthClient holds the replay guards and the rate limits charged while
	// validating assertions and tokens.
	AuthClient *redis.Client

	// Ops holds connection-attempt telemetry dedupe.
	Ops cache.Cache

	// OpsClient holds anonymous tunnel sessions and the tunnel and recheck
	// rate limits.
	OpsClient *redis.Client

	// Evictable holds the toolset and gateway token caches.
	Evictable cache.EvictableCache

	// EvictableClient holds the session client-info records.
	EvictableClient redisrole.EvictableClient
}
