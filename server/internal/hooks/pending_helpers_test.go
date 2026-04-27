package hooks

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

var (
	toolName   = "test_tool"
	toolUseID  = "toolu_123"
	toolName1  = "tool1"
	toolUseID1 = "toolu_123"
	toolName2  = "tool2"
	toolUseID2 = "toolu_234"
)

// TestBufferHook_AtomicAppend tests that buffering hooks uses atomic RPUSH
func TestBufferHook_AtomicAppend(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	toolName := "test_tool"
	toolUseID := "toolu_123"

	// Buffer a single hook
	payload := &hooks.ClaudeHookPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
	}

	// Access the private bufferHook method via the service
	// Since it's private, we'll test it indirectly through the Claude endpoint
	result, err := ti.service.Claude(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the hook was buffered in Redis by checking the key exists
	redisKey := "hook:pending:" + sessionID
	exists, err := ti.redisClient.Exists(ctx, redisKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists, "Hook should be buffered in Redis")

	// Verify it's a list with one element
	length, err := ti.redisClient.LLen(ctx, redisKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), length, "Should have exactly one buffered hook")
}

// TestBufferHook_MultipleConcurrent tests that concurrent buffering works correctly
func TestBufferHook_MultipleConcurrent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	numHooks := 50
	var wg sync.WaitGroup

	// Buffer multiple hooks concurrently to test for race conditions
	for i := range numHooks {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			toolName := "concurrent_tool"
			toolUseID := uuid.NewString()
			payload := &hooks.ClaudeHookPayload{
				HookEventName: "PreToolUse",
				SessionID:     &sessionID,
				ToolName:      &toolName,
				ToolUseID:     &toolUseID,
			}

			_, err := ti.service.Claude(ctx, payload)
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Verify all hooks were buffered atomically
	redisKey := "hook:pending:" + sessionID
	length, err := ti.redisClient.LLen(ctx, redisKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(numHooks), length, "All hooks should be buffered atomically without race conditions")
}

// TestFlushPendingHooks_DirectCall tests flushing by calling the flush method directly
func TestFlushPendingHooks_DirectCall(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()

	// Buffer multiple hooks using the cache directly
	cacheAdapter := cache.NewRedisCacheAdapter(ti.redisClient)
	numHooks := 5
	for range numHooks {
		payload := hooks.ClaudeHookPayload{
			HookEventName: "PreToolUse",
			SessionID:     &sessionID,
			ToolName:      &toolName,
			ToolUseID:     &toolUseID,
		}

		err := cacheAdapter.ListAppend(ctx, "hook:pending:"+sessionID, payload, 24*time.Hour)
		require.NoError(t, err)
	}

	// Verify hooks are buffered
	redisKey := "hook:pending:" + sessionID
	lengthBefore, err := ti.redisClient.LLen(ctx, redisKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(numHooks), lengthBefore)

	// Create session metadata
	metadata := SessionMetadata{
		SessionID:   sessionID,
		UserEmail:   "test@example.com",
		GramOrgID:   uuid.NewString(),
		ProjectID:   uuid.NewString(),
		ServiceName: "test-service",
		ClaudeOrgID: "claude-org-123",
	}

	// Call flushPendingHooks directly
	ti.service.flushPendingHooks(ctx, sessionID, &metadata)

	// Verify hooks were flushed (Redis list should be deleted)
	exists, err := ti.redisClient.Exists(ctx, redisKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists, "Buffered hooks should be flushed and deleted from Redis")
}

// TestFlushPendingHooks_EmptyList tests flushing when there are no pending hooks
func TestFlushPendingHooks_EmptyList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()

	// Create session metadata
	metadata := SessionMetadata{
		SessionID:   sessionID,
		UserEmail:   "test@example.com",
		GramOrgID:   uuid.NewString(),
		ProjectID:   uuid.NewString(),
		ServiceName: "test-service",
		ClaudeOrgID: "claude-org-123",
	}

	// Call flushPendingHooks with no buffered hooks (should not error)
	ti.service.flushPendingHooks(ctx, sessionID, &metadata)

	// Verify no Redis key was created
	redisKey := "hook:pending:" + sessionID
	exists, err := ti.redisClient.Exists(ctx, redisKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists)
}

