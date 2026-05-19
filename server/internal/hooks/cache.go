package hooks

import (
	"fmt"
	"time"
)

// sessionCacheKey returns the Redis key for session metadata
func sessionCacheKey(sessionID string) string {
	return fmt.Sprintf("session:metadata:%s", sessionID)
}

// hookPendingCacheKey returns the Redis key for buffered hooks for a session
func hookPendingCacheKey(sessionID string) string {
	return fmt.Sprintf("hook:pending:%s", sessionID)
}

// sessionMCPListCacheKey returns the Redis key for the parsed `claude mcp list`
// snapshot of a session. Stored on SessionStart, TTL refreshed on every
// subsequent hook for the same session so we don't lose the mapping while
// the user is actively working but garbage-collect dead sessions.
func sessionMCPListCacheKey(sessionID string) string {
	return fmt.Sprintf("session:mcp-list:%s", sessionID)
}

// sessionMCPListTTL is how long the parsed MCP list survives without any
// hook activity for its session id. Each hook received refreshes it.
const sessionMCPListTTL = 12 * time.Hour
