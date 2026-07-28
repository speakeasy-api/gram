package hooks

import (
	"testing"
	"time"

	"github.com/google/uuid"
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

	// Staging is scanned per project and the fixture project is shared across
	// test databases, so the ids must be unique to this run.
	sessionID := "claude-staging-session-" + uuid.NewString()
	stagedRequestID := "req_staged_" + uuid.NewString()
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
				strAttr("request_id", stagedRequestID),
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
		require.NotContains(t, log.Attributes, stagedRequestID, "redacted api_request must not write through")
	}

	// The staging listing is project-wide (and the fixture project persists
	// across runs), so pick out this run's row by its unique request id.
	var stagedRow *telemetryrepo.StagedTelemetryLogRow
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		staged, err := chClient.ListStagedTelemetryLogs(ctx, authCtx.ProjectID.String())
		assert.NoError(collect, err)
		for i := range staged {
			if staged[i].RequestID == stagedRequestID {
				stagedRow = &staged[i]
			}
		}
		assert.NotNil(collect, stagedRow)
	}, 3*time.Second, 50*time.Millisecond)

	require.Contains(t, stagedRow.Attributes, `"custom"`)
	require.NotNil(t, stagedRow.GramChatID)
	require.Equal(t, sessionID, *stagedRow.GramChatID)
	// The staged row must carry the org id (materialized from the stamped
	// gram.org.id attribute) — it is the attribution tuple's join scope.
	require.Equal(t, authCtx.ActiveOrganizationID, stagedRow.OrgID)
}

// TestIngest_CapturesMCPAttributionTuples verifies that a unified ingest
// payload carrying data.mcp_attribution (the Claude Stop/SubagentStop shape)
// stores one Redis tuple per request id for the promotion worker to join on.
// Tuples must be keyed by the authenticated org, not the project: the plugin's
// hooks key resolves a project from the client-sent project slug, which need
// not match the OTEL exporter's project on the staged row.
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
	authCtx := hookAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID

	var tuple telemetry.MCPAttributionTuple
	require.NoError(t, cacheAdapter.Get(ctx, telemetry.MCPAttributionTupleKey(orgID, "req_attr_1"), &tuple))
	require.Equal(t, telemetry.MCPAttributionTuple{Server: "workos-public", Tool: "whoami"}, tuple)

	require.NoError(t, cacheAdapter.Get(ctx, telemetry.MCPAttributionTupleKey(orgID, "req_attr_2"), &tuple))
	require.Equal(t, telemetry.MCPAttributionTuple{Server: "linear-public", Tool: ""}, tuple)

	require.Error(t, cacheAdapter.Get(ctx, telemetry.MCPAttributionTupleKey(orgID, "req_attr_3"), &tuple))

	// The old project-scoped key must not be written anymore — a promotion
	// pass reading by org would miss it.
	require.Error(t, cacheAdapter.Get(ctx, telemetry.MCPAttributionTupleKey(authCtx.ProjectID.String(), "req_attr_1"), &tuple))
}
