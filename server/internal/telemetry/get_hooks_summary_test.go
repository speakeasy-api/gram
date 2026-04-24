package telemetry_test

import (
	"context"
	"encoding/json"
	"strings"
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

	// Two events from user1 on server-a (one success, one failure) — each in its own trace
	// so trace_summaries produces 2 rows for server-a (count(*) is trace-level, not log-level).
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
		result:         `{"temp":72}`,
		conversationID: "conv-1",
	})
	traceID1b := uuid.New().String()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-9 * time.Minute),
		traceID:        traceID1b,
		userEmail:      "user1@example.com",
		hookSource:     "mcp",
		toolSource:     "server-a",
		toolName:       "weather",
		errorMsg:       "upstream timeout",
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
		result:         `"ok"`,
		conversationID: "conv-2",
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Total events = 3 traces (trace_summaries counts at trace level, not log level), sessions = 2 unique conversation IDs
	require.Equal(t, int64(3), result.TotalEvents)
	require.Equal(t, int64(2), result.TotalSessions)

	// Server aggregation: 2 servers
	serversByName := make(map[string]*gen.HooksServerSummary)
	for _, s := range result.Servers {
		serversByName[s.ServerName] = s
	}
	require.Len(t, serversByName, 2)
	require.Equal(t, int64(2), serversByName["server-a"].EventCount)
	require.Equal(t, int64(1), serversByName["server-a"].SuccessCount)
	require.Equal(t, int64(1), serversByName["server-a"].FailureCount)
	require.Equal(t, int64(1), serversByName["server-b"].EventCount)
	require.Equal(t, int64(1), serversByName["server-b"].SuccessCount)
	require.Equal(t, int64(0), serversByName["server-b"].FailureCount)

	// User aggregation: 2 users
	usersByEmail := make(map[string]*gen.HooksUserSummary)
	for _, u := range result.Users {
		usersByEmail[u.UserEmail] = u
	}
	require.Len(t, usersByEmail, 2)
	require.Equal(t, int64(2), usersByEmail["user1@example.com"].EventCount)
	require.Equal(t, int64(1), usersByEmail["user1@example.com"].SuccessCount)
	require.Equal(t, int64(1), usersByEmail["user1@example.com"].FailureCount)
	require.Equal(t, int64(1), usersByEmail["user2@example.com"].EventCount)
	require.Equal(t, int64(1), usersByEmail["user2@example.com"].SuccessCount)
	require.Equal(t, int64(0), usersByEmail["user2@example.com"].FailureCount)

	// Breakdown: one entry for user1/server-a/weather covering both traces.
	// trace_summaries aggregates has_error=1 for the failure trace via gram.hook.error.
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
	require.Equal(t, int64(1), row.FailureCount)

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
		result:         `"ok"`,
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
		result:         `"ok"`,
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
		result:         `"ok"`,
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
		result:         `"ok"`,
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
	result         string // gen_ai.tool.call.result; non-empty marks the trace as has_result=1
	errorMsg       string // gram.hook.error; non-empty marks the trace as has_error=1
	skillName      string // non-empty when toolName = "Skill"
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
		"user.email":            p.userEmail,
		"genai.conversation.id": p.conversationID,
	}
	if p.toolSource != "" {
		attrs["gram.tool_call.source"] = p.toolSource
	}
	if p.result != "" {
		attrs["gen_ai.tool.call.result"] = p.result
	}
	if p.errorMsg != "" {
		attrs["gram.hook.error"] = p.errorMsg
	}
	if p.skillName != "" {
		// gen_ai.tool.call.arguments is stored as a JSON-encoded string in OTel attributes,
		// matching what JSONExtractString(toString(attributes.gen_ai.tool.call.arguments), 'skill') expects.
		skillArgs, marshalErr := json.Marshal(map[string]any{"skill": p.skillName})
		require.NoError(t, marshalErr)
		attrs["gen_ai.tool.call.arguments"] = string(skillArgs)
	}

	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	// trace_id is FixedString(32) — strip hyphens from UUID to get 32 hex chars
	traceID := strings.ReplaceAll(p.traceID, "-", "")

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), p.timestamp.UnixNano(), p.timestamp.UnixNano(), "INFO", "hook event",
		traceID, nil, string(attrsJSON), "{}",
		p.projectID, p.deploymentID, "hooks:"+p.toolName, "gram-hooks")
	require.NoError(t, err)
}

