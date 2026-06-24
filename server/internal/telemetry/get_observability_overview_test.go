package telemetry_test

import (
	"context"
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetObservabilityOverview_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		IncludeTimeSeries: true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Summary)
	require.NotNil(t, result.Comparison)

	// All metrics should be zero for empty data
	require.Equal(t, int64(0), result.Summary.TotalChats)
	require.Equal(t, int64(0), result.Summary.ResolvedChats)
	require.Equal(t, int64(0), result.Summary.FailedChats)
	require.Equal(t, int64(0), result.Summary.TotalToolCalls)
}

func TestGetObservabilityOverview_WithChatResolutions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()
	chatID3 := uuid.New().String()

	// Insert resolution events for chats
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, "success", 85)
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID2, "failure", 25)
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), chatID3, "partial", 60)

	// Insert some tool calls
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), "tools:http:petstore:listPets", 200, 0.5)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), "tools:http:petstore:getPet", 500, 1.0)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
			From:              from,
			To:                to,
			IncludeTimeSeries: true,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}

		// Verify chat resolution metrics
		assert.Equal(c, int64(3), res.Summary.TotalChats)
		assert.Equal(c, int64(1), res.Summary.ResolvedChats)
		assert.Equal(c, int64(1), res.Summary.FailedChats)

		// Verify tool metrics
		assert.Equal(c, int64(2), res.Summary.TotalToolCalls)
		assert.Equal(c, int64(1), res.Summary.FailedToolCalls)

		// Verify time series is returned
		assert.NotEmpty(c, res.TimeSeries)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestGetObservabilityOverview_TimeSeriesMetrics(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	// Insert data at different times to verify time bucketing
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()

	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-50*time.Minute), chatID1, "success", 90)
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID2, "success", 85)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
			From:              from,
			To:                to,
			IncludeTimeSeries: true,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}

		if !assert.NotEmpty(c, res.TimeSeries) {
			return
		}

		// With 2-hour range and auto-calculated 15-min buckets, we should have 8+ buckets
		assert.GreaterOrEqual(c, len(res.TimeSeries), 8)

		// Verify the first bucket has a valid timestamp
		assert.Positive(c, res.TimeSeries[0].BucketTimeUnixNano)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestGetObservabilityOverview_UnevaluatedChats(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()
	chatID3 := uuid.New().String()

	// Insert chat completion logs without any evaluation label (the common case for unresolved chats)
	insertChatCompletionLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, 100, 50, 150, 30.0, "stop", "gpt-4", "openai")
	insertChatCompletionLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID2, 200, 100, 300, 45.0, "stop", "gpt-4", "openai")

	// Insert one evaluated chat for comparison
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), chatID3, "success", 90)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
			From:              from,
			To:                to,
			IncludeTimeSeries: true,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}

		// All 3 chats must be counted — unevaluated chats should not be excluded
		assert.Equal(c, int64(3), res.Summary.TotalChats)

		// Only the one evaluated-as-success chat counts toward resolved
		assert.Equal(c, int64(1), res.Summary.ResolvedChats)
		assert.Equal(c, int64(0), res.Summary.FailedChats)

		// Session duration is computed from chat completion events regardless of evaluation label
		assert.Greater(c, res.Summary.AvgSessionDurationMs, float64(0))

		// Time series must also reflect all 3 chats in total
		totalInTimeSeries := int64(0)
		for _, b := range res.TimeSeries {
			totalInTimeSeries += b.TotalChats
		}
		assert.Equal(c, int64(3), totalInTimeSeries)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestGetObservabilityOverview_FromAfterTo(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(1 * time.Hour).Format(time.RFC3339) // after
	to := now.Add(-1 * time.Hour).Format(time.RFC3339)  // before

	_, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		IncludeTimeSeries: true,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "'from' time must be before 'to' time")
}

