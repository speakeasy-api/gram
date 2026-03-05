# Hooks Service

This service handles Claude Code hook events for tool usage observability, providing real-time tracking of tool calls with user attribution.

## Architecture Overview

The hooks service uses a **Redis-buffered validation pattern** to handle the timing mismatch between unauthenticated hook events and authenticated session metadata.

### The Challenge

Claude Code sends two types of requests to our service:

1. **Hook Events** (unauthenticated) - `PreToolUse`, `PostToolUse`, `PostToolUseFailure`
   - (Usually) arrive first (immediately when tools are called)
   - Contain tool information (including `session_id`) but no user identity
   - Cannot validate authenticity

2. **Logs Events** (authenticated via API key)
   - (Usually) arrive later (after session established)
   - Contain `session_id`, `user_email`, `organization.id`
   - Validated through API key authentication

**Problem**: Hook events often arrive *before* we know the user's identity via the Logs endpoint.

### The Solution: Redis Buffering

```
┌─────────────────────────────────────────────────────────────────┐
│                      Hook Event Flow                             │
└─────────────────────────────────────────────────────────────────┘

Hook Event Arrives
       │
       ├──> Check Redis: "session:metadata:{session_id}"
       │
       ├──> FOUND: Session validated! ✓
       │    └──> Write directly to ClickHouse with:
       │         • user_email
       │         • gram_org_id
       │         • project_id
       │         • tool call data
       │
       └──> NOT FOUND: Session not yet validated
            └──> Buffer in Redis: "hook:pending:{session_id}:{tool_use_id}"
                 • TTL: 10 minutes
                 • Contains full hook payload
                 • Will be processed when session validates


┌─────────────────────────────────────────────────────────────────┐
│                      Logs Event Flow                             │
└─────────────────────────────────────────────────────────────────┘

Logs Event Arrives (Authenticated)
       │
       ├──> Validate API Key ✓
       │
       ├──> Extract:
       │    • session_id
       │    • user_email
       │    • Claude organization.id
       │
       ├──> Get gram_org_id & project_id from auth context
       │
       └──> Store in Redis: "session:metadata:{session_id}"
            • TTL: 24 hours
            • Future hooks will find this and write directly to ClickHouse
            • Buffered hooks will be processed on next call (within 10min window)
```

## Key Components

### Files

- **[impl.go](impl.go)** - Main service implementation
  - Hook event handlers
  - Logs endpoint
  - Session validation

- **[impl_helpers.go](impl_helpers.go)** - Helper functions
  - `bufferHook()` - Store hooks in Redis
  - `writeHookToClickHouseWithMetadata()` - Write validated hooks
  - `buildTelemetryAttributesWithMetadata()` - Build attributes with user context

### Redis Keys

| Key Pattern | Purpose | TTL | Contents |
|-------------|---------|-----|----------|
| `session:metadata:{session_id}` | Session validation cache | 24 hours | `SessionMetadata{UserEmail, GramOrgID, ProjectID}` |
| `hook:pending:{session_id}:{tool_use_id}` | Buffered hook events | 10 minutes | JSON-encoded `ClaudePayload` |

### Data Types

```go
type SessionMetadata struct {
    UserEmail string  // From Logs endpoint
    GramOrgID string  // From API key auth context
    ProjectID string  // From API key auth context (UUID)
}
```

## Security Model

### Unauthenticated Hook Events

- ❌ No authentication required
- ❌ Cannot trust sender
- ✅ **Mitigation**: Buffered in Redis, not written to permanent storage
- ✅ **Auto-cleanup**: 10 minute TTL - unvalidated hooks expire

### Authenticated Logs Endpoint

- ✅ API key authentication required (`security.ByKey`)
- ✅ Project-scoped authorization (`security.ProjectSlug`)
- ✅ Only validated sessions create `session:metadata` entries
- ✅ All ClickHouse writes require validated session

### Attack Scenarios

| Attack | Impact | Mitigation |
|--------|--------|------------|
| Spam hook events | Redis memory consumption | 10min TTL, auto-expiration |
| Fake session IDs | Wasted Redis entries | No Logs event = no validation = auto-expire |
| Replay attacks | Duplicate hooks buffered | Idempotent writes, bounded by TTL |

**Result**: No unauthenticated data reaches ClickHouse. Only hooks with matching authenticated sessions are persisted.

## Flow Examples

### Example 1: Happy Path (Logs First)

```
1. [10:00:00] Logs endpoint called → session:metadata:abc123 stored
2. [10:00:01] PreToolUse hook → Checks Redis → FOUND → Writes to ClickHouse ✓
3. [10:00:05] PostToolUse hook → Checks Redis → FOUND → Writes to ClickHouse ✓
```

**Result**: Immediate writes with full user context.

### Example 2: Hook First (Typical)

```
1. [10:00:00] PreToolUse hook → Checks Redis → NOT FOUND → Buffered
2. [10:00:01] Logs endpoint called → session:metadata:abc123 stored
3. [10:00:05] PostToolUse hook → Checks Redis → FOUND → Writes to ClickHouse ✓
```

**Result**:
- First hook buffered (not written)
- Subsequent hooks after Logs write immediately
- Buffered hook expires after 10min if not retried

### Example 3: Spam Attack

```
1. [10:00:00] Malicious PreToolUse (fake session) → Buffered
2. [10:10:00] Buffer expires (no Logs endpoint ever called)
```

**Result**: No permanent storage impact, Redis automatically cleans up.

### Example 4: Late Logs

```
1. [10:00:00] PreToolUse hook → Buffered
2. [10:00:01] PostToolUse hook → Buffered
3. [10:09:00] Logs endpoint called → session:metadata stored
4. [10:09:30] User retries → Both hooks write to ClickHouse ✓
```

