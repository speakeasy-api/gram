package hooks

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// tool.requested carries no data.mcp, so without the session inventory the
// guard sees an empty URL — which can never be Gram-hosted — and denies a
// server the inventory proves is hosted.
func TestIngest_ShadowMCPGuardResolvesGramHostedServerFromSessionInventory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := "canonical-inventory-hosted-" + uuid.NewString()
	// What the relay sends: it strips "claude.ai " before shipping.
	serverName := "Platform - Search"
	serverURL := "https://app.getgram.ai/mcp/platform-search"

	inventory := canonicalIngestPayload("claude", "session.started", sessionID)
	inventory.Data = &gen.HookIngestData{
		McpInventory: []*gen.HookMCPData{{
			ServerName:     &serverName,
			ServerIdentity: &serverName,
			URL:            &serverURL,
		}},
	}
	inventoryResult, err := ti.service.Ingest(ctx, inventory)
	require.NoError(t, err)
	require.Equal(t, "allow", inventoryResult.Decision)

	toolName := "mcp__claude_ai_Platform_-_Search__search--query"
	toolCallID := "call-inventory-hosted"
	call := canonicalIngestPayload("claude", "tool.requested", sessionID)
	call.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"pullNumber": 1},
		},
	}

	result, err := ti.service.Ingest(ctx, call)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision,
		"the session inventory resolves this server to a Gram-hosted URL, so the guard must not treat it as shadow MCP")
}

// Same gap, quieter effect: a row with no gram.mcp.server_url classifies as
// shadow MCP no matter where the call went.
func TestIngest_MCPToolCallStampsProvenanceFromSessionInventory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := "canonical-inventory-provenance-" + uuid.NewString()
	// What the relay sends: it strips "claude.ai " before shipping.
	serverName := "Platform - Search"
	serverURL := "https://app.getgram.ai/mcp/platform-search"

	inventory := canonicalIngestPayload("claude", "session.started", sessionID)
	inventory.Data = &gen.HookIngestData{
		McpInventory: []*gen.HookMCPData{{
			ServerName:     &serverName,
			ServerIdentity: &serverName,
			URL:            &serverURL,
		}},
	}
	_, err := ti.service.Ingest(ctx, inventory)
	require.NoError(t, err)

	timestamp := time.Now().UTC()
	toolName := "mcp__claude_ai_Platform_-_Search__search--query"
	toolCallID := "call-inventory-provenance"
	rawEventName := "PreToolUse"
	call := canonicalIngestPayload("claude", "tool.requested", sessionID)
	call.Source.RawEventName = &rawEventName
	call.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"pullNumber": 1},
		},
	}

	_, err = ti.service.Ingest(ctx, call)
	require.NoError(t, err)

	// Hook rows have an empty gram.tool.urn, so GramURNs can't select them.
	var logs []telemetryrepo.TelemetryLog
	require.Eventually(t, func() bool {
		logs, err = chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) == 2
	}, 5*time.Second, 50*time.Millisecond, "expected the SessionStart and PreToolUse rows to land in telemetry")

	var toolRow telemetryrepo.TelemetryLog
	for _, row := range logs {
		if strings.Contains(row.Attributes, toolName) {
			toolRow = row
		}
	}
	require.NotEmpty(t, toolRow.Attributes, "the MCP tool call must produce a hook row")

	// ClickHouse escapes forward slashes in the stored JSON.
	attributes := strings.ReplaceAll(toolRow.Attributes, `\/`, "/")
	require.Contains(t, attributes, serverURL,
		"the row must carry gram.mcp.server_url so the offline scanner resolves the server instead of guessing")
	require.Contains(t, attributes, serverName,
		"the row must carry the inventory's display name as gram.tool_call.source")
}

// Only mcp__ names carry a server segment. Resolving Cursor's "MCP:<tool>" or
// OpenCode's bare names against the inventory would attach an unrelated server.
func TestIngest_NonClaudeToolNameDialectsDoNotResolveAgainstServerInventory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)

	sessionID := "canonical-inventory-dialects-" + uuid.NewString()
	// Names collide with the tool names below: a match would be misattribution.
	sendMessage := "send_message"
	github := "github"
	serverURL := "https://mcp.example.com/mcp"

	inventory := canonicalIngestPayload("cursor", "session.started", sessionID)
	inventory.Data = &gen.HookIngestData{
		McpInventory: []*gen.HookMCPData{
			{ServerName: &sendMessage, URL: &serverURL},
			{ServerName: &github, URL: &serverURL},
		},
	}
	_, err := ti.service.Ingest(ctx, inventory)
	require.NoError(t, err)

	require.Nil(t, ti.service.canonicalMCPEntry(ctx, sessionID, "MCP:send_message"),
		"a Cursor MCP: name must not resolve against the server inventory")
	require.Nil(t, ti.service.canonicalMCPEntry(ctx, sessionID, "github_pull_request_read"),
		"a bare OpenCode tool name must not resolve against the server inventory")
}
