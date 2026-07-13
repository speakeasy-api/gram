package hooks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// TestLogs_StagesRedactedClaudeAPIRequests exercises the ingest fork: a
// Claude api_request row stamped mcp_server.name='custom' must land in
// telemetry_logs_staging, while verbatim-labeled api_requests and tool events
// write through to telemetry_logs as before.
func TestLogs_StagesRedactedClaudeAPIRequests(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := "claude-staging-session-1"
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		// Redacted api_request: must be staged, not written through.
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp)),
			Body:         &gen.OTELLogBody{StringValue: new("claude_code.api_request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("event.name", "api_request"),
				strAttr("request_id", "req_staged_1"),
				strAttr("mcp_server.name", "custom"),
				strAttr("mcp_tool.name", "custom"),
				strAttr("prompt.id", "prompt-1"),
			},
		},
		// Verbatim api_request: writes through.
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp.Add(time.Second))),
			Body:         &gen.OTELLogBody{StringValue: new("claude_code.api_request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("event.name", "api_request"),
				strAttr("request_id", "req_verbatim_1"),
				strAttr("mcp_server.name", "linear-public"),
				strAttr("prompt.id", "prompt-1"),
			},
		},
		// Tool event: writes through even though it mentions no MCP names.
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp.Add(2 * time.Second))),
			Body:         &gen.OTELLogBody{StringValue: new("claude_code.tool_result")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("event.name", "tool_result"),
				strAttr("prompt.id", "prompt-1"),
			},
		},
	))
	require.NoError(t, err)

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), claudeOTELLogsURN, timestamp, 2)
	for _, log := range logs {
		require.NotContains(t, log.Attributes, "req_staged_1", "redacted api_request must not write through")
	}

	var staged []telemetryrepo.StagedTelemetryLogRow
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var err error
		staged, err = chClient.ListStagedTelemetryLogs(ctx, authCtx.ProjectID.String(), sessionID)
		assert.NoError(collect, err)
		assert.Len(collect, staged, 1)
	}, 3*time.Second, 50*time.Millisecond)

	require.Equal(t, "req_staged_1", staged[0].RequestID)
	require.Contains(t, staged[0].Attributes, `"custom"`)
	require.NotNil(t, staged[0].GramChatID)
	require.Equal(t, sessionID, *staged[0].GramChatID)
}

// TestIngest_CapturesMCPAttributionTuples verifies that a unified ingest
// payload carrying data.mcp_attribution (the Claude Stop/SubagentStop shape)
// stores one Redis tuple per request id for the promotion worker to join on.
func TestIngest_CapturesMCPAttributionTuples(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)

	sessionID := "claude-attr-session-1"
	result, err := ti.service.Ingest(ctx, &gen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SchemaVersion:    "hook.ingest.v1",
		IdempotencyKey:   nil,
		Source: &gen.HookIngestSource{
			Adapter:        "claude",
			AdapterVersion: nil,
			RawEventName:   new("SubagentStop"),
			Hostname:       nil,
		},
		Session: &gen.HookIngestSession{
			ID:     new(sessionID),
			TurnID: nil,
			Cwd:    nil,
			Model:  nil,
		},
		Event: &gen.HookIngestEvent{
			Type:       "usage.reported",
			OccurredAt: nil,
		},
		Data: &gen.HookIngestData{
			Prompt:       nil,
			ToolCall:     nil,
			Mcp:          nil,
			Usage:        nil,
			Message:      nil,
			Skill:        nil,
			Notification: nil,
			McpAttribution: []*gen.HookMCPAttributionEntry{
				{
					RequestID: "req_attr_1",
					McpServer: new("workos-public"),
					McpTool:   new("whoami"),
				},
				{
					RequestID: "req_attr_2",
					McpServer: new("linear-public"),
					McpTool:   nil,
				},
				// No server name: nothing to restore, must be skipped.
				{
					RequestID: "req_attr_3",
					McpServer: nil,
					McpTool:   new("whoami"),
				},
			},
		},
		Raw: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	cacheAdapter := cache.NewRedisCacheAdapter(ti.redisClient)

	var tuple telemetry.MCPAttributionTuple
	require.NoError(t, cacheAdapter.Get(ctx, telemetry.MCPAttributionTupleKey("req_attr_1"), &tuple))
	require.Equal(t, telemetry.MCPAttributionTuple{Server: "workos-public", Tool: "whoami"}, tuple)

	require.NoError(t, cacheAdapter.Get(ctx, telemetry.MCPAttributionTupleKey("req_attr_2"), &tuple))
	require.Equal(t, telemetry.MCPAttributionTuple{Server: "linear-public", Tool: ""}, tuple)

	require.Error(t, cacheAdapter.Get(ctx, telemetry.MCPAttributionTupleKey("req_attr_3"), &tuple))
}
