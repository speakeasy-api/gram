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
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	// builtinTestConnector is a connector UUID server name, as the Claude
	// desktop app and Cowork use for claude.ai connectors.
	builtinTestConnector = "1c55c87d-ac7c-4dfc-95f1-69e84cb5f92c"

	// builtinTestHostedURL is a Speakeasy-hosted MCP URL.
	builtinTestHostedURL = "https://app.getgram.ai/mcp/team-tools-8g3az"

	// builtinTestVendorCommand is a Codex bundled server launch command.
	builtinTestVendorCommand = "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl"
)

// canonicalBuiltinCase is one canonical tool call under a block_all Shadow MCP
// policy.
type canonicalBuiltinCase struct {
	name       string
	adapter    string
	toolName   string
	serverName string
	identity   string
	url        string
	command    string

	// inventory is the session MCP inventory; nil means none was reported.
	inventory []MCPServerEntry

	// deviceInventory is the device copy; nil means there is none.
	deviceInventory []MCPServerEntry

	wantDenied bool
}

func canonicalBuiltinPayload(tc canonicalBuiltinCase, eventType, sessionID, command string) *gen.IngestPayload {
	adapter := conv.Default(tc.adapter, "claude")
	serverName := conv.Default(tc.serverName, mcpServerIdentityFromToolName(tc.toolName))
	mcp := &gen.HookMCPData{ServerName: &serverName, ServerIdentity: &serverName}
	if tc.url != "" {
		mcp.URL = &tc.url
	}
	if command != "" {
		mcp.Command = &command
		mcp.ServerIdentity = &command
	}
	if tc.identity != "" {
		mcp.ServerIdentity = &tc.identity
	}
	callID := "call-1"
	hostname := "dev-host"
	cwd := "/work/app"
	payload := canonicalIngestPayload(adapter, eventType, sessionID)
	payload.Source.Hostname = &hostname
	payload.Session.Cwd = &cwd
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{ID: &callID, Name: &tc.toolName, Input: map[string]any{}},
		Mcp:      mcp,
	}
	return payload
}

// seedCanonicalBuiltinInventories writes the case's session inventory and
// device copy.
func seedCanonicalBuiltinInventories(t *testing.T, ctx context.Context, ti *testInstance, tc canonicalBuiltinCase, payload *gen.IngestPayload) {
	t.Helper()
	if tc.inventory != nil {
		require.NoError(t, ti.service.cache.Set(ctx,
			sessionMCPListCacheKey(testProjectID(t, ctx), canonicalSessionID(payload)), tc.inventory, sessionMCPListTTL))
	}
	if tc.deviceInventory != nil {
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		actor := ti.service.resolveCanonicalActor(ctx, payload, authCtx)
		require.NotEmpty(t, actor.UserID)
		ti.service.cacheDeviceMCPList(ctx, ti.service.canonicalDeviceMCPListScope(ctx, authCtx, actor.UserID, payload), tc.deviceInventory)
	}
}

