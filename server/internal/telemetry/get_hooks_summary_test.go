package telemetry_test

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/assert"
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}

		// Total events = 3 traces (trace_summaries counts at trace level, not log level), sessions = 2 unique conversation IDs
		assert.Equal(c, int64(3), res.TotalEvents)
		assert.Equal(c, int64(2), res.TotalSessions)

		// Server aggregation: 2 servers
		serversByName := make(map[string]*gen.HooksServerSummary)
		for _, s := range res.Servers {
			serversByName[s.ServerName] = s
		}
		if !assert.Len(c, serversByName, 2) {
			return
		}
		assert.Equal(c, int64(2), serversByName["server-a"].EventCount)
		assert.Equal(c, int64(1), serversByName["server-a"].SuccessCount)
		assert.Equal(c, int64(1), serversByName["server-a"].FailureCount)
		assert.Equal(c, int64(1), serversByName["server-b"].EventCount)
		assert.Equal(c, int64(1), serversByName["server-b"].SuccessCount)
		assert.Equal(c, int64(0), serversByName["server-b"].FailureCount)

		// User aggregation: 2 users
		usersByEmail := make(map[string]*gen.HooksUserSummary)
		for _, u := range res.Users {
			usersByEmail[u.UserEmail] = u
		}
		if !assert.Len(c, usersByEmail, 2) {
			return
		}
		assert.Equal(c, int64(2), usersByEmail["user1@example.com"].EventCount)
		assert.Equal(c, int64(1), usersByEmail["user1@example.com"].SuccessCount)
		assert.Equal(c, int64(1), usersByEmail["user1@example.com"].FailureCount)
		assert.Equal(c, int64(1), usersByEmail["user2@example.com"].EventCount)
		assert.Equal(c, int64(1), usersByEmail["user2@example.com"].SuccessCount)
		assert.Equal(c, int64(0), usersByEmail["user2@example.com"].FailureCount)

		// Breakdown: one entry for user1/server-a/weather covering both traces.
		// trace_summaries aggregates has_error=1 for the failure trace via gram.hook.error.
		breakdownKey := func(b *gen.HooksBreakdownRow) string {
			return b.UserEmail + "|" + b.ServerName + "|" + b.ToolName
		}
		breakdownMap := make(map[string]*gen.HooksBreakdownRow)
		for _, b := range res.Breakdown {
			breakdownMap[breakdownKey(b)] = b
		}
		row := breakdownMap["user1@example.com|server-a|weather"]
		if !assert.NotNil(c, row) {
			return
		}
		assert.Equal(c, int64(2), row.EventCount)
		assert.Equal(c, int64(1), row.FailureCount)

		// Time series: at least one point
		assert.NotEmpty(c, res.TimeSeries)
		for _, pt := range res.TimeSeries {
			assert.NotEmpty(c, pt.BucketStartNs)
			assert.Positive(c, pt.EventCount)
		}
	}, 10*time.Second, 200*time.Millisecond)
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

	// Filter to only "mcp" type — tool_source != '' AND tool_name != 'Skill'
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From:           now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:             now.Add(1 * time.Hour).Format(time.RFC3339),
			TypesToInclude: []string{"mcp"},
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(1), res.TotalEvents)
		if !assert.Len(c, res.Servers, 1) {
			return
		}
		assert.Equal(c, "remote-server", res.Servers[0].ServerName)
	}, 10*time.Second, 200*time.Millisecond)
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

	// Filter to only alice's events
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
			Filters: []*gen.LogFilter{
				{Path: "user.email", Operator: "eq", Values: []string{"alice@example.com"}},
			},
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(1), res.TotalEvents)
		if !assert.Len(c, res.Users, 1) {
			return
		}
		assert.Equal(c, "alice@example.com", res.Users[0].UserEmail)
	}, 10*time.Second, 200*time.Millisecond)
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
	mcpMatch       string // gram.mcp.match for matched MCP inventory entries
	mcpServerURL   string // gram.mcp.server_url for matched MCP server URLs
	conversationID string // genai.conversation.id for session counting
	customAttrs    map[string]any
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
		skillArgs, marshalErr := json.Marshal(map[string]any{"skill": p.skillName})
		require.NoError(t, marshalErr)
		attrs["gen_ai"] = map[string]any{
			"tool": map[string]any{
				"call": map[string]any{
					"arguments": string(skillArgs),
				},
			},
		}
	}
	if p.mcpMatch != "" {
		attrs["gram.mcp.match"] = p.mcpMatch
	}
	if p.mcpServerURL != "" {
		attrs["gram.mcp.server_url"] = p.mcpServerURL
	}
	maps.Copy(attrs, p.customAttrs)

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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}

		assert.NotEmpty(c, res.SkillTimeSeries)

		countBySkill := make(map[string]int64)
		for _, pt := range res.SkillTimeSeries {
			assert.NotEmpty(c, pt.BucketStartNs)
			countBySkill[pt.SkillName] += pt.EventCount
		}
		assert.Equal(c, int64(2), countBySkill["golang"])
		assert.Equal(c, int64(1), countBySkill["typescript"])
	}, 10*time.Second, 200*time.Millisecond)
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

	// Wait for the non-skill event to land (TotalEvents == 1), then assert
	// SkillTimeSeries stays empty.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(1), res.TotalEvents)
		assert.Empty(c, res.SkillTimeSeries)
	}, 10*time.Second, 200*time.Millisecond)
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(2), res.TotalEvents)
		if !assert.Len(c, res.SkillTimeSeries, 1) {
			return
		}
		assert.Equal(c, "golang", res.SkillTimeSeries[0].SkillName)
		assert.Equal(c, int64(1), res.SkillTimeSeries[0].EventCount)
	}, 10*time.Second, 200*time.Millisecond)
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(4), res.TotalEvents)

		type key struct{ skill, user string }
		bySkillUser := make(map[key]int64)
		for _, row := range res.SkillBreakdown {
			bySkillUser[key{row.SkillName, row.UserEmail}] += row.UseCount
		}

		assert.Equal(c, int64(2), bySkillUser[key{"golang", "user1@example.com"}])
		assert.Equal(c, int64(1), bySkillUser[key{"golang", "user2@example.com"}])
		assert.Equal(c, int64(1), bySkillUser[key{"typescript", "user2@example.com"}])
		assert.NotContains(c, bySkillUser, key{"typescript", "user1@example.com"})
	}, 10*time.Second, 200*time.Millisecond)
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

	// Wait for the non-skill event to land (TotalEvents == 1), then assert
	// SkillBreakdown stays empty.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(1), res.TotalEvents)
		assert.Empty(c, res.SkillBreakdown)
	}, 10*time.Second, 200*time.Millisecond)
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

	// TypesToInclude=["skill"] scopes the overall summary to skill events,
	// but skill_time_series and skill_breakdown hardcode tool_name='Skill' so they
	// return skill data regardless of TypesToInclude.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From:           now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:             now.Add(1 * time.Hour).Format(time.RFC3339),
			TypesToInclude: []string{"skill"},
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(1), res.TotalEvents)
		if !assert.Len(c, res.SkillTimeSeries, 1) {
			return
		}
		assert.Equal(c, "golang", res.SkillTimeSeries[0].SkillName)
		if !assert.Len(c, res.SkillBreakdown, 1) {
			return
		}
		assert.Equal(c, "golang", res.SkillBreakdown[0].SkillName)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestGetHooksSummary_SkillFieldsIgnoreTypesToInclude(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	// One skill event and one MCP event.
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

	// TypesToInclude=["mcp"] scopes TotalEvents/Servers/Users to MCP events only,
	// but skill_time_series and skill_breakdown hardcode tool_name='Skill' so they
	// must always return skill data regardless of TypesToInclude.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From:           now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:             now.Add(1 * time.Hour).Format(time.RFC3339),
			TypesToInclude: []string{"mcp"},
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(1), res.TotalEvents) // only MCP event counted
		if !assert.Len(c, res.SkillTimeSeries, 1) {
			return
		}
		assert.Equal(c, "golang", res.SkillTimeSeries[0].SkillName)
		if !assert.Len(c, res.SkillBreakdown, 1) {
			return
		}
		assert.Equal(c, "golang", res.SkillBreakdown[0].SkillName)
	}, 10*time.Second, 200*time.Millisecond)
}
