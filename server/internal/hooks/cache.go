package hooks

import (
	"fmt"
)

// sessionCacheKey returns the Redis key for session metadata
func sessionCacheKey(sessionID string) string {
	return fmt.Sprintf("session:metadata:%s", sessionID)
}

// hookPendingCacheKey returns the Redis key for buffered hooks for a session
func hookPendingCacheKey(sessionID string) string {
	return fmt.Sprintf("hook:pending:%s", sessionID)
}