// TestBufferAndFlush_MultipleSessionsConcurrent tests buffering and flushing across multiple sessions
func TestBufferAndFlush_MultipleSessionsConcurrent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	numSessions := 10
	hooksPerSession := 5

	var wg sync.WaitGroup

	// Create multiple sessions and buffer hooks concurrently
	for sessionIdx := range numSessions {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sessionID := uuid.NewString()

			// Buffer multiple hooks for this session
			for range hooksPerSession {
				toolName := "test_tool"
				toolUseID := uuid.NewString()
				payload := &hooks.ClaudeHookPayload{
					HookEventName: "PreToolUse",
					SessionID:     &sessionID,
					ToolName:      &toolName,
					ToolUseID:     &toolUseID,
				}

				_, err := ti.service.Claude(ctx, payload)
				assert.NoError(t, err)
			}

			// Verify hooks are buffered for this session
			redisKey := "hook:pending:" + sessionID
			length, err := ti.redisClient.LLen(ctx, redisKey).Result()
			assert.NoError(t, err)
			assert.Equal(t, int64(hooksPerSession), length)
		}(sessionIdx)
	}

	wg.Wait()
}

// TestSessionMetadata_CacheSetGet tests storing and retrieving session metadata
func TestSessionMetadata_CacheSetGet(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	metadata := SessionMetadata{
		SessionID:   sessionID,
		UserEmail:   "user@example.com",
		GramOrgID:   uuid.NewString(),
		ProjectID:   uuid.NewString(),
		ServiceName: "test-service",
		ClaudeOrgID: "claude-org-456",
	}

	cacheAdapter := cache.NewRedisCacheAdapter(ti.redisClient)

	// Store metadata
	key := "session:metadata:" + sessionID
	err := cacheAdapter.Set(ctx, key, metadata, 24*time.Hour)
	require.NoError(t, err)

	// Retrieve metadata
	var retrieved SessionMetadata
	err = cacheAdapter.Get(ctx, key, &retrieved)
	require.NoError(t, err)

	// Verify all fields match
	assert.Equal(t, metadata.SessionID, retrieved.SessionID)
	assert.Equal(t, metadata.UserEmail, retrieved.UserEmail)
	assert.Equal(t, metadata.GramOrgID, retrieved.GramOrgID)
	assert.Equal(t, metadata.ProjectID, retrieved.ProjectID)
	assert.Equal(t, metadata.ServiceName, retrieved.ServiceName)
	assert.Equal(t, metadata.ClaudeOrgID, retrieved.ClaudeOrgID)
}

// TestListAppend_TTLBehavior tests that TTL is only set once for new keys
func TestListAppend_TTLBehavior(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	cacheAdapter := cache.NewRedisCacheAdapter(ti.redisClient)
	key := "test:list:" + uuid.NewString()

	// First append - should set TTL
	err := cacheAdapter.ListAppend(ctx, key, "item1", 10*time.Second)
	require.NoError(t, err)

	// Check TTL exists
	ttl1, err := ti.redisClient.TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl1.Seconds(), 0.0, "TTL should be set")

	// Wait a bit
	time.Sleep(1 * time.Second)

	// Second append - should NOT reset TTL
	err = cacheAdapter.ListAppend(ctx, key, "item2", 10*time.Second)
	require.NoError(t, err)

	// Check TTL is less than original (proving it wasn't reset)
	ttl2, err := ti.redisClient.TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Less(t, ttl2.Seconds(), ttl1.Seconds(), "TTL should not be reset on subsequent appends")
}

// TestListRange_CorrectDeserialization tests that ListRange properly deserializes msgpack data
func TestListRange_CorrectDeserialization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	cacheAdapter := cache.NewRedisCacheAdapter(ti.redisClient)
	key := "test:payloads:" + uuid.NewString()

	// Create test payloads
	sessionID := uuid.NewString()
	payloads := []hooks.ClaudeHookPayload{
		{
			HookEventName: "PreToolUse",
			SessionID:     &sessionID,
			ToolName:      &toolName1,
			ToolUseID:     &toolUseID1,
		},
		{
			HookEventName: "PostToolUse",
			SessionID:     &sessionID,
			ToolName:      &toolName2,
			ToolUseID:     &toolUseID2,
		},
	}

	// Append payloads
	for _, payload := range payloads {
		err := cacheAdapter.ListAppend(ctx, key, payload, 1*time.Minute)
		require.NoError(t, err)
	}

	// Read back using ListRange
	var retrieved []hooks.ClaudeHookPayload
	err := cacheAdapter.ListRange(ctx, key, 0, -1, &retrieved)
	require.NoError(t, err)

	// Verify we got both payloads back
	require.Len(t, retrieved, 2)
	assert.Equal(t, "PreToolUse", retrieved[0].HookEventName)
	assert.Equal(t, "PostToolUse", retrieved[1].HookEventName)
	assert.Equal(t, "tool1", *retrieved[0].ToolName)
	assert.Equal(t, "tool2", *retrieved[1].ToolName)
}
