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
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// otelOnlyCtx returns a context that simulates the OTEL flow: no auth
// context attached, so recordHook routes through the Redis-session
// lookup + bufferHook fallback rather than the plugin attribution path.
func otelOnlyCtx(ctx context.Context) context.Context {
	return contextvalues.SetAuthContext(ctx, nil)
}

// MCP-routed tool calls must carry the resolved server identifier on every
// telemetry log via gram.mcp.match — for *every* MCP tool call regardless
// of policy state. The offline risk batch scanner reads this back by
// trace_id to populate risk_results.match.
func TestBuildTelemetryAttributesWithMetadata_SetsMCPMatchFromCachedList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp", Transport: "STDIO"}},
		sessionMCPListTTL,
	))

	mcpToolName := "mcp__mise__run_task"
	toolUse := "toolu_local_mcp"
	metadata := &SessionMetadata{
		SessionID: sessionID,
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &mcpToolName,
		ToolUseID:     &toolUse,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, "mise mcp", attrs[attr.MCPMatchKey])
}

// Cowork runs the same claude-code binary and reports the same OTEL
// service.name, so the agent variant cached by SessionStart is the only
// signal that distinguishes it. Tool-call telemetry must stamp hook_source
// = "cowork" so cowork sessions stay filterable in tool logs.
func TestBuildTelemetryAttributesWithMetadata_HookSourceFromCoworkVariant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionAgentVariantCacheKey(sessionID),
		agentVariantCowork, sessionMCPListTTL))

	// ServiceName is "claude-code" (what cowork reports) — the variant must
	// still win so the row is labelled "cowork", not "claude-code".
	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, agentVariantCowork, attrs[attr.HookSourceKey])
}

// End-to-end across the cache: a SessionStart carrying a cowork MCP
// inventory (what hook.sh sends from inside cowork) must be enough for a
// later tool call to be labelled "cowork" — no hand-seeded cache. This
// pins the writer (cacheMCPListSnapshot) and reader (sessionAgentVariant)
// to the same key and value.
func TestBuildTelemetryAttributesWithMetadata_HookSourceCoworkFromSessionStart(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	userEmail := "cowork-hook-source@example.com"
	_, err := ti.service.Claude(ctx, &hooks.ClaudePayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_cowork": []any{
				map[string]any{
					"name":           "Linear",
					"url":            "https://mcp.linear.app/mcp",
					"transport":      "HTTP",
					"status":         "connected",
					"connector_uuid": "linear-connector",
				},
			},
		},
	})
	require.NoError(t, err)

	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, agentVariantCowork, attrs[attr.HookSourceKey])
}

// Current cowork builds self-identify with OTEL service.name "cowork" — the
// source of truth. Tool rows must be labelled cowork from it alone, with no
// SessionStart inventory variant on file (the canonical ingest transport
// stamps none).
func TestBuildTelemetryAttributesWithMetadata_HookSourceFromCoworkServiceName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "cowork",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, agentVariantCowork, attrs[attr.HookSourceKey])
}

// Claude Code Desktop is its own surface: the desktop hook adapter slug must
// pass through as hook_source, not collapse into claude-code (CLI) — and not
// into cowork, which shares the same adapter but is distinguished by the OTEL
// service.name or the SessionStart variant.
func TestBuildTelemetryAttributesWithMetadata_HookSourceClaudeCodeDesktop(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code-desktop",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, surfaceClaudeCodeDesktop, attrs[attr.HookSourceKey])
}

// A claude-code variant (or no cached variant) must not be rewritten to
// "cowork"; hook_source keeps the ServiceName / "claude" default.
func TestBuildTelemetryAttributesWithMetadata_HookSourceDefaultsWithoutCoworkVariant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionAgentVariantCacheKey(sessionID),
		agentVariantClaudeCode, sessionMCPListTTL))

	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, "claude-code", attrs[attr.HookSourceKey])
}

// Native (non-MCP) tools must NOT get a gram.mcp.match attribute — the
// hook never routes them through an MCP server, and an empty value on the
// log row would pollute the CH index.
func TestBuildTelemetryAttributesWithMetadata_NoMCPMatchForNativeTool(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	bashTool := "Bash"
	toolUse := "toolu_bash"
	metadata := &SessionMetadata{
		SessionID: sessionID,
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &bashTool,
		ToolUseID:     &toolUse,
		SessionID:     &sessionID,
	}, metadata)

	_, has := attrs[attr.MCPMatchKey]
	assert.False(t, has, "Bash and other native tools must not get an MCP match attribute")
}

