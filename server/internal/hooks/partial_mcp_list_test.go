package hooks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// ingestCanonicalInventory reports an MCP inventory for a session through
// canonical ingest, complete or partial.
func ingestCanonicalInventory(t *testing.T, ctx context.Context, ti *testInstance, sessionID string, complete bool, entries ...*gen.HookMCPData) {
	t.Helper()
	payload := canonicalIngestPayload("claude", "session.started", sessionID)
	payload.Data = &gen.HookIngestData{
		McpInventory:          entries,
		McpInventoryCollected: new(complete),
	}
	_, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
}

// ingestCanonicalLinearCall sends a tool call to the linear server, whose
// payload states no URL or command, and returns the guard's decision.
func ingestCanonicalLinearCall(t *testing.T, ctx context.Context, ti *testInstance, sessionID string) string {
	t.Helper()
	server := "linear"
	toolName := "mcp__linear__list_issues"
	callID := "call-1"
	payload := canonicalIngestPayload("claude", "tool.requested", sessionID)
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{ID: &callID, Name: &toolName, Input: map[string]any{}},
		Mcp:      &gen.HookMCPData{ServerName: &server, ServerIdentity: &server},
	}
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result.Decision
}

func linearInventoryEntry(url, command string) *gen.HookMCPData {
	server := "linear"
	entry := &gen.HookMCPData{ServerName: &server, ServerIdentity: &server}
	if url != "" {
		entry.URL = &url
	}
	if command != "" {
		entry.Command = &command
	}
	return entry
}

// TestIngest_PartialMCPInventoryIsInstallEvidence: the partial list a sender
// reports before its complete list is ready supplies a server's URL, command
// and install evidence exactly like a snapshot entry.
func TestIngest_PartialMCPInventoryIsInstallEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		entry        *gen.HookMCPData
		wantDecision string
	}{
		{name: "partial entry with a launch command is denied", entry: linearInventoryEntry("", "npx linear-mcp"), wantDecision: "deny"},
		{name: "partial entry with a non-Speakeasy URL is denied", entry: linearInventoryEntry("https://mcp.example.test/mcp", ""), wantDecision: "deny"},
		{name: "partial entry with a Speakeasy-hosted URL is allowed", entry: linearInventoryEntry(builtinTestHostedURL, ""), wantDecision: "allow"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.riskScanner = stubBlockingShadowMCPScanner{}
			sessionID := uuid.NewString()

			ingestCanonicalInventory(t, ctx, ti, sessionID, false, tc.entry)

			require.Equal(t, tc.wantDecision, ingestCanonicalLinearCall(t, ctx, ti, sessionID))
		})
	}
}

// TestIngest_PartialMCPInventoryNeverProvesAbsence: a server a partial list
// omits stays unverified, and the partial list never claims a complete read,
// so neither the built-in label nor the Codex meta-tool guard treats it as
// proof that the server is not configured.
func TestIngest_PartialMCPInventoryNeverProvesAbsence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	sessionID := uuid.NewString()

	other := "mise"
	command := "mise mcp"
	ingestCanonicalInventory(t, ctx, ti, sessionID, false, &gen.HookMCPData{ServerName: &other, ServerIdentity: &other, Command: &command})

	require.Equal(t, "allow", ingestCanonicalLinearCall(t, ctx, ti, sessionID))

	payload := canonicalBuiltinPayload(canonicalBuiltinCase{toolName: "mcp__ccd_new_tool__run"}, "tool.requested", sessionID, "")
	rows := ti.service.canonicalTelemetryRows(ctx, payload, authCtx, &SessionMetadata{UserID: authCtx.UserID}, "claude", time.Now(), "")
	require.NotEmpty(t, rows)
	require.Equal(t, "unverified", rows[0].attrs[attr.MCPBuiltinEvidenceKey])

	var read bool
	err := ti.service.cache.Get(ctx, sessionMCPInventoryReadCacheKey(testProjectID(t, ctx), sessionID), &read)
	require.Error(t, err, "a partial list must not record a complete inventory read")
	_, err = ti.service.getCachedMCPList(ctx, sessionID)
	require.Error(t, err, "a partial list must not become the session snapshot")
}

// TestIngest_CompleteMCPInventoryWinsOverEarlierPartial: once the complete
// list arrives it decides, even when the earlier partial list named the server.
func TestIngest_CompleteMCPInventoryWinsOverEarlierPartial(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}
	sessionID := uuid.NewString()

	ingestCanonicalInventory(t, ctx, ti, sessionID, false, linearInventoryEntry("", "npx linear-mcp"))
	require.Equal(t, "deny", ingestCanonicalLinearCall(t, ctx, ti, sessionID))

	ingestCanonicalInventory(t, ctx, ti, sessionID, true, linearInventoryEntry(builtinTestHostedURL, ""))
	require.Equal(t, "allow", ingestCanonicalLinearCall(t, ctx, ti, sessionID))
}

