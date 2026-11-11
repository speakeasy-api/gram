package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
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
		Hostname:  "host-" + uuid.NewString(),
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

func TestDeviceMCPList_ExpiresAfterADay(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scope := testDeviceScope(t, ctx)

	ti.service.cacheDeviceMCPList(ctx, scope, []MCPServerEntry{testMCPEntry("linear")})

	for _, identity := range scope.identities() {
		ttl, err := ti.redisClient.TTL(ctx, deviceMCPListCacheKey(scope, identity)).Result()
		require.NoError(t, err)
		require.LessOrEqual(t, ttl, deviceMCPListTTL)
		require.Greater(t, ttl, deviceMCPListTTL-time.Minute)
	}
	require.Equal(t, 24*time.Hour, deviceMCPListTTL)
}

// A new session's first hook arrives before its OTEL stream, so a copy written
// with a device id must still be found by hostname alone, and the reverse.
func TestDeviceMCPList_FoundByEitherDeviceIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scope := testDeviceScope(t, ctx)
	ti.service.cacheDeviceMCPList(ctx, scope, []MCPServerEntry{testMCPEntry("linear")})

	byHost := scope
	byHost.DeviceID = ""
	_, ok := ti.service.getDeviceMCPList(ctx, byHost)
	require.True(t, ok)

	byDevice := scope
	byDevice.Hostname = ""
	_, ok = ti.service.getDeviceMCPList(ctx, byDevice)
	require.True(t, ok)
}

func TestDeviceMCPList_Isolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*deviceMCPListScope)
	}{
		{name: "another user", mutate: func(s *deviceMCPListScope) { s.UserID = uuid.NewString() }},
		{name: "another device", mutate: func(s *deviceMCPListScope) { s.DeviceID, s.Hostname = "device-other", "host-other" }},
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
	scope.Hostname = ""

	identity := scope.identities()[0]
	require.NoError(t, ti.service.cache.Set(ctx, deviceMCPListCacheKey(scope, identity), deviceMCPList{
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
		{name: "no device identity", mutate: func(s *deviceMCPListScope) { s.DeviceID, s.Hostname = "", "" }},
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

	hostname := "dev-host-" + uuid.NewString()
	cwd := "/work/app"
	server := "linear"
	command := "npx linear-mcp"
	inventory := canonicalIngestPayload("claude", "session.started", uuid.NewString())
	inventory.Source.Hostname = &hostname
	inventory.Session.Cwd = &cwd
	inventory.Data = &gen.HookIngestData{
		McpInventory:          []*gen.HookMCPData{{ServerName: &server, ServerIdentity: &server, Command: &command}},
		McpInventoryCollected: new(true),
	}
	_, err := ti.service.Ingest(ctx, inventory)
	require.NoError(t, err)

	toolName := "mcp__linear__list_issues"
	callID := "call-1"
	next := canonicalIngestPayload("claude", "tool.requested", uuid.NewString())
	next.Source.Hostname = &hostname
	next.Session.Cwd = &cwd
	next.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{ID: &callID, Name: &toolName, Input: map[string]any{}},
		Mcp:      &gen.HookMCPData{ServerName: &server, ServerIdentity: &server},
	}
	result, err := ti.service.Ingest(ctx, next)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
}

// An incomplete inventory proves nothing and never becomes the device copy.
func TestIngest_IncompleteInventoryLeavesDeviceCopy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	hostname := "dev-host-" + uuid.NewString()
	cwd := "/work/app"
	server := "linear"
	command := "npx linear-mcp"
	payload := canonicalIngestPayload("claude", "session.started", uuid.NewString())
	payload.Source.Hostname = &hostname
	payload.Session.Cwd = &cwd
	payload.Data = &gen.HookIngestData{
		McpInventory:          []*gen.HookMCPData{{ServerName: &server, ServerIdentity: &server, Command: &command}},
		McpInventoryCollected: new(false),
	}
	_, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	actor := ti.service.resolveCanonicalActor(ctx, payload, authCtx)
	_, found := ti.service.getDeviceMCPList(ctx, ti.service.canonicalDeviceMCPListScope(ctx, authCtx, actor.UserID, payload))
	require.False(t, found)
}