func TestGetObservabilityOverview_RemoteMCPServerIDFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	serverA := uuid.New().String()
	serverB := uuid.New().String()

	insertRemoteMCPToolCallLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:externalmcp:"+serverA+":listIssues", serverA, 200, 0.4)
	insertRemoteMCPToolCallLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), "tools:externalmcp:"+serverA+":createIssue", serverA, 500, 1.2)
	insertRemoteMCPToolCallLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), "tools:externalmcp:"+serverB+":listIssues", serverB, 200, 0.3)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Without filter: all three tool calls show up.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
			From:              from,
			To:                to,
			IncludeTimeSeries: false,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(3), res.Summary.TotalToolCalls)
	}, 10*time.Second, 200*time.Millisecond)

	// Filter by server A: only its two calls show up.
	scoped, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		RemoteMcpServerID: &serverA,
		IncludeTimeSeries: false,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), scoped.Summary.TotalToolCalls)
	require.Equal(t, int64(1), scoped.Summary.FailedToolCalls)
	require.Len(t, scoped.TopToolsByCount, 2)
}

func TestGetObservabilityOverview_MCPServerIDFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	serverA := uuid.New().String()
	serverB := uuid.New().String()

	// Server A spans both backings under one fronting mcp_server_id: a
	// remote-backed tool call (also carries remote_mcp_server.id) and a
	// toolset-backed tool call (also carries toolset.slug). Server B is a
	// separate fronting server. Filtering by mcp_server_id must capture both
	// of server A's calls regardless of how each was backed.
	insertMCPServerToolCallLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:externalmcp:"+serverA+":listIssues", serverA, map[string]any{"gram.remote_mcp_server.id": uuid.New().String()}, 200, 0.4)
	insertMCPServerToolCallLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), "tools:petstore:createPet", serverA, map[string]any{"gram.toolset.slug": "petstore"}, 500, 1.2)
	insertMCPServerToolCallLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), "tools:externalmcp:"+serverB+":listIssues", serverB, nil, 200, 0.3)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Without filter: all three tool calls show up.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
			From:              from,
			To:                to,
			IncludeTimeSeries: false,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(3), res.Summary.TotalToolCalls)
	}, 10*time.Second, 200*time.Millisecond)

	// Filter by server A: both its calls show up regardless of backing.
	scoped, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		McpServerID:       &serverA,
		IncludeTimeSeries: false,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), scoped.Summary.TotalToolCalls)
	require.Equal(t, int64(1), scoped.Summary.FailedToolCalls)
	require.Len(t, scoped.TopToolsByCount, 2)
}

func TestGetObservabilityOverview_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		IncludeTimeSeries: true,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "logs are not enabled")
}

// insertRemoteMCPToolCallLog inserts a tool call log that carries a
// gram.remote_mcp_server.id attribute so the materialized
// remote_mcp_server_id column is populated.
func insertRemoteMCPToolCallLog(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, toolURN, remoteMCPServerID string, statusCode int32, durationSec float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.tool.urn":                toolURN,
		"gram.remote_mcp_server.id":    remoteMCPServerID,
		"http.server.request.duration": durationSec,
		"http.response.status_code":    statusCode,
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "tool call",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, toolURN, "gram-tools")
	require.NoError(t, err)
}

// insertMCPServerToolCallLog inserts a tool call log carrying a
// gram.mcp_server.id attribute (the fronting MCP server id) so the
// materialized mcp_server_id column is populated. extraAttrs lets a test also
// stamp backing-specific attributes (e.g. gram.remote_mcp_server.id or
// gram.toolset.slug) to prove one mcp_server_id spans both backings.
func insertMCPServerToolCallLog(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, toolURN, mcpServerID string, extraAttrs map[string]any, statusCode int32, durationSec float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.tool.urn":                toolURN,
		"gram.mcp_server.id":           mcpServerID,
		"http.server.request.duration": durationSec,
		"http.response.status_code":    statusCode,
	}
	maps.Copy(attributes, extraAttrs)

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "tool call",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, toolURN, "gram-tools")
	require.NoError(t, err)
}

// insertResolutionLog inserts a chat resolution event log
func insertResolutionLog(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID string, resolution string, score int) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":        chatID,
		"gen_ai.conversation.duration":  30.0, // 30 seconds
		"gram.resource.urn":             "agents:chat:resolution",
		"evaluation.score":              score,
		"gen_ai.evaluation.score.label": resolution, // This feeds the MATERIALIZED column
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, gram_chat_id,
			service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat resolution",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "agents:chat:resolution", chatID,
		"gram-agents")
	require.NoError(t, err)
}