// Cowork tool names embed the connector UUID rather than a sanitized
// server name, so the SessionStart inventory match must resolve the
// entry by ConnectorUUID and rewrite gram.tool_call.source from the UUID
// to the human-readable Name. The MCP server URL is persisted alongside.
func TestBuildTelemetryAttributesWithMetadata_CoworkOverridesSourceWithName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	connectorUUID := "a1b2c3d4-e5f6-7890-abcd-ef0123456789"
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{
			Source: "claude.ai", Name: "Slack",
			URL: "https://mcp.example.com/slack", Transport: "HTTP",
			ConnectorUUID: connectorUUID,
		}},
		sessionMCPListTTL,
	))

	mcpToolName := "mcp__" + connectorUUID + "__send_message"
	toolUse := "toolu_cowork_mcp"
	metadata := &SessionMetadata{
		SessionID: sessionID,
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &mcpToolName,
		ToolUseID:     &toolUse,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, "Slack", attrs[attr.ToolCallSourceKey])
	assert.Equal(t, "https://mcp.example.com/slack", attrs[attr.MCPServerURLKey])
	assert.Equal(t, "https://mcp.example.com/slack", attrs[attr.MCPMatchKey])
}

// When the Cowork inventory entry has no Name (defensive fallback), the
// source must remain the connector UUID — the matched entry still
// contributes its URL.
func TestBuildTelemetryAttributesWithMetadata_CoworkFallsBackToUUIDWhenNoName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	connectorUUID := "a1b2c3d4-e5f6-7890-abcd-ef0123456789"
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{
			Source: "claude.ai", URL: "https://mcp.example.com/slack",
			Transport: "HTTP", ConnectorUUID: connectorUUID,
		}},
		sessionMCPListTTL,
	))

	mcpToolName := "mcp__" + connectorUUID + "__send_message"
	toolUse := "toolu_cowork_mcp_no_name"
	metadata := &SessionMetadata{
		SessionID: sessionID,
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &mcpToolName,
		ToolUseID:     &toolUse,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, connectorUUID, attrs[attr.ToolCallSourceKey])
	assert.Equal(t, "https://mcp.example.com/slack", attrs[attr.MCPServerURLKey])
}

// Claude Code MCP tool names embed a sanitized form of the server name
// ("claude_ai_Linear_Speakeasy"); when the SessionStart inventory carries
// the raw Name ("Linear (Speakeasy)") we replace the sanitized source
// with it so dashboards display the same value Cowork produces.
func TestBuildTelemetryAttributesWithMetadata_ClaudeCodeReplacesSanitizedSourceWithName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{
			Source: "claude.ai", Name: "Linear (Speakeasy)",
			URL: "https://chat.speakeasy.com/mcp/linear", Transport: "HTTP",
		}},
		sessionMCPListTTL,
	))

	mcpToolName := "mcp__claude_ai_Linear_Speakeasy__list_issues"
	toolUse := "toolu_claude_mcp"
	metadata := &SessionMetadata{
		SessionID: sessionID,
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &mcpToolName,
		ToolUseID:     &toolUse,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, "Linear (Speakeasy)", attrs[attr.ToolCallSourceKey])
	assert.Equal(t, "https://chat.speakeasy.com/mcp/linear", attrs[attr.MCPServerURLKey])
}

// When the MCP list snapshot doesn't include the called server, we fall
// back to the server-prefix portion of the tool name so the batch scanner
// still has *something* to allowlist on. Better than no attribute (which
// would force the scanner into its own prefix-derivation fallback).
func TestBuildTelemetryAttributesWithMetadata_FallsBackToServerPrefix(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "other", URL: "https://other.example.com/mcp", Transport: "HTTP"}},
		sessionMCPListTTL,
	))

	mcpToolName := "mcp__mise__run_task"
	toolUse := "toolu_no_match"
	metadata := &SessionMetadata{
		SessionID: sessionID,
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &mcpToolName,
		ToolUseID:     &toolUse,
		SessionID:     &sessionID,
	}, metadata)

	assert.Equal(t, "mise", attrs[attr.MCPMatchKey])
}

func TestBuildTelemetryAttributesWithMetadata_ResolvesUserIDFromEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "claude-user-id"
	userEmail := "claude-user@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)

	metadata := &SessionMetadata{
		SessionID:     uuid.NewString(),
		ServiceName:   "",
		UserEmail:     userEmail,
		UserID:        "",
		ExternalOrgID: "",
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		SessionID:     &metadata.SessionID,
	}, metadata)

	// User identity travels on LogParams.UserInfo, not the attributes map;
	// the build call's job is to resolve the user ID onto the metadata.
	assert.NotContains(t, attrs, attr.UserEmailKey)
	assert.NotContains(t, attrs, attr.UserIDKey)
	assert.Equal(t, userID, metadata.UserID)
}

func TestPersistToolCallEvent_PreservesEmailWhenUserIDUnresolved(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	userEmail := "smartnews-user@example.com"
	metadata := &SessionMetadata{
		SessionID: sessionID,
		UserEmail: userEmail,
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}
	start := time.Now().UTC()

	err := ti.service.persistToolCallEvent(ctx, &hooks.ClaudePayload{
		HookEventName: "ToolEvent",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		SessionID:     &sessionID,
	}, metadata)
	require.NoError(t, err)
	require.Empty(t, metadata.UserID, "test email should not resolve to a connected user")

	var logs []telemetryrepo.TelemetryLog
	require.Eventually(t, func() bool {
		var err error
		logs, err = chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     start.Add(-time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramChatID:    sessionID,
			EventSource:   "hook",
			SortOrder:     "desc",
			Limit:         10,
		})
		return err == nil && len(logs) == 1
	}, 2*time.Second, 50*time.Millisecond)

	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Attributes, userEmail)
}