// TestIngest_ShadowMCPBlockAllClassifiesServers: a block_all Shadow MCP policy
// governs servers the user installed, never a coding agent's own built-ins. A
// server with no URL, no launch command and no install evidence is a built-in
// whatever its name, with or without an inventory.
func TestIngest_ShadowMCPBlockAllClassifiesServers(t *testing.T) {
	t.Parallel()

	const linearTool = "mcp__linear__list_issues"
	tests := []canonicalBuiltinCase{
		{name: "known built-in with an inventory is allowed", toolName: "mcp__claude-in-chrome__tabs_context_mcp", inventory: []MCPServerEntry{}},
		{name: "known built-in without an inventory is allowed", toolName: "mcp__ccd_turn__wrap_up"},
		{name: "unlisted name without an inventory is allowed", toolName: "mcp__ccd_new_tool__run"},
		{name: "unlisted name missing from the inventory is allowed", toolName: linearTool, inventory: []MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp"}}},
		{name: "server without transport or inventory is allowed", toolName: linearTool},
		{
			name:       "server listed in the inventory without transport is denied",
			toolName:   linearTool,
			inventory:  []MCPServerEntry{{Source: "claude", Name: "linear"}},
			wantDenied: true,
		},
		{
			name:       "known built-in name listed with a launch command is denied",
			toolName:   "mcp__claude-in-chrome__tabs_context_mcp",
			inventory:  []MCPServerEntry{{Source: "local", Name: "claude-in-chrome", Command: "npx some-mcp"}},
			wantDenied: true,
		},
		{
			name:       "server listed with a third-party URL is denied",
			toolName:   linearTool,
			inventory:  []MCPServerEntry{{Source: "claude", Name: "linear", URL: "https://mcp.example.test/mcp"}},
			wantDenied: true,
		},
		{name: "server listed with a Speakeasy-hosted URL is allowed", toolName: linearTool, inventory: []MCPServerEntry{{Source: "claude", Name: "linear", URL: builtinTestHostedURL}}},
		{name: "server listed with a vendor bundle command is allowed", toolName: "mcp__computer_use__click", inventory: []MCPServerEntry{{Source: "codex", Name: "computer_use", Command: builtinTestVendorCommand}}},
		{name: "payload with a vendor bundle command is allowed", toolName: "mcp__computer_use__click", command: builtinTestVendorCommand},
		{name: "payload with a user launch command is denied", toolName: "mcp__claude-in-chrome__tabs_context_mcp", command: "npx some-mcp", wantDenied: true},
		{name: "launch command reported as server identity is denied", toolName: "mcp__claude-in-chrome__tabs_context_mcp", identity: "npx some-mcp", wantDenied: true},
		{name: "payload with an external URL is denied", toolName: "mcp__ccd_turn__wrap_up", url: "https://mcp.example.test/mcp", wantDenied: true},
		{name: "payload with a Speakeasy-hosted URL is allowed", toolName: "mcp__team-tools__list_tools", url: builtinTestHostedURL},
		{name: "connector UUID without an inventory is denied", toolName: "mcp__" + builtinTestConnector + "__list_issues", wantDenied: true},
		{
			name:       "connector UUID missing from the inventory is denied",
			toolName:   "mcp__" + builtinTestConnector + "__list_issues",
			inventory:  []MCPServerEntry{},
			wantDenied: true,
		},
		{
			name:      "connector UUID resolved to a Speakeasy-hosted URL is allowed",
			toolName:  "mcp__" + builtinTestConnector + "__list_issues",
			inventory: []MCPServerEntry{{Source: "cowork", Name: "Team tools", ConnectorUUID: builtinTestConnector, URL: builtinTestHostedURL}},
		},
		{
			name:       "cursor plugin server without transport is denied",
			adapter:    cursorAdapter,
			toolName:   "MCP:list_issues",
			serverName: "Linear",
			identity:   "plugin-acme-Linear",
			wantDenied: true,
		},
		{
			name:       "cursor extension server without transport is denied",
			adapter:    cursorAdapter,
			toolName:   "MCP:track",
			serverName: "extension-Tracker",
			identity:   "user-vendor.ext-Tracker",
			wantDenied: true,
		},
		{name: "cursor built-in server is allowed", adapter: cursorAdapter, toolName: "MCP:browser_click", serverName: "cursor-ide-browser", identity: "cursor-ide-browser"},
		{
			name:            "server the device copy lists is denied before the session inventory arrives",
			toolName:        linearTool,
			deviceInventory: []MCPServerEntry{{Source: "claude", Name: "linear", Command: "npx linear-mcp"}},
			wantDenied:      true,
		},
		{
			name:            "session inventory wins over the device copy",
			toolName:        linearTool,
			inventory:       []MCPServerEntry{},
			deviceInventory: []MCPServerEntry{{Source: "claude", Name: "linear", Command: "npx linear-mcp"}},
		},
		{name: "built-in missing from the device copy is allowed", toolName: "mcp__ccd_turn__wrap_up", deviceInventory: []MCPServerEntry{{Source: "claude", Name: "linear"}}},
		{
			name:       "codex server whose tool prefix holds a double underscore is denied",
			adapter:    "codex",
			toolName:   "mcp__foo__bar__list",
			serverName: "foo--bar",
			inventory:  []MCPServerEntry{{Source: "codex", Name: "foo--bar", ToolPrefix: "foo__bar", Command: "npx foo-bar-mcp"}},
			wantDenied: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.riskScanner = stubBlockingShadowMCPScanner{}

			payload := canonicalBuiltinPayload(tc, "tool.requested", uuid.NewString(), tc.command)
			seedCanonicalBuiltinInventories(t, ctx, ti, tc, payload)

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

// TestCanonicalTelemetryRows_RecordsBuiltinEvidence: the telemetry row of a
// built-in call records why it is one, and a row whose server the inventory
// resolves records that transport for the scanner instead.
func TestCanonicalTelemetryRows_RecordsBuiltinEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tc           canonicalBuiltinCase
		wantEvidence string
		wantMatch    string
	}{
		{tc: canonicalBuiltinCase{name: "known name", toolName: "mcp__claude-in-chrome__tabs_context_mcp", inventory: []MCPServerEntry{}}, wantEvidence: "known_name"},
		{tc: canonicalBuiltinCase{name: "unlisted name missing from the inventory", toolName: "mcp__ccd_new_tool__run", inventory: []MCPServerEntry{}}, wantEvidence: "not_in_inventory"},
		{tc: canonicalBuiltinCase{name: "unlisted name without an inventory", toolName: "mcp__ccd_new_tool__run"}, wantEvidence: "unverified"},
		{tc: canonicalBuiltinCase{name: "vendor bundle command", toolName: "mcp__computer_use__click", command: builtinTestVendorCommand}, wantEvidence: "vendor_bundle", wantMatch: builtinTestVendorCommand},
		{tc: canonicalBuiltinCase{name: "cursor built-in", adapter: cursorAdapter, toolName: "MCP:browser_click", serverName: "cursor-ide-browser", identity: "cursor-ide-browser"}, wantEvidence: "cursor_builtin"},
		{
			tc:        canonicalBuiltinCase{name: "inventory resolves a URL", toolName: "mcp__linear__list_issues", inventory: []MCPServerEntry{{Source: "claude", Name: "linear", URL: "https://mcp.example.test/mcp"}}},
			wantMatch: "https://mcp.example.test/mcp",
		},
		{tc: canonicalBuiltinCase{name: "installed without transport", toolName: "mcp__linear__list_issues", inventory: []MCPServerEntry{{Source: "claude", Name: "linear"}}}},
	}

	for _, tt := range tests {
		t.Run(tt.tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			payload := canonicalBuiltinPayload(tt.tc, "tool.requested", uuid.NewString(), tt.tc.command)
			seedCanonicalBuiltinInventories(t, ctx, ti, tt.tc, payload)

			rows := ti.service.canonicalTelemetryRows(ctx, payload, authCtx, &SessionMetadata{UserID: authCtx.UserID}, "claude", time.Now(), "")
			require.NotEmpty(t, rows)
			evidence, _ := rows[0].attrs[attr.MCPBuiltinEvidenceKey].(string)
			match, _ := rows[0].attrs[attr.MCPMatchKey].(string)
			require.Equal(t, tt.wantEvidence, evidence)
			require.Equal(t, tt.wantMatch, match)
		})
	}
}

// TestClaude_PreToolUse_ShadowMCPBlockAllClassifiesServers: built-in servers
// never appear in `claude mcp list`, so the PreToolUse guard allows any server
// the inventory does not configure, and no longer denies one just because no
// inventory has loaded yet. Only a connector, whose URL needs the inventory,
// still waits for it.
func TestClaude_PreToolUse_ShadowMCPBlockAllClassifiesServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		toolName        string
		inventory       []MCPServerEntry
		deviceInventory []MCPServerEntry
		want            string
	}{
		{
			name:      "known built-in missing from the inventory is allowed",
			toolName:  "mcp__claude-in-chrome__tabs_context_mcp",
			inventory: []MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp"}},
			want:      "allow",
		},
		{name: "unlisted server missing from the inventory is allowed", toolName: "mcp__linear__list_issues", inventory: []MCPServerEntry{}, want: "allow"},
		{name: "known built-in without any inventory is allowed", toolName: "mcp__ccd_session__start_session", want: "allow"},
		{name: "unlisted server without any inventory is allowed", toolName: "mcp__ccd_new_tool__run", want: "allow"},
		{
			name:      "known built-in name configured as stdio is denied",
			toolName:  "mcp__claude-in-chrome__tabs_context_mcp",
			inventory: []MCPServerEntry{{Source: "local", Name: "claude-in-chrome", Command: "npx some-mcp"}},
			want:      "deny",
		},
		{
			name:      "server configured with a vendor bundle command is allowed",
			toolName:  "mcp__computer_use__click",
			inventory: []MCPServerEntry{{Source: "local", Name: "computer_use", Command: builtinTestVendorCommand}},
			want:      "allow",
		},
		{name: "connector without any inventory is denied", toolName: "mcp__" + builtinTestConnector + "__list_issues", want: "deny"},
		{
			name:            "server the device copy configures is denied without a session inventory",
			toolName:        "mcp__linear__list_issues",
			deviceInventory: []MCPServerEntry{{Source: "local", Name: "linear", Command: "npx linear-mcp"}},
			want:            "deny",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.productFeatures = alwaysEnabledFeatures{}
			ti.service.riskScanner = stubBlockingShadowMCPScanner{}
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.Email)

			sessionID := uuid.NewString()
			userEmail := *authCtx.Email
			hostname := "dev-host"
			cwd := "/work/app"
			toolUseID := "toolu_builtin"
			payload := &gen.ClaudePayload{
				HookEventName: "PreToolUse",
				SessionID:     &sessionID,
				UserEmail:     &userEmail,
				HookHostname:  &hostname,
				Cwd:           &cwd,
				ToolName:      &tc.toolName,
				ToolUseID:     &toolUseID,
				ToolInput:     map[string]any{},
			}
			if tc.inventory != nil {
				require.NoError(t, ti.service.cache.Set(ctx,
					sessionMCPListCacheKey(testProjectID(t, ctx), sessionID), tc.inventory, sessionMCPListTTL))
			}
			if tc.deviceInventory != nil {
				metadata, err := ti.service.resolveClaudeSessionMetadata(ctx, sessionID, userEmail)
				require.NoError(t, err)
				require.NotEmpty(t, metadata.UserID)
				ti.service.cacheDeviceMCPList(ctx, claudeDeviceMCPListScope(metadata, payload), tc.deviceInventory)
			}

			result, err := ti.service.Claude(ctx, payload)
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

// TestCanonicalIngest_AgentBuiltinServerFindings: the findings scanner reads
// the classification ingest records, so it agrees with the guard. A built-in
// call yields no finding whether or not an inventory loaded, while a server
// the user installed, a stdio server borrowing a built-in name, and a call
// whose command rode on one event only are still reported.
func TestCanonicalIngest_AgentBuiltinServerFindings(t *testing.T) {
	t.Parallel()

	type scanEvent struct {
		eventType string
		command   string
	}
	requested := []scanEvent{{eventType: "tool.requested", command: ""}}
	tests := []struct {
		name        string
		toolName    string
		events      []scanEvent
		inventory   []MCPServerEntry
		wantFinding bool
	}{
		{name: "known built-in with an inventory yields no finding", toolName: "mcp__claude-in-chrome__tabs_context_mcp", events: requested, inventory: []MCPServerEntry{}},
		{name: "known built-in without an inventory yields no finding", toolName: "mcp__claude-in-chrome__tabs_context_mcp", events: requested},
		{name: "unlisted built-in without an inventory yields no finding", toolName: "mcp__ccd_new_tool__run", events: requested},
		{
			name:     "vendor bundle command yields no finding",
			toolName: "mcp__computer_use__click",
			events:   []scanEvent{{eventType: "tool.requested", command: builtinTestVendorCommand}},
		},
		{
			name:        "server listed without transport is reported",
			toolName:    "mcp__linear__list_issues",
			events:      requested,
			inventory:   []MCPServerEntry{{Source: "claude", Name: "linear"}},
			wantFinding: true,
		},
		{
			name:        "known built-in name with a stdio command is reported",
			toolName:    "mcp__claude-in-chrome__tabs_context_mcp",
			events:      []scanEvent{{eventType: "tool.requested", command: "npx some-mcp"}},
			inventory:   []MCPServerEntry{},
			wantFinding: true,
		},
		{
			name:     "known built-in name with a command on one event only is reported",
			toolName: "mcp__claude-in-chrome__tabs_context_mcp",
			events: []scanEvent{
				{eventType: "tool.requested", command: "/usr/bin/node server.js"},
				{eventType: "tool.completed", command: ""},
			},
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

			// Aggregate the call's rows the way the provenance query does: it
			// admits a row only when it carries a match, a server URL, or
			// built-in evidence, then keeps the max of each column.
			var prov telemetryrepo.MCPProvenance
			admitted := false
			for _, event := range tc.events {
				payload := canonicalBuiltinPayload(canonicalBuiltinCase{toolName: tc.toolName}, event.eventType, sessionID, event.command)
				rows := ti.service.canonicalTelemetryRows(ctx, payload, authCtx, &SessionMetadata{UserID: authCtx.UserID}, "claude", time.Now(), "")
				require.NotEmpty(t, rows)

				attrs := rows[0].attrs
				match, _ := attrs[attr.MCPMatchKey].(string)
				serverURL, _ := attrs[attr.MCPServerURLKey].(string)
				evidence, _ := attrs[attr.MCPBuiltinEvidenceKey].(string)
				source, _ := attrs[attr.ToolCallSourceKey].(string)
				if match == "" && serverURL == "" && evidence == "" {
					continue
				}
				admitted = true
				prov.Match = max(prov.Match, match)
				prov.ServerURL = max(prov.ServerURL, serverURL)
				prov.ServerIdentity = max(prov.ServerIdentity, source)
				prov.BuiltinEvidence = max(prov.BuiltinEvidence, evidence)
				prov.HookSource = "claude"
			}
			recorded := map[string]telemetryrepo.MCPProvenance{}
			if admitted {
				recorded["call-1"] = prov
			}

			scanner := shadowmcpscan.NewScanner(testenv.NewLogger(t), builtinScanValidator{}, builtinScanHosted{}, builtinScanProvenance{rows: recorded}, nil)
			out := scanner.Scan(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, uuid.Nil, []shadowmcpscan.Message{
				{UserID: "", ToolCalls: []shadowmcpscan.ToolCall{{ID: "call-1", Name: tc.toolName, Arguments: `{}`, CreatedAt: time.Now(), Sender: "claude"}}},
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

// TestInventoryServerFacts covers how an inventory entry is matched to the
// server a tool call names and what it proves.
func TestInventoryServerFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		entries     []MCPServerEntry
		serverName  string
		wantURL     string
		wantCommand string
		wantListed  bool
	}{
		{name: "exact name with URL", entries: []MCPServerEntry{{Name: "linear", URL: "https://mcp.example.test"}}, serverName: "linear", wantURL: "https://mcp.example.test", wantListed: true},
		{name: "claude.ai connector prefix", entries: []MCPServerEntry{{Source: "claude.ai", Name: "Linear"}}, serverName: "claude_ai_Linear", wantListed: true},
		{name: "plugin name with colons", entries: []MCPServerEntry{{Source: "claude", Name: "plugin:slack:slack", URL: "https://mcp.example.test"}}, serverName: "plugin_slack_slack", wantURL: "https://mcp.example.test", wantListed: true},
		{name: "connector UUID", entries: []MCPServerEntry{{Name: "Linear", ConnectorUUID: builtinTestConnector, Command: "npx x"}}, serverName: builtinTestConnector, wantCommand: "npx x", wantListed: true},
		{
			name:       "conflicting duplicates are listed without transport",
			entries:    []MCPServerEntry{{Name: "linear", URL: "https://a.example.test"}, {Name: "linear", URL: "https://b.example.test"}},
			serverName: "linear",
			wantListed: true,
		},
		{
			name:       "identical duplicates keep their transport",
			entries:    []MCPServerEntry{{Name: "linear", URL: "https://a.example.test"}, {Name: "linear", URL: "https://a.example.test"}},
			serverName: "linear",
			wantURL:    "https://a.example.test",
			wantListed: true,
		},
		{name: "absent", entries: []MCPServerEntry{{Name: "mise", Command: "mise mcp"}}, serverName: "linear"},
		{name: "empty name", entries: []MCPServerEntry{{Name: ""}}, serverName: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			url, command, listed := inventoryServerFacts(tc.entries, tc.serverName)
			require.Equal(t, tc.wantURL, url)
			require.Equal(t, tc.wantCommand, command)
			require.Equal(t, tc.wantListed, listed)
		})
	}
}

// TestInventoryShadowMCPFacts_UnverifiedNeverForKnownNames: a listed name keeps
// its known_name label without an inventory, so the unverified label only
// ever marks names nobody has listed.
func TestInventoryShadowMCPFacts_UnverifiedNeverForKnownNames(t *testing.T) {
	t.Parallel()

	missing := shadowMCPInventory{entries: nil, loaded: false}
	require.Equal(t, shadowmcp.BuiltinEvidenceKnownName, shadowmcp.ClassifyServer(inventoryShadowMCPFacts("claude-in-chrome", missing)).Evidence)
	require.Equal(t, shadowmcp.BuiltinEvidenceUnverified, shadowmcp.ClassifyServer(inventoryShadowMCPFacts("ccd_new_tool", missing)).Evidence)
}
