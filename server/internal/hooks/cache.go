package hooks

import (
	"fmt"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

var _ cache.CacheableObject[SessionMetadata] = (*SessionMetadata)(nil)

func (m SessionMetadata) CacheKey() string {
	return sessionCacheKey(m.SessionID)
}

func sessionCacheKey(sessionID string) string {
	return fmt.Sprintf("session:metadata:%s", sessionID)
}

func (m SessionMetadata) TTL() time.Duration {
	return 24 * time.Hour
}

func (m SessionMetadata) AdditionalCacheKeys() []string {
	return []string{}
}

type ClaudePayloadCache struct {
	SessionID string
	Payloads  []gen.ClaudePayload
}

var _ cache.CacheableObject[ClaudePayloadCache] = (*ClaudePayloadCache)(nil)

func (c ClaudePayloadCache) CacheKey() string {
	return hookPendingCacheKey(c.SessionID)
}

func hookPendingCacheKey(sessionID string) string {
	return fmt.Sprintf("hook:pending:%s", sessionID)
}

func (c ClaudePayloadCache) AdditionalCacheKeys() []string {
	return []string{}
}

func (c ClaudePayloadCache) TTL() time.Duration {
	return 10 * time.Minute
}
