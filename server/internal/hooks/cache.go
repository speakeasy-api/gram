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

// hookIdempotencyCacheKey returns the Redis key marking a hook invocation as
// already persisted, keyed by the per-invocation idempotency token the sender
// reuses across retries.
func hookIdempotencyCacheKey(token string) string {
	return fmt.Sprintf("hook:idempotency:%s", token)
}

// sessionAgentVariantCacheKey returns the Redis key for the agent variant
// of a session ("cowork" or "claude-code"). Stamped by SessionStart based
// on which mcp_inventory_* payload field is present; shares the MCP list
// TTL. Absence means SessionStart hasn't been processed for this session
// yet — callers should treat that as an ambiguous Claude session rather
// than assuming claude-code.
func sessionAgentVariantCacheKey(sessionID string) string {
	return fmt.Sprintf("session:agent-variant:%s", sessionID)
}

const (
	// agentVariantCowork marks a session that originated from a cowork
	// (cmux-managed) Claude Code environment rather than the standard CLI.
	agentVariantCowork = "cowork"
	// agentVariantClaudeCode marks a session that originated from the
	// standard Claude Code CLI (where `claude mcp list` was reachable).
	agentVariantClaudeCode = "claude-code"
)

// sessionMCPListTTL is how long the parsed MCP list survives without any
// hook activity for its session id. Each hook received refreshes it.
const sessionMCPListTTL = 12 * time.Hour

// hookIdempotencyTTL bounds how long a hook idempotency token blocks a repeat
// persistence. It only needs to outlive a sender's retry window (a handful of
// backoff attempts within seconds), so a few minutes is ample while keeping
// the dedup keyspace small.
const hookIdempotencyTTL = 10 * time.Minute

// hookReplayIdempotencyTTL is the claim window for events redelivered from a
// device's offline spool (X-Gram-Replayed). Replays can race each other far
// outside a normal retry burst — the device agent's recovery trigger, the
// hooks' opportunistic drain, Windows' lock-free double drains, and a
// failed post-accept unlink re-sent by a much later drain — so the claim
// must cover the spool's full 14-day retention, not one drain cycle. Only
// downtime backlog pays this keyspace cost, which keeps the volume trivial
// next to live traffic.
//
// Scope, deliberately: this dedupes replay-vs-replay. It does NOT cover the
// original-vs-replay case — a live delivery the server persisted but whose
// response was lost claims only hookIdempotencyTTL, and a spool replay
// arriving after those 10 minutes persists a second copy. Closing that gap
// needs a durable (Postgres) dedupe backstop, since extending the live TTL
// would hold a Redis key for every hook event fleet-wide; accepted and
// tracked on DNO-498. A duplicate telemetry row is the cheaper failure next
// to dropping downtime backlog.
const hookReplayIdempotencyTTL = 15 * 24 * time.Hour
