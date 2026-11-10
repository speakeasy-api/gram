package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestIngest_ShadowMCPBlockAllExemptsAgentBuiltinServers: a block_all Shadow
// MCP policy governs MCP servers people configure, not the coding agent's own
// in-process servers. Those arrive with no URL and no launch command, and only
// then, with a loaded session inventory that confirms it, does their name
// exempt them: a stdio or remote server named like a built-in, or any call
// whose inventory cannot be read, is still denied.
func TestIngest_ShadowMCPBlockAllExemptsAgentBuiltinServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		toolName   string
		url        string
		command    string
		identity   string
		inventory  []MCPServerEntry
		wantDenied bool
	}{
		{name: "claude in chrome is allowed", toolName: "mcp__claude-in-chrome__tabs_context_mcp", inventory: []MCPServerEntry{}, wantDenied: false},
		{name: "desktop app task tool is allowed", toolName: "mcp__ccd_turn__wrap_up", inventory: []MCPServerEntry{}, wantDenied: false},
		{name: "desktop app browser pane is allowed", toolName: "mcp__Claude_Browser__navigate", inventory: []MCPServerEntry{}, wantDenied: false},
		{
			name:       "built-in name without a session inventory is denied",
			toolName:   "mcp__claude-in-chrome__tabs_context_mcp",
			inventory:  nil,
			wantDenied: true,
		},
		{
			name:       "built-in name with a launch command as server identity is denied",
			toolName:   "mcp__claude-in-chrome__tabs_context_mcp",
			identity:   "npx some-mcp",
			inventory:  []MCPServerEntry{},
			wantDenied: true,
		},
		{
			name:       "unlisted ccd name is denied",
			toolName:   "mcp__ccd_custom__run",
			inventory:  []MCPServerEntry{},
			wantDenied: true,
		},
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
			if tc.identity != "" {
				mcp.ServerIdentity = &tc.identity
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
			require.Equal(t, "allow", result.Decision)
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

// builtinScanValidator denies every call on the signature fallback, so only
// recorded provenance can keep a scanned call clean.
type builtinScanValidator struct{}

func (builtinScanValidator) ValidateToolsetCall(context.Context, any, string, string) (string, bool) {
	return "missing toolset signature", true
}

// builtinScanHosted trusts only the built-in Speakeasy hosts.
type builtinScanHosted struct{}

func (builtinScanHosted) TrustedMCPHostsForOrg(context.Context, string) ([]string, error) {
	return nil, nil
}

// builtinScanProvenance replays fixed provenance rows by tool call id.
type builtinScanProvenance struct {
	rows map[string]telemetryrepo.MCPProvenance
}

func (p builtinScanProvenance) LookupMCPProvenanceByToolCallID(context.Context, uuid.UUID, []string, time.Time) (map[string]telemetryrepo.MCPProvenance, error) {
	return p.rows, nil
}

// TestCanonicalIngest_AgentBuiltinServerFindings: the findings scanner trusts a
// built-in name only through the provenance row ingest records. A canonical
// built-in call with a loaded session inventory records the bare server name as
// gram.mcp.match and yields no finding; without an inventory it records no
// match and the call is still reported, as is a stdio server borrowing the name.
func TestCanonicalIngest_AgentBuiltinServerFindings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		toolName    string
		command     string
		inventory   []MCPServerEntry
		wantFinding bool
	}{
		{
			name:        "built-in with a loaded inventory yields no finding",
			toolName:    "mcp__claude-in-chrome__tabs_context_mcp",
			inventory:   []MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp"}},
			wantFinding: false,
		},
		{
			name:        "desktop app built-in with a loaded inventory yields no finding",
			toolName:    "mcp__ccd_turn__wrap_up",
			inventory:   []MCPServerEntry{},
			wantFinding: false,
		},
		{
			name:        "built-in without a session inventory is reported",
			toolName:    "mcp__claude-in-chrome__tabs_context_mcp",
			inventory:   nil,
			wantFinding: true,
		},
		{
			name:        "built-in name with a stdio command is reported",
			toolName:    "mcp__claude-in-chrome__tabs_context_mcp",
			command:     "npx some-mcp",
			inventory:   []MCPServerEntry{},
			wantFinding: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			sessionID := uuid.NewString()
			if tc.inventory != nil {
				require.NoError(t, ti.service.cache.Set(ctx,
					sessionMCPListCacheKey(testProjectID(t, ctx), sessionID), tc.inventory, sessionMCPListTTL))
			}

			serverName := mcpServerIdentityFromToolName(tc.toolName)
			mcp := &gen.HookMCPData{ServerName: &serverName, ServerIdentity: &serverName}
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

			rows := ti.service.canonicalTelemetryRows(ctx, payload, authCtx, &SessionMetadata{}, "claude", time.Now(), "")
			require.NotEmpty(t, rows)

			// The provenance query admits a row only when it carries a match
			// or a server URL, and reads the tool call source as identity.
			attrs := rows[0].attrs
			match, _ := attrs[attr.MCPMatchKey].(string)
			serverURL, _ := attrs[attr.MCPServerURLKey].(string)
			source, _ := attrs[attr.ToolCallSourceKey].(string)
			recorded := map[string]telemetryrepo.MCPProvenance{}
			if match != "" || serverURL != "" {
				recorded[callID] = telemetryrepo.MCPProvenance{Match: match, ServerURL: serverURL, ServerIdentity: source, HookSource: "claude"}
			}

			scanner := shadowmcpscan.NewScanner(testenv.NewLogger(t), builtinScanValidator{}, builtinScanHosted{}, builtinScanProvenance{rows: recorded}, nil)
			out := scanner.Scan(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, uuid.Nil, []shadowmcpscan.Message{
				{UserID: "", ToolCalls: []shadowmcpscan.ToolCall{{ID: callID, Name: tc.toolName, Arguments: `{}`, CreatedAt: time.Now(), Sender: "claude"}}},
			})
			require.Len(t, out, 1)
			if tc.wantFinding {
				require.Len(t, out[0], 1)
				return
			}
			require.Empty(t, out[0])
		})
	}
}
