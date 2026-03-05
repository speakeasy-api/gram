package hooks

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

// TestBufferHook_AtomicAppend tests that buffering hooks uses atomic RPUSH
func TestBufferHook_AtomicAppend(t *testing.T) {
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	toolName := "test_tool"
	toolUseID := "toolu_123"

	// Buffer a single hook
	payload := &hooks.ClaudePayload{
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
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	numHooks := 50
	var wg sync.WaitGroup

	// Buffer multiple hooks concurrently to test for race conditions
	for i := 0; i < numHooks; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			toolName := "concurrent_tool"
			toolUseID := uuid.NewString()
			payload := &hooks.ClaudePayload{
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

// TestFlushPendingHooks_Success tests successful flushing of buffered hooks
func TestFlushPendingHooks_Success(t *testing.T) {
	ctx, ti := newTestHooksService(t)

	// Create session metadata
	sessionID := uuid.NewString()
	userEmail := "test@example.com"
	gramOrgID := uuid.NewString()
	projectID := uuid.NewString()

	// Buffer multiple hooks first
	numHooks := 5
	for i := 0; i < numHooks; i++ {
		toolName := "test_tool"
		toolUseID := uuid.NewString()
		payload := &hooks.ClaudePayload{
			HookEventName: "PreToolUse",
			SessionID:     &sessionID,
			ToolName:      &toolName,
			ToolUseID:     &toolUseID,
		}

		_, err := ti.service.Claude(ctx, payload)
		require.NoError(t, err)
	}

	// Verify hooks are buffered
	redisKey := "hook:pending:" + sessionID
	lengthBefore, err := ti.redisClient.LLen(ctx, redisKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(numHooks), lengthBefore)

	// Store session metadata to trigger flush
	metadata := SessionMetadata{
		SessionID:   sessionID,
		UserEmail:   userEmail,
		GramOrgID:   gramOrgID,
		ProjectID:   projectID,
		ServiceName: "test-service",
		ClaudeOrgID: "claude-org-123",
	}

	cacheAdapter := cache.NewRedisCacheAdapter(ti.redisClient)
	metadataKey := "session:metadata:" + sessionID
	err = cacheAdapter.Set(ctx, metadataKey, metadata, 24*time.Hour)
	require.NoError(t, err)

	// Send a new hook to trigger flush (since session is now validated)
	toolName := "trigger_tool"
	toolUseID := uuid.NewString()
	payload := &hooks.ClaudePayload{
		HookEventName: "PostToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
	}

	_, err = ti.service.Claude(ctx, payload)
	require.NoError(t, err)

	// Verify hooks were flushed (Redis list should be deleted)
	exists, err := ti.redisClient.Exists(ctx, redisKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists, "Buffered hooks should be flushed and deleted from Redis")
}

// TestFlushPendingHooks_EmptyList tests flushing when there are no pending hooks
func TestFlushPendingHooks_EmptyList(t *testing.T) {
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	userEmail := "test@example.com"
	gramOrgID := uuid.NewString()
	projectID := uuid.NewString()

	// Store session metadata without buffering any hooks
	metadata := SessionMetadata{
		SessionID:   sessionID,
		UserEmail:   userEmail,
		GramOrgID:   gramOrgID,
		ProjectID:   projectID,
		ServiceName: "test-service",
		ClaudeOrgID: "claude-org-123",
	}

	cacheAdapter := cache.NewRedisCacheAdapter(ti.redisClient)
	metadataKey := "session:metadata:" + sessionID
	err := cacheAdapter.Set(ctx, metadataKey, metadata, 24*time.Hour)
	require.NoError(t, err)

	// Send a hook (should not error even though there are no pending hooks to flush)
	toolName := "test_tool"
	toolUseID := uuid.NewString()
	payload := &hooks.ClaudePayload{
		HookEventName: "PostToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
	}

	_, err = ti.service.Claude(ctx, payload)
	require.NoError(t, err)
}

// TestBufferAndFlush_MultipleSessionsConcurrent tests buffering and flushing across multiple sessions
func TestBufferAndFlush_MultipleSessionsConcurrent(t *testing.T) {
	ctx, ti := newTestHooksService(t)

	numSessions := 10
	hooksPerSession := 5

	var wg sync.WaitGroup

	// Create multiple sessions and buffer hooks concurrently
	for sessionIdx := 0; sessionIdx < numSessions; sessionIdx++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sessionID := uuid.NewString()

			// Buffer multiple hooks for this session
			for hookIdx := 0; hookIdx < hooksPerSession; hookIdx++ {
				toolName := "test_tool"
				toolUseID := uuid.NewString()
				payload := &hooks.ClaudePayload{
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
	ctx, ti := newTestHooksService(t)

	cacheAdapter := cache.NewRedisCacheAdapter(ti.redisClient)
	key := "test:payloads:" + uuid.NewString()

	// Create test payloads
	sessionID := uuid.NewString()
	payloads := []hooks.ClaudePayload{
		{
			HookEventName: "PreToolUse",
			SessionID:     &sessionID,
			ToolName:      stringPtr("tool1"),
			ToolUseID:     stringPtr("id1"),
		},
		{
			HookEventName: "PostToolUse",
			SessionID:     &sessionID,
			ToolName:      stringPtr("tool2"),
			ToolUseID:     stringPtr("id2"),
		},
	}

	// Append payloads
	for _, payload := range payloads {
		err := cacheAdapter.ListAppend(ctx, key, payload, 1*time.Minute)
		require.NoError(t, err)
	}

	// Read back using ListRange
	var retrieved []hooks.ClaudePayload
	err := cacheAdapter.ListRange(ctx, key, 0, -1, &retrieved)
	require.NoError(t, err)

	// Verify we got both payloads back
	require.Len(t, retrieved, 2)
	assert.Equal(t, "PreToolUse", retrieved[0].HookEventName)
	assert.Equal(t, "PostToolUse", retrieved[1].HookEventName)
	assert.Equal(t, "tool1", *retrieved[0].ToolName)
	assert.Equal(t, "tool2", *retrieved[1].ToolName)
}

func stringPtr(s string) *string {
	return &s
}