func TestGetHooksSummary_SkillTimeSeriesGroupsBySkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	// Two events for "golang" skill, one for "typescript" skill.
	// All share conv-1 intentionally — testing event counts, not session counts.
	for i, skillName := range []string{"golang", "golang", "typescript"} {
		insertHookEvent(t, ctx, hookEventParams{
			projectID:      projectID,
			deploymentID:   deploymentID,
			timestamp:      now.Add(-time.Duration(10+i) * time.Minute),
			traceID:        uuid.New().String(),
			userEmail:      "user@example.com",
			hookSource:     "local",
			toolSource:     "",
			toolName:       "Skill",
			skillName:      skillName,
			conversationID: "conv-1",
		})
	}

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.NotEmpty(t, result.SkillTimeSeries)

	countBySkill := make(map[string]int64)
	for _, pt := range result.SkillTimeSeries {
		require.NotEmpty(t, pt.BucketStartNs)
		countBySkill[pt.SkillName] += pt.EventCount
	}
	require.Equal(t, int64(2), countBySkill["golang"])
	require.Equal(t, int64(1), countBySkill["typescript"])
}

func TestGetHooksSummary_SkillTimeSeriesEmptyWhenNoSkillEvents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-5 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "user@example.com",
		hookSource:     "mcp",
		toolSource:     "server-a",
		toolName:       "fetch",
		conversationID: "conv-1",
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.Empty(t, result.SkillTimeSeries)
}

func TestGetHooksSummary_SkillTimeSeriesExcludesNonSkillEvents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	// One skill event and one non-skill event
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-10 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "user@example.com",
		hookSource:     "local",
		toolSource:     "",
		toolName:       "Skill",
		skillName:      "golang",
		conversationID: "conv-1",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-8 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "user@example.com",
		hookSource:     "mcp",
		toolSource:     "server-a",
		toolName:       "fetch",
		conversationID: "conv-2",
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.Equal(t, int64(2), result.TotalEvents)
	require.Len(t, result.SkillTimeSeries, 1)
	require.Equal(t, "golang", result.SkillTimeSeries[0].SkillName)
	require.Equal(t, int64(1), result.SkillTimeSeries[0].EventCount)
}

func TestGetHooksSummary_SkillBreakdownGroupsBySkillAndUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	// user1 uses "golang" twice, user2 uses "golang" once, user2 uses "typescript" once.
	for i, params := range []struct {
		user, skill string
	}{
		{"user1@example.com", "golang"},
		{"user1@example.com", "golang"},
		{"user2@example.com", "golang"},
		{"user2@example.com", "typescript"},
	} {
		insertHookEvent(t, ctx, hookEventParams{
			projectID:      projectID,
			deploymentID:   deploymentID,
			timestamp:      now.Add(-time.Duration(10+i) * time.Minute),
			traceID:        uuid.New().String(),
			userEmail:      params.user,
			hookSource:     "local",
			toolSource:     "",
			toolName:       "Skill",
			skillName:      params.skill,
			conversationID: "conv-1",
		})
	}

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err)

	type key struct{ skill, user string }
	bySkillUser := make(map[key]int64)
	for _, row := range result.SkillBreakdown {
		bySkillUser[key{row.SkillName, row.UserEmail}] += row.UseCount
	}

	require.Equal(t, int64(2), bySkillUser[key{"golang", "user1@example.com"}])
	require.Equal(t, int64(1), bySkillUser[key{"golang", "user2@example.com"}])
	require.Equal(t, int64(1), bySkillUser[key{"typescript", "user2@example.com"}])
	require.NotContains(t, bySkillUser, key{"typescript", "user1@example.com"})
}

func TestGetHooksSummary_SkillBreakdownEmptyWhenNoSkillEvents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-5 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "user@example.com",
		hookSource:     "mcp",
		toolSource:     "server-a",
		toolName:       "fetch",
		conversationID: "conv-1",
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.Empty(t, result.SkillBreakdown)
}

func TestGetHooksSummary_SkillTimeSeriesWithSkillTypeFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-10 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "user@example.com",
		hookSource:     "local",
		toolSource:     "",
		toolName:       "Skill",
		skillName:      "golang",
		conversationID: "conv-1",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   deploymentID,
		timestamp:      now.Add(-8 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "user@example.com",
		hookSource:     "mcp",
		toolSource:     "server-a",
		toolName:       "fetch",
		conversationID: "conv-2",
	})

	time.Sleep(200 * time.Millisecond)

	// TypesToInclude=["skill"] scopes the overall summary to skill events,
	// but GetSkillTimeSeries hardcodes tool_name='Skill' so SkillTimeSeries
	// should still return the skill point regardless of what TypesToInclude is.
	result, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From:           now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:             now.Add(1 * time.Hour).Format(time.RFC3339),
		TypesToInclude: []string{"skill"},
	})

	require.NoError(t, err)
	require.Equal(t, int64(1), result.TotalEvents)
	require.Len(t, result.SkillTimeSeries, 1)
	require.Equal(t, "golang", result.SkillTimeSeries[0].SkillName)
}
