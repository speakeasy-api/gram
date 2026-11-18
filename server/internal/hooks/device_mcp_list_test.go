package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// testDeviceScope is a device scope in ctx's project for its user.
func testDeviceScope(t *testing.T, ctx context.Context) deviceMCPListScope {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotEmpty(t, authCtx.UserID)
	return deviceMCPListScope{
		ProjectID: authCtx.ProjectID.String(),
		UserID:    authCtx.UserID,
		DeviceID:  "device-" + uuid.NewString(),
		Adapter:   "claude",
		Cwd:       "/work/app",
	}
}

func TestDeviceMCPList_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scope := testDeviceScope(t, ctx)

	ti.service.cacheDeviceMCPList(ctx, scope, []MCPServerEntry{testMCPEntry("linear")})

	entries, ok := ti.service.getDeviceMCPList(ctx, scope)
	require.True(t, ok)
	require.Equal(t, []MCPServerEntry{testMCPEntry("linear")}, entries)
}

// An empty but complete inventory is still a copy: it proves no servers are
// configured.
func TestDeviceMCPList_EmptyInventoryIsStored(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scope := testDeviceScope(t, ctx)

	ti.service.cacheDeviceMCPList(ctx, scope, nil)

	entries, ok := ti.service.getDeviceMCPList(ctx, scope)
	require.True(t, ok)
	require.Empty(t, entries)
}

func TestDeviceMCPList_ExpiresAfterFourteenDays(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scope := testDeviceScope(t, ctx)

	ti.service.cacheDeviceMCPList(ctx, scope, []MCPServerEntry{testMCPEntry("linear")})

	ttl, err := ti.redisClient.TTL(ctx, deviceMCPListCacheKey(scope)).Result()
	require.NoError(t, err)
	require.LessOrEqual(t, ttl, deviceMCPListTTL)
	require.Greater(t, ttl, deviceMCPListTTL-time.Minute)
	require.Equal(t, 14*24*time.Hour, deviceMCPListTTL)
}

// Once the copy's 14-day TTL elapses the guard has nothing to fall back to: a
// server only the copy listed is classified without install evidence again.
func TestDeviceMCPList_ExpiredCopyIsNotRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	tc := canonicalBuiltinCase{
		name:            "expired device copy",
		toolName:        "mcp__linear__list_issues",
		deviceInventory: []MCPServerEntry{{Source: "claude", Name: "linear", Command: "npx linear-mcp"}},
	}
	payload := canonicalBuiltinPayload(tc, "tool.requested", uuid.NewString(), "")
	seedCanonicalBuiltinInventories(t, ctx, ti, tc, payload)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	actor := ti.service.resolveCanonicalActor(ctx, payload, authCtx)
	scope := ti.service.canonicalDeviceMCPListScope(ctx, authCtx, actor.UserID, payload)
	_, found := ti.service.getDeviceMCPList(ctx, scope)
	require.True(t, found, "the copy is readable before it expires")
	before, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", before.Decision, "the live copy is install evidence")

	// Expire the copy, as its 14-day TTL would.
	key := deviceMCPListCacheKey(scope)
	ttl, err := ti.redisClient.TTL(ctx, key).Result()
	require.NoError(t, err)
	require.LessOrEqual(t, ttl, 14*24*time.Hour)
	require.NoError(t, ti.redisClient.PExpire(ctx, key, time.Millisecond).Err())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		exists, err := ti.redisClient.Exists(ctx, key).Result()
		assert.NoError(c, err)
		assert.Zero(c, exists)
	}, 5*time.Second, 10*time.Millisecond)

	_, found = ti.service.getDeviceMCPList(ctx, scope)
	require.False(t, found)

	after, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", after.Decision, "an expired copy is no install evidence")
}

// sessionMCPListSetFailingCache fails every write of a session MCP list
// snapshot and passes everything else through.
type sessionMCPListSetFailingCache struct {
	cache.Cache
}

func (c sessionMCPListSetFailingCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if strings.HasPrefix(key, "session:mcp-list:") {
		return errors.New("snapshot write failed")
	}
	//nolint:wrapcheck // test pass-through to the embedded real cache
	return c.Cache.Set(ctx, key, value, ttl)
}

// On the Claude endpoint a device copy is written only after the session
// snapshot itself was stored, so a failed session write leaves no copy.
func TestClaudeMCPListSnapshot_DeviceCopyNeedsStoredSnapshot(t *testing.T) {
	t.Parallel()

	for _, sessionWriteFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "stored snapshot", true: "failed snapshot"}[sessionWriteFails], func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.Email)
			sessionID := uuid.NewString()
			seedSessionDeviceID(t, ctx, ti, sessionID, "device-"+uuid.NewString())
			if sessionWriteFails {
				ti.service.cache = sessionMCPListSetFailingCache{Cache: ti.service.cache}
			}
			userEmail := *authCtx.Email
			cwd := "/work/app"
			payload := &gen.ClaudePayload{
				HookEventName: "SessionStart",
				SessionID:     &sessionID,
				UserEmail:     &userEmail,
				Cwd:           &cwd,
				AdditionalData: map[string]any{
					"mcp_inventory_claude_code": "linear: npx linear-mcp - connected",
				},
			}
			ti.service.captureMCPListSnapshot(ctx, payload)

			metadata, err := ti.service.resolveClaudeSessionMetadata(ctx, sessionID, userEmail)
			require.NoError(t, err)
			require.NotEmpty(t, metadata.UserID)
			require.NotEmpty(t, metadata.DeviceID)
			_, found := ti.service.getDeviceMCPList(ctx, claudeDeviceMCPListScope(metadata, payload))
			require.Equal(t, !sessionWriteFails, found)
		})
	}
}

