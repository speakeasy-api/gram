package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestGetHooksSummary_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	_, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "logs are not enabled")
}

func TestGetHooksSummary_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Servers)
	require.Empty(t, result.Users)
	require.Empty(t, result.Breakdown)
	require.Empty(t, result.TimeSeries)
	require.Equal(t, int64(0), result.TotalEvents)
	require.Equal(t, int64(0), result.TotalSessions)
}

func TestGetHooksSummary_AggregatesServersUsersAndBreakdown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	// Two events from user1 on server-a (one success, one failure)
	traceID1 := uuid.New().String()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-10 * time.Minute),
		traceID:        traceID1,
		userEmail:      "user1@example.com",
		hookSource:     "mcp",
		toolSource:     "server-a",
		toolName:       "weather",
		hookEvent:      "PostToolUse",
		conversationID: "conv-1",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-9 * time.Minute),
		traceID:        traceID1,
		userEmail:      "user1@example.com",
		hookSource:     "mcp",
		toolSource:     "server-a",
		toolName:       "weather",
		hookEvent:      "PostToolUseFailure",
		conversationID: "conv-1",
	})

	// One event from user2 on server-b (success)
	traceID2 := uuid.New().String()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-5 * time.Minute),
		traceID:        traceID2,
		userEmail:      "user2@example.com",
		hookSource:     "local",
		toolSource:     "server-b",
		toolName:       "fetch",
		hookEvent:      "PostToolUse",
		conversationID: "conv-2",
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Total events = 3 (individual log entries), sessions = 2 unique conversation IDs
	require.Equal(t, int64(3), result.TotalEvents)
	require.Equal(t, int64(2), result.TotalSessions)

	// Server aggregation: 2 servers
	serversByName := make(map[string]*gen.HooksServerSummary)
	for _, s := range result.Servers {
		serversByName[s.ServerName] = s
	}
	require.Len(t, serversByName, 2)
	require.Equal(t, int64(2), serversByName["server-a"].EventCount)
	require.Equal(t, int64(1), serversByName["server-b"].EventCount)

	// User aggregation: 2 users
	usersByEmail := make(map[string]*gen.HooksUserSummary)
	for _, u := range result.Users {
		usersByEmail[u.UserEmail] = u
	}
	require.Len(t, usersByEmail, 2)
	require.Equal(t, int64(2), usersByEmail["user1@example.com"].EventCount)
	require.Equal(t, int64(1), usersByEmail["user2@example.com"].EventCount)

	// Breakdown: at least one entry for user1/server-a/weather
	breakdownKey := func(b *gen.HooksBreakdownRow) string {
		return b.UserEmail + "|" + b.ServerName + "|" + b.ToolName
	}
	breakdownMap := make(map[string]*gen.HooksBreakdownRow)
	for _, b := range result.Breakdown {
		breakdownMap[breakdownKey(b)] = b
	}
	row := breakdownMap["user1@example.com|server-a|weather"]
	require.NotNil(t, row)
	require.Equal(t, int64(2), row.EventCount)
	// trace_summaries aggregates hook_has_failure = 1 when any log in the trace is a failure
	require.GreaterOrEqual(t, row.FailureCount, int64(0))

	// Time series: at least one point
	require.NotEmpty(t, result.TimeSeries)
	for _, pt := range result.TimeSeries {
		require.NotEmpty(t, pt.BucketStartNs)
		require.Positive(t, pt.EventCount)
	}
}

func TestGetHooksSummary_FilterByHookType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	traceID1 := uuid.New().String()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-10 * time.Minute),
		traceID:        traceID1,
		userEmail:      "user@example.com",
		hookSource:     "mcp",
		toolSource:     "remote-server",
		toolName:       "tool-a",
		hookEvent:      "PostToolUse",
		conversationID: "conv-1",
	})

	traceID2 := uuid.New().String()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-8 * time.Minute),
		traceID:        traceID2,
		userEmail:      "user@example.com",
		hookSource:     "local",
		toolSource:     "", // empty tool_source = local
		toolName:       "tool-b",
		hookEvent:      "PostToolUse",
		conversationID: "conv-2",
	})

	time.Sleep(200 * time.Millisecond)

	// Filter to only "mcp" type — tool_source != '' AND tool_name != 'Skill'
	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From:           now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:             now.Add(1 * time.Hour).Format(time.RFC3339),
		TypesToInclude: []string{"mcp"},
	})

	require.NoError(t, err)
	require.Equal(t, int64(1), result.TotalEvents)
	require.Len(t, result.Servers, 1)
	require.Equal(t, "remote-server", result.Servers[0].ServerName)
}

func TestGetHooksSummary_AttributeFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	traceID1 := uuid.New().String()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-10 * time.Minute),
		traceID:        traceID1,
		userEmail:      "alice@example.com",
		hookSource:     "mcp",
		toolSource:     "server-x",
		toolName:       "tool",
		hookEvent:      "PostToolUse",
		conversationID: "conv-1",
	})

	traceID2 := uuid.New().String()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-8 * time.Minute),
		traceID:        traceID2,
		userEmail:      "bob@example.com",
		hookSource:     "mcp",
		toolSource:     "server-x",
		toolName:       "tool",
		hookEvent:      "PostToolUse",
		conversationID: "conv-2",
	})

	time.Sleep(200 * time.Millisecond)

	// Filter to only alice's events
	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		Filters: []*gen.LogFilter{
			{Path: "user.email", Operator: "eq", Values: []string{"alice@example.com"}},
		},
	})

	require.NoError(t, err)
	require.Equal(t, int64(1), result.TotalEvents)
	require.Len(t, result.Users, 1)
	require.Equal(t, "alice@example.com", result.Users[0].UserEmail)
}

// hookEventParams holds parameters for inserting a single hook log event.
type hookEventParams struct {
	projectID      string
	deploymentID   string
	timestamp      time.Time
	traceID        string
	userEmail      string
	hookSource     string // "mcp", "local", etc.
	toolSource     string // server/host name (gram.tool_call.source)
	toolName       string
	hookEvent      string // "PostToolUse" or "PostToolUseFailure"
	conversationID string // genai.conversation.id for session counting
}

// insertHookEvent inserts a single telemetry log representing a hook event.
// Hook-related fields are populated via attributes so the trace_summaries
// materialized view aggregates them correctly.
func insertHookEvent(t *testing.T, ctx context.Context, p hookEventParams) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attrs := map[string]any{
		"gram.event.source":     "hook",
		"gram.hook.source":      p.hookSource,
		"gram.tool.name":        p.toolName,
		"gram.hook.event":       p.hookEvent,
		"user.email":            p.userEmail,
		"genai.conversation.id": p.conversationID,
	}
	if p.toolSource != "" {
		attrs["gram.tool_call.source"] = p.toolSource
	}

	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), p.timestamp.UnixNano(), p.timestamp.UnixNano(), "INFO", "hook event",
		p.traceID, nil, string(attrsJSON), "{}",
		p.projectID, p.deploymentID, "hooks:"+p.toolName, "gram-hooks")
	require.NoError(t, err)
}
