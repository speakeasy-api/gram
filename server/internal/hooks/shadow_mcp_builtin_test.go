package hooks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

// TestIngest_ShadowMCPBlockAllExemptsAgentBuiltinServers: a block_all Shadow
// MCP policy governs MCP servers people configure, not the coding agent's own
// in-process servers. Those arrive with no URL and no launch command, and only
// then does their name exempt them: a stdio or remote server named like a
// built-in is still denied.
func TestIngest_ShadowMCPBlockAllExemptsAgentBuiltinServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		toolName   string
		url        string
		command    string
		inventory  []MCPServerEntry
		wantDenied bool
	}{
		{name: "claude in chrome is allowed", toolName: "mcp__claude-in-chrome__tabs_context_mcp", wantDenied: false},
		{name: "desktop app task tool is allowed", toolName: "mcp__ccd_turn__wrap_up", wantDenied: false},
		{name: "desktop app browser pane is allowed", toolName: "mcp__Claude_Browser__navigate", wantDenied: false},
		{
			name:       "built-in name with a stdio command is denied",
			toolName:   "mcp__claude-in-chrome__tabs_context_mcp",
			command:    "npx some-mcp",
			wantDenied: true,
		},
		{
			name:       "built-in name with an external URL is denied",
			toolName:   "mcp__ccd_turn__wrap_up",
			url:        "https://mcp.example.test/mcp",
			wantDenied: true,
		},
		{
			name:       "built-in name configured as stdio in the session inventory is denied",
			toolName:   "mcp__claude-in-chrome__tabs_context_mcp",
			inventory:  []MCPServerEntry{{Source: "local", Name: "claude-in-chrome", Command: "npx some-mcp"}},
			wantDenied: true,
		},
		{name: "third-party server without transport is denied", toolName: "mcp__linear__list_issues", wantDenied: true},
		{
			name:       "Speakeasy-hosted URL is allowed",
			toolName:   "mcp__speakeasy-team__list_tools",
			url:        "https://app.getgram.ai/mcp/speakeasy-team-8g3az",
			wantDenied: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.riskScanner = stubBlockingShadowMCPScanner{}

			sessionID := uuid.NewString()
			if tc.inventory != nil {
				require.NoError(t, ti.service.cache.Set(ctx,
					sessionMCPListCacheKey(testProjectID(t, ctx), sessionID), tc.inventory, sessionMCPListTTL))
			}

			serverName := mcpServerIdentityFromToolName(tc.toolName)
			mcp := &gen.HookMCPData{ServerName: &serverName, ServerIdentity: &serverName}
			if tc.url != "" {
				mcp.URL = &tc.url
			}
			if tc.command != "" {
				mcp.Command = &tc.command
				mcp.ServerIdentity = &tc.command
			}
			callID := "call-1"
			payload := canonicalIngestPayload("claude", "tool.requested", sessionID)
			payload.Data = &gen.HookIngestData{
				ToolCall: &gen.HookToolCallData{ID: &callID, Name: &tc.toolName, Input: map[string]any{}},
				Mcp:      mcp,
			}

			result, err := ti.service.Ingest(ctx, payload)
			require.NoError(t, err)
			require.NotNil(t, result)
			if tc.wantDenied {
				require.Equal(t, "deny", result.Decision)
				return
			}
			require.NotEqual(t, "deny", result.Decision)
		})
	}
}

// TestClaude_PreToolUse_ShadowMCPBlockAllExemptsAgentBuiltinServers: built-in
// servers never appear in `claude mcp list`, so the PreToolUse guard must not
// deny them as missing from the configuration, while an inventory entry with a
// launch command under the same name keeps the stdio deny.
func TestClaude_PreToolUse_ShadowMCPBlockAllExemptsAgentBuiltinServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		toolName  string
		inventory []MCPServerEntry
		want      string
	}{
		{
			name:      "claude in chrome missing from inventory is allowed",
			toolName:  "mcp__claude-in-chrome__tabs_context_mcp",
			inventory: []MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp"}},
			want:      "allow",
		},
		{
			name:      "desktop app tool missing from inventory is allowed",
			toolName:  "mcp__ccd_session__start_session",
			inventory: []MCPServerEntry{},
			want:      "allow",
		},
		{
			name:      "built-in name configured as stdio is denied",
			toolName:  "mcp__claude-in-chrome__tabs_context_mcp",
			inventory: []MCPServerEntry{{Source: "local", Name: "claude-in-chrome", Command: "npx some-mcp"}},
			want:      "deny",
		},
		{
			name:      "third-party server missing from inventory is denied",
			toolName:  "mcp__linear__list_issues",
			inventory: []MCPServerEntry{},
			want:      "deny",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.productFeatures = alwaysEnabledFeatures{}
			ti.service.riskScanner = stubBlockingShadowMCPScanner{}

			sessionID := uuid.NewString()
			require.NoError(t, ti.service.cache.Set(ctx,
				sessionMCPListCacheKey(testProjectID(t, ctx), sessionID), tc.inventory, sessionMCPListTTL))

			toolUseID := "toolu_builtin"
			userEmail := "claude-builtin@example.com"
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
		})
	}
}