func TestBuildTelemetryAttributesWithMetadata_SetsHookHostname(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	metadata := &SessionMetadata{
		SessionID: uuid.NewString(),
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}
	hostname := " subomi-mbp "
	attrs := ti.service.buildTelemetryAttributesWithMetadata(ctx, &hooks.ClaudePayload{
		HookEventName: "PreToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		SessionID:     &metadata.SessionID,
		HookHostname:  &hostname,
	}, metadata)

	assert.Equal(t, "subomi-mbp", attrs[attr.HookHostnameKey])
}

var (
	toolName   = "test_tool"
	toolUseID  = "toolu_123"
	toolName1  = "tool1"
	toolUseID1 = "toolu_123"
	toolName2  = "tool2"
	toolUseID2 = "toolu_234"
)

// TestBufferHook_AtomicAppend tests that buffering hooks uses atomic RPUSH.
// Exercises the OTEL fallback path (no plugin auth headers) so recordHook
// routes through the Redis buffer rather than the plugin attribution path.
func TestBufferHook_AtomicAppend(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ctx = otelOnlyCtx(ctx)

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

// TestBufferHook_MultipleConcurrent tests that concurrent buffering works
// correctly under the OTEL fallback path.
func TestBufferHook_MultipleConcurrent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ctx = otelOnlyCtx(ctx)

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

// TestFlushPendingHooks_DirectCall tests flushing by calling the flush method directly
func TestFlushPendingHooks_DirectCall(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()

	// Buffer multiple hooks using the cache directly
	cacheAdapter := cache.NewRedisCacheAdapter(ti.redisClient)
	numHooks := 5
	for range numHooks {
		payload := hooks.ClaudePayload{
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
		SessionID:     sessionID,
		ServiceName:   "test-service",
		UserEmail:     "test@example.com",
		UserID:        "",
		ExternalOrgID: "claude-org-123",
		GramOrgID:     uuid.NewString(),
		ProjectID:     uuid.NewString(),
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
		SessionID:     sessionID,
		ServiceName:   "test-service",
		UserEmail:     "test@example.com",
		UserID:        "",
		ExternalOrgID: "claude-org-123",
		GramOrgID:     uuid.NewString(),
		ProjectID:     uuid.NewString(),
	}

	// Call flushPendingHooks with no buffered hooks (should not error)
	ti.service.flushPendingHooks(ctx, sessionID, &metadata)

	// Verify no Redis key was created
	redisKey := "hook:pending:" + sessionID
	exists, err := ti.redisClient.Exists(ctx, redisKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists)
}

// TestBufferAndFlush_MultipleSessionsConcurrent exercises buffering and
// flushing across multiple sessions under the OTEL fallback path.
func TestBufferAndFlush_MultipleSessionsConcurrent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ctx = otelOnlyCtx(ctx)

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
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	metadata := SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   "test-service",
		UserEmail:     "user@example.com",
		UserID:        "",
		ExternalOrgID: "claude-org-456",
		GramOrgID:     uuid.NewString(),
		ProjectID:     uuid.NewString(),
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
	assert.Equal(t, metadata.ExternalOrgID, retrieved.ExternalOrgID)
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
	payloads := []hooks.ClaudePayload{
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

// TestMetrics_StampsAnthropicProviderOnUsage drives the Claude usage-metrics
// path end-to-end (Metrics -> writeMetricsToClickHouse) and asserts the
// persisted usage row carries provider=anthropic.
func TestMetrics_StampsAnthropicProviderOnUsage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	name := "claude_code.token.usage"
	payload := &hooks.MetricsPayload{
		ResourceMetrics: []*hooks.OTELResourceMetrics{{
			ScopeMetrics: []*hooks.OTELScopeMetrics{{
				Metrics: []*hooks.OTELMetric{{
					Name: &name,
					Sum: &hooks.OTELSum{
						AggregationTemporality: "AGGREGATION_TEMPORALITY_DELTA",
						DataPoints: []*hooks.OTELNumberDataPoint{{
							TimeUnixNano: new(nanoString(now)),
							AsInt:        "100",
							Attributes: []*hooks.OTELAttribute{
								strAttr("session.id", "claude-metrics-session"),
								strAttr("model", "claude-opus-4-8"),
								strAttr("type", "input"),
								strAttr("user.email", "metrics-user@example.com"),
							},
						}},
					},
				}},
			}},
		}},
	}

	require.NoError(t, ti.service.Metrics(ctx, payload))

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), "claude-code:usage:metrics", now, 1)
	require.Contains(t, logs[0].Attributes, providerAnthropic)
}