**Result**: Works if retry happens within 10min window.

## ClickHouse Schema

Hook events are stored in the `telemetry_logs` table with these key attributes:

```sql
SELECT
    event_source,           -- 'hook'
    hook_event,             -- 'PreToolUse', 'PostToolUse', etc.
    tool_name,              -- Tool that was called
    user_email,             -- From session metadata
    gram_project_id,        -- From session metadata
    gram_organization_id,   -- From session metadata (gram org)
    attributes.gen_ai.conversation.id,  -- session_id
    attributes.gen_ai.tool_call.id,     -- tool_use_id
    attributes.gen_ai.tool_call.arguments,
    attributes.gen_ai.tool_call.result
FROM telemetry_logs
WHERE event_source = 'hook'
```

## Configuration

### Redis Cache

The service requires a Redis connection for buffering:

```go
cache := cache.NewRedisCacheAdapter(redisClient)
hooksService := hooks.NewService(logger, db, tracerProvider, telemSvc, sessionManager, cache)
```

### TTL Values

Defined in `impl_helpers.go`:

```go
const (
    hookBufferTTL     = 10 * time.Minute   // Buffered hook expiration
    sessionMetadataTTL = 24 * time.Hour    // Session metadata expiration
)
```

**Rationale**:
- **10 minutes**: Balance between reliability and memory usage. Most sessions validate within seconds.
- **24 hours**: Long enough for full Claude Code sessions, short enough to avoid unbounded growth.

## Monitoring & Observability

### Key Metrics to Track

1. **Buffer Rate**: % of hooks that are buffered vs directly written
   - High rate indicates Logs endpoint delays

2. **Buffer Expiration**: Count of hooks that expire before validation
   - High count indicates integration issues

3. **Redis Memory**: Size of `hook:pending:*` keys
   - Spike indicates spam or Logs endpoint failures

4. **Validation Lag**: Time between hook event and Logs event
   - Should be < 1 second typically

### Log Events

The service emits these log events:

- `hook_buffered` - Hook stored in Redis
- `hook_written` - Hook written to ClickHouse with metadata
- `session_validated` - Session metadata stored from Logs endpoint

## Future Improvements

### 1. Proactive Buffer Flushing

Currently, buffered hooks are only written when *new* hooks arrive for the same session. This means the first hook in a session may never be written if no subsequent hooks occur.

**Solution**: Background job to scan and flush buffered hooks:

```go
// Runs every 30 seconds
func (s *Service) flushExpiredBuffers() {
    sessions := s.redis.Keys("session:metadata:*")
    for session := range sessions {
        sessionID := extractSessionID(session)
        bufferedHooks := s.redis.Keys(fmt.Sprintf("hook:pending:%s:*", sessionID))

        for hook := range bufferedHooks {
            payload := unmarshalHook(hook)
            metadata := getSessionMetadata(sessionID)
            s.writeHookToClickHouseWithMetadata(ctx, payload, metadata)
            s.redis.Delete(hook)
        }
    }
}
```

### 2. Metrics & Alerting

- Add Prometheus metrics for buffer rates
- Alert on high buffer expiration rates
- Dashboard for validation lag

### 3. Batch Writes

- Buffer multiple hooks in memory
- Flush to ClickHouse in batches
- Reduce write amplification

### 4. Session Pre-Registration

If Claude Code can notify us when sessions start, we could pre-register sessions in Postgres:

```sql
CREATE TABLE claude_sessions (
    session_id TEXT PRIMARY KEY,
    gram_org_id UUID NOT NULL,
    user_email TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);
```

This would eliminate buffering entirely - all hooks could validate against Postgres immediately.

## Troubleshooting

### Hooks Not Appearing in ClickHouse

**Symptoms**: Hook events arrive but no data in `telemetry_logs`

**Debugging**:
1. Check Redis: `KEYS session:metadata:*` - Is session metadata present?
2. Check logs for `session_validated` events - Did Logs endpoint succeed?
3. Check Redis: `KEYS hook:pending:*` - Are hooks buffered?
4. Verify API key authentication on Logs endpoint

**Common Causes**:
- Logs endpoint not being called
- API key auth failures
- Redis connection issues
- Hook events missing `session_id`

### Redis Memory Growth

**Symptoms**: Redis memory usage increasing over time

**Debugging**:
1. Count buffered hooks: `KEYS hook:pending:*`
2. Count session metadata: `KEYS session:metadata:*`
3. Check TTLs: `TTL session:metadata:abc123`

**Common Causes**:
- TTL not being set correctly
- Logs endpoint failures causing orphaned buffers
- Spam attacks creating many fake sessions

**Mitigation**:
- Verify TTL configuration
- Add maxmemory policy: `maxmemory-policy allkeys-lru`
- Monitor buffer expiration rates

### Validation Lag

**Symptoms**: High delay between hook and Logs events

**Debugging**:
1. Check Logs endpoint response times
2. Verify network latency from Claude Code
3. Check API key auth performance

**Impact**: Hooks remain buffered longer, higher Redis memory usage

**Mitigation**:
- Optimize Logs endpoint
- Increase hookBufferTTL if needed
- Add caching for auth checks

## Related Documentation

- [ClickHouse Schema](../../clickhouse/schema.sql) - Telemetry logs table definition
- [Telemetry Service](../telemetry/) - ClickHouse write operations
- [Auth Service](../auth/) - API key authentication
- [Design Spec](../../design/hooks/design.go) - Goa service definition

## Questions?

This architecture balances security, reliability, and performance. The key insight is using Redis as a temporary validation bridge, ensuring only authenticated data reaches permanent storage while handling the asynchronous nature of Claude Code's event delivery.

For questions or improvements, see the implementation guide at `/IMPLEMENTATION_SUMMARY.md`.
