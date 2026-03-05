# Hooks Redis Buffering Implementation Summary

## Overview
Implemented Option 1: Redis buffering with session validation via Logs endpoint.

## Architecture
1. **Hook Events** (unauthenticated) → Check Redis for session metadata
   - If metadata exists → Write directly to ClickHouse with full context
   - If not exists → Buffer hook payload in Redis (10min TTL)

2. **Logs Endpoint** (authenticated) → Store session metadata in Redis (24h TTL)
   - Validates session via API key
   - Stores user_email, gram_org_id, project_id
   - Future hook events will find this metadata and write to ClickHouse

## Files Modified

### 1. `/Users/robertcrumbaugh/code/gram/server/internal/hooks/impl.go`
**Changes needed:**
- Add `cache cache.Cache` field to Service struct
- Add `SessionMetadata` type definition
- Update `NewService()` to accept cache parameter
- Implement `Logs()` endpoint method
- Update hook handlers to check Redis before buffering

### 2. `/Users/robertcrumbaugh/code/gram/server/internal/hooks/impl_helpers.go` ✅ CREATED
Helper functions for:
- `bufferHook()` - Store hook in Redis
- `writeHookToClickHouseWithMetadata()` - Write hook with session context
- `buildTelemetryAttributesWithMetadata()` - Build attributes with metadata
- `flushPendingHooks()` - Placeholder for flush logic

### 3. `/Users/robertcrumbaugh/code/gram/server/cmd/gram/start.go` ✅ UPDATED
- Line 669: Pass `cache.NewRedisCacheAdapter(redisClient)` to `hooks.NewService()`

### 4. `/Users/robertcrumbaugh/code/gram/server/design/hooks/design.go` ✅ UPDATED
- Logs endpoint now has API key authentication

## Implementation Steps Required

### Step 1: Update impl.go Service struct
```go
type Service struct {
	tracer           trace.Tracer
	logger           *slog.Logger
	db               *pgxpool.Pool
	telemetryService *telemetry.Service
	auth             *auth.Auth
	cache            cache.Cache  // ADD THIS
}

// ADD THIS TYPE
type SessionMetadata struct {
	UserEmail string
	GramOrgID string
	ProjectID string
}
```

### Step 2: Update imports in impl.go
Add:
```go
"github.com/speakeasy-api/gram/server/internal/cache"
"github.com/speakeasy-api/gram/server/internal/oops"
```

### Step 3: Update NewService in impl.go
```go
func NewService(logger *slog.Logger, db *pgxpool.Pool, tracerProvider trace.TracerProvider, telemetryService *telemetry.Service, sessions *sessions.Manager, cache cache.Cache) *Service {
	return &Service{
		tracer:           tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/hooks"),
		logger:           logger.With(attr.SlogComponent("hooks")),
		db:               db,
		telemetryService: telemetryService,
		auth:             auth.New(logger, db, sessions),
		cache:            cache,  // ADD THIS
	}
}
```

### Step 4: Implement Logs endpoint in impl.go
Add this method:
```go
// Logs handles authenticated OTEL logs data from Claude Code
func (s *Service) Logs(ctx context.Context, payload *gen.LogsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	// Iterate through all resource logs
	for _, resourceLog := range payload.ResourceLogs {
		if resourceLog == nil {
			continue
		}

		// Extract service name from resource attributes
		serviceName := extractResourceAttribute(resourceLog.Resource, "service.name")

		// Iterate through all scope logs
		for _, scopeLog := range resourceLog.ScopeLogs {
			if scopeLog == nil {
				continue
			}

			// Iterate through all log records
			for _, logRecord := range scopeLog.LogRecords {
				if logRecord == nil {
					continue
				}

				// Extract session data
				data := extractLogData(logRecord, serviceName)

				if data.SessionID == "" {
					continue
				}

				// Store session metadata in Redis
				metadata := SessionMetadata{
					UserEmail: data.UserEmail,
					GramOrgID: authCtx.ActiveOrganizationID,
					ProjectID: authCtx.ProjectID.String(),
				}

				metadataKey := fmt.Sprintf("session:metadata:%s", data.SessionID)
				if err := s.cache.Set(ctx, metadataKey, metadata, 24*time.Hour); err != nil {
					s.logger.ErrorContext(ctx, "Failed to store session metadata", attr.SlogError(err))
					continue
				}

				s.logger.InfoContext(ctx, "Stored session metadata",
					attr.SlogEvent("session_validated"),
					"session_id", data.SessionID,
					"user_email", data.UserEmail,
				)

				// Note: Buffered hooks will be processed when they're called again
				// within the 10min TTL window
			}
		}
	}

	return nil
}
```