// TestIngest_LaterPartialMCPInventoryNeverDowngradesComplete: a partial list
// that arrives after the complete list neither replaces nor outranks it.
func TestIngest_LaterPartialMCPInventoryNeverDowngradesComplete(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}
	sessionID := uuid.NewString()

	ingestCanonicalInventory(t, ctx, ti, sessionID, true, linearInventoryEntry(builtinTestHostedURL, ""))
	ingestCanonicalInventory(t, ctx, ti, sessionID, false, linearInventoryEntry("", "npx linear-mcp"))

	require.Equal(t, "allow", ingestCanonicalLinearCall(t, ctx, ti, sessionID))

	entries, err := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, builtinTestHostedURL, entries[0].URL)
	_, err = ti.service.getCachedPartialMCPList(ctx, sessionID)
	require.Error(t, err, "no partial list is stored beside a complete snapshot")
}

// A partial list never becomes the device copy, which only a complete list
// may write.
func TestIngest_PartialMCPInventoryLeavesDeviceCopy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	sessionID := uuid.NewString()
	seedSessionDeviceID(t, ctx, ti, sessionID, "device-"+uuid.NewString())

	ingestCanonicalInventory(t, ctx, ti, sessionID, false, linearInventoryEntry("", "npx linear-mcp"))

	payload := canonicalIngestPayload("claude", "tool.requested", sessionID)
	actor := ti.service.resolveCanonicalActor(ctx, payload, authCtx)
	scope := ti.service.canonicalDeviceMCPListScope(ctx, authCtx, actor.UserID, payload)
	require.NotEmpty(t, scope.DeviceID)
	_, found := ti.service.getDeviceMCPList(ctx, scope)
	require.False(t, found)
}

// TestClaude_PreToolUse_PartialMCPInventory: on the Claude endpoint a partial
// list entry is judged by its URL or command, so a claude.ai connector or
// plugin server it names is no longer held back by the missing-inventory deny.
func TestClaude_PreToolUse_PartialMCPInventory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolName string
		partial  []MCPServerEntry
		want     string
		restart  bool
	}{
		{
			name:     "plugin server with a Speakeasy-hosted partial URL is allowed",
			toolName: "mcp__plugin_team_team__list_tools",
			partial:  []MCPServerEntry{{Source: "plugin", PluginName: "team", Name: "team", URL: builtinTestHostedURL}},
			want:     "allow",
		},
		{
			name:     "claude.ai connector with a third-party partial URL is denied by policy",
			toolName: "mcp__claude_ai_Linear__list_issues",
			partial:  []MCPServerEntry{{Source: "claude.ai", Name: "Linear", URL: "https://mcp.example.test/mcp"}},
			want:     "deny",
		},
		{
			name:     "server with a partial launch command is denied by policy",
			toolName: "mcp__linear__list_issues",
			partial:  []MCPServerEntry{{Source: "local", Name: "linear", Command: "npx linear-mcp"}},
			want:     "deny",
		},
		{
			name:     "plugin server the partial list omits waits for the inventory",
			toolName: "mcp__plugin_team_team__list_tools",
			partial:  []MCPServerEntry{{Source: "local", Name: "linear", Command: "npx linear-mcp"}},
			want:     "deny",
			restart:  true,
		},
		{name: "built-in the partial list omits is allowed", toolName: "mcp__ccd_new_tool__run", partial: []MCPServerEntry{{Source: "local", Name: "linear"}}, want: "allow"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.productFeatures = alwaysEnabledFeatures{}
			ti.service.riskScanner = stubBlockingShadowMCPScanner{}

			sessionID := uuid.NewString()
			require.NoError(t, ti.service.cache.Set(ctx,
				sessionPartialMCPListCacheKey(testProjectID(t, ctx), sessionID), tc.partial, sessionMCPListTTL))

			toolUseID := "toolu_partial"
			userEmail := "claude-partial@example.com"
			result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
				HookEventName: "PreToolUse",
				SessionID:     &sessionID,
				UserEmail:     &userEmail,
				ToolName:      &tc.toolName,
				ToolUseID:     &toolUseID,
				ToolInput:     map[string]any{},
			})
			require.NoError(t, err)
			require.NotNil(t, result)

			output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
			require.True(t, ok)
			require.NotNil(t, output.PermissionDecision)
			require.Equal(t, tc.want, *output.PermissionDecision)
			if tc.want != "deny" {
				return
			}
			require.NotNil(t, output.PermissionDecisionReason)
			require.Equal(t, tc.restart, strings.Contains(*output.PermissionDecisionReason, "restart Claude Code"))
		})
	}
}