// Without a device id there is no copy to read: the hostname is not a device
// identity, so a session whose OTEL stream has not reported one yet misses.
func TestDeviceMCPList_NeedsDeviceID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scope := testDeviceScope(t, ctx)
	ti.service.cacheDeviceMCPList(ctx, scope, []MCPServerEntry{testMCPEntry("linear")})

	withoutDevice := scope
	withoutDevice.DeviceID = ""
	_, ok := ti.service.getDeviceMCPList(ctx, withoutDevice)
	require.False(t, ok)
}

func TestDeviceMCPList_Isolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*deviceMCPListScope)
	}{
		{name: "another user", mutate: func(s *deviceMCPListScope) { s.UserID = uuid.NewString() }},
		{name: "another device", mutate: func(s *deviceMCPListScope) { s.DeviceID = "device-other" }},
		{name: "another adapter", mutate: func(s *deviceMCPListScope) { s.Adapter = "codex" }},
		{name: "another working directory", mutate: func(s *deviceMCPListScope) { s.Cwd = "/work/other" }},
		{name: "another project", mutate: func(s *deviceMCPListScope) { s.ProjectID = uuid.NewString() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			scope := testDeviceScope(t, ctx)
			ti.service.cacheDeviceMCPList(ctx, scope, []MCPServerEntry{testMCPEntry("linear")})

			other := scope
			tc.mutate(&other)
			_, ok := ti.service.getDeviceMCPList(ctx, other)
			require.False(t, ok)
		})
	}
}

// A copy stored under the reader's key but written for someone else, however
// it got there, is never returned.
func TestDeviceMCPList_RejectsCopyOfAnotherUser(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scope := testDeviceScope(t, ctx)

	require.NoError(t, ti.service.cache.Set(ctx, deviceMCPListCacheKey(scope), deviceMCPList{
		Owner:   mcpListOwner{Actor: ""},
		UserID:  uuid.NewString(),
		Entries: []MCPServerEntry{testMCPEntry("intruder")},
	}, deviceMCPListTTL))

	_, ok := ti.service.getDeviceMCPList(ctx, scope)
	require.False(t, ok)
}

// Only a sender authenticated to the scope's project writes a copy.
func TestDeviceMCPList_WriteNeedsTheScopeProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scope := testDeviceScope(t, ctx)

	ti.service.cacheDeviceMCPList(otherProjectContext(t, ctx), scope, []MCPServerEntry{testMCPEntry("intruder")})

	_, ok := ti.service.getDeviceMCPList(ctx, scope)
	require.False(t, ok)
}

// Agents carry no device identity: they neither write nor read a copy.
func TestDeviceMCPList_AgentsNeitherWriteNorRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	agentCtx := agentKeyContext(t, ctx, ti)
	scope := testDeviceScope(t, ctx)

	ti.service.cacheDeviceMCPList(agentCtx, scope, []MCPServerEntry{testMCPEntry("agent")})
	_, ok := ti.service.getDeviceMCPList(ctx, scope)
	require.False(t, ok)

	ti.service.cacheDeviceMCPList(ctx, scope, []MCPServerEntry{testMCPEntry("human")})
	_, ok = ti.service.getDeviceMCPList(agentCtx, scope)
	require.False(t, ok)
}

func TestDeviceMCPList_UnusableScopes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	tests := []struct {
		name   string
		mutate func(*deviceMCPListScope)
	}{
		{name: "no user", mutate: func(s *deviceMCPListScope) { s.UserID = "" }},
		{name: "no device id", mutate: func(s *deviceMCPListScope) { s.DeviceID = "" }},
		{name: "no adapter", mutate: func(s *deviceMCPListScope) { s.Adapter = "" }},
	}
	for _, tc := range tests {
		scope := testDeviceScope(t, ctx)
		tc.mutate(&scope)
		ti.service.cacheDeviceMCPList(ctx, scope, []MCPServerEntry{testMCPEntry("linear")})
		_, ok := ti.service.getDeviceMCPList(ctx, scope)
		require.False(t, ok, tc.name)
	}
}

// A complete inventory a canonical session reports becomes the device copy, so
// the next session on the same device is classified against it before its own
// inventory arrives.
func TestIngest_CompleteInventoryRefreshesDeviceCopy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	deviceID := "device-" + uuid.NewString()
	cwd := "/work/app"
	server := "linear"
	command := "npx linear-mcp"
	firstSession := uuid.NewString()
	seedSessionDeviceID(t, ctx, ti, firstSession, deviceID)
	inventory := canonicalIngestPayload("claude", "session.started", firstSession)
	inventory.Session.Cwd = &cwd
	inventory.Data = &gen.HookIngestData{
		McpInventory:          []*gen.HookMCPData{{ServerName: &server, ServerIdentity: &server, Command: &command}},
		McpInventoryCollected: new(true),
	}
	_, err := ti.service.Ingest(ctx, inventory)
	require.NoError(t, err)

	toolName := "mcp__linear__list_issues"
	callID := "call-1"
	nextSession := uuid.NewString()
	seedSessionDeviceID(t, ctx, ti, nextSession, deviceID)
	next := canonicalIngestPayload("claude", "tool.requested", nextSession)
	next.Session.Cwd = &cwd
	next.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{ID: &callID, Name: &toolName, Input: map[string]any{}},
		Mcp:      &gen.HookMCPData{ServerName: &server, ServerIdentity: &server},
	}
	result, err := ti.service.Ingest(ctx, next)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
}