### Step 5: Update hook handlers in impl.go

Replace `handlePreToolUse`, `handlePostToolUse`, and `handlePostToolUseFailure` with:

```go
func (s *Service) handlePreToolUse(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	if payload.SessionID == nil || *payload.SessionID == "" {
		s.logger.WarnContext(ctx, "PreToolUse called without session ID")
		return &gen.ClaudeHookResult{}, nil
	}

	sessionID := *payload.SessionID

	// Check if session metadata already exists
	var metadata SessionMetadata
	metadataKey := fmt.Sprintf("session:metadata:%s", sessionID)
	err := s.cache.Get(ctx, metadataKey, &metadata)

	if err == nil {
		// Session validated - write directly to ClickHouse
		s.writeHookToClickHouseWithMetadata(ctx, payload, &metadata)
		return &gen.ClaudeHookResult{}, nil
	}

	// Session not validated yet - buffer in Redis
	if err := s.bufferHook(ctx, sessionID, payload); err != nil {
		s.logger.ErrorContext(ctx, "Failed to buffer hook", attr.SlogError(err))
	}

	return &gen.ClaudeHookResult{}, nil
}

func (s *Service) handlePostToolUse(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	if payload.SessionID == nil || *payload.SessionID == "" {
		return &gen.ClaudeHookResult{}, nil
	}

	sessionID := *payload.SessionID

	var metadata SessionMetadata
	metadataKey := fmt.Sprintf("session:metadata:%s", sessionID)
	err := s.cache.Get(ctx, metadataKey, &metadata)

	if err == nil {
		s.writeHookToClickHouseWithMetadata(ctx, payload, &metadata)
		return &gen.ClaudeHookResult{}, nil
	}

	if err := s.bufferHook(ctx, sessionID, payload); err != nil {
		s.logger.ErrorContext(ctx, "Failed to buffer hook", attr.SlogError(err))
	}

	return &gen.ClaudeHookResult{}, nil
}

func (s *Service) handlePostToolUseFailure(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	if payload.SessionID == nil || *payload.SessionID == "" {
		return &gen.ClaudeHookResult{}, nil
	}

	sessionID := *payload.SessionID

	var metadata SessionMetadata
	metadataKey := fmt.Sprintf("session:metadata:%s", sessionID)
	err := s.cache.Get(ctx, metadataKey, &metadata)

	if err == nil {
		s.writeHookToClickHouseWithMetadata(ctx, payload, &metadata)
		return &gen.ClaudeHookResult{}, nil
	}

	if err := s.bufferHook(ctx, sessionID, payload); err != nil {
		s.logger.ErrorContext(ctx, "Failed to buffer hook", attr.SlogError(err))
	}

	return &gen.ClaudeHookResult{}, nil
}
```

### Step 6: Fix OTELLogData references

In the `extractLogData` function (around line 400), change:
```go
case "organization.id":
	data.ClaudeOrgID = value  // Changed from data.OrgID
```

## Key Redis Keys

- `session:metadata:{session_id}` - Stores SessionMetadata (24h TTL)
- `hook:pending:{session_id}:{tool_use_id}` - Buffered hook payloads (10min TTL)

## Security

- ✅ Logs endpoint is authenticated (API key required)
- ✅ Only validated sessions write to ClickHouse
- ✅ Unvalidated hooks expire after 10 minutes
- ✅ Session metadata expires after 24 hours

## Testing

1. Call hook endpoint WITHOUT prior Logs call → Hook buffered in Redis
2. Call Logs endpoint → Session metadata stored
3. Call hook endpoint AFTER Logs call → Hook written directly to ClickHouse with full context
4. Wait 10+ minutes → Buffered hooks expire automatically

## Future Improvements

1. Implement actual flush logic in `flushPendingHooks()` using Redis SCAN
2. Add metrics for buffered vs direct writes
3. Add alerting for high buffer rates (indicates Logs endpoint issues)
4. Consider background job to proactively flush buffered hooks
