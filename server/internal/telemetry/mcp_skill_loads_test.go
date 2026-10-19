package telemetry_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// insertMCPSkillLoad writes the row the hosted MCP skill:// resources/read
// path logs: a resource_read named Skill with the skill name as its only
// argument, attributed to the server it was served through.
func insertMCPSkillLoad(t *testing.T, ctx context.Context, ti *testInstance, projectID string, timestamp time.Time, toolsetSlug, skillName string) {
	t.Helper()

	attrs := map[string]any{
		"gram.event.source":          "resource_read",
		"gram.tool.name":             "Skill",
		"gen_ai.tool.call.arguments": `{"skill":"` + skillName + `"}`,
		"gram.toolset.slug":          toolsetSlug,
		"gram.resource.uri":          "skill://" + skillName + "/SKILL.md",
		"gram.mcp.skill.id":          uuid.NewString(),
		"gram.mcp.skill.version_id":  uuid.NewString(),
		"gram.mcp.skill.digest":      "sha256:00",
		"http.response.status_code":  200,
		"user.email":                 "erin@example.com",
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	traceID := strings.ReplaceAll(uuid.New().String(), "-", "")
	spanID := uuid.New().String()[:16]
	err = ti.chClient.InsertTelemetryLogsSync(ctx, []telemetryRepo.InsertTelemetryLogParams{{
		ID:                   uuid.New().String(),
		TimeUnixNano:         timestamp.UnixNano(),
		ObservedTimeUnixNano: timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "",
		TraceID:              &traceID,
		SpanID:               &spanID,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              urn.NewSkill(uuid.New()).String(),
		ServiceName:          "gram-server",
		ServiceVersion:       nil,
		GramChatID:           nil,
	}})
	require.NoError(t, err)
}

func TestListToolUsageTraces_ClassifiesMCPSkillLoadAsSkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-20 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	insertMCPSkillLoad(t, ctx, ti, projectID, now.Add(-10*time.Minute), "payments", "golang")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	rawPathQuery := ":"
	cases := map[string]*gen.ListToolUsageTracesPayload{
		"summary path": {From: from, To: to, Limit: 10, Sort: "desc"},
		"raw path":     {From: from, To: to, Query: &rawPathQuery, Limit: 10, Sort: "desc"},
	}
	for name, payload := range cases {
		result := waitForToolUsageTraces(t, ctx, ti, payload, func(result *gen.ListToolUsageTracesResult) bool {
			return len(result.Traces) == 2
		})
		require.Len(t, result.Traces, 2, name)

		byTarget := make(map[string]*gen.ToolUsageTraceSummary)
		for _, row := range result.Traces {
			byTarget[string(row.TargetType)+":"+row.TargetID] = row
		}

		hosted := byTarget["hosted_mcp_server:payments"]
		require.NotNil(t, hosted, name)
		require.Equal(t, "charge", hosted.ToolName, name)

		skill := byTarget["skill:golang"]
		require.NotNil(t, skill, name)
		require.Equal(t, gen.ToolUsageTargetKind("skill"), skill.TargetKind, name)
		require.Equal(t, "golang", skill.TargetLabel, name)
		require.Equal(t, "golang", skill.ToolName, name)
		require.Equal(t, "resource_read", skill.EventSource, name)
		require.Nil(t, skill.HookStatus, name)
		require.NotNil(t, skill.HTTPStatusCode, name)
		require.Equal(t, int32(200), *skill.HTTPStatusCode, name)
	}

	// The dashboard splits a skill's channel on the event source.
	viaMCP := waitForToolUsageTraces(t, ctx, ti, &gen.ListToolUsageTracesPayload{
		From:        from,
		To:          to,
		TargetTypes: []gen.ToolUsageTargetType{"skill"},
		Filters:     []*gen.LogFilter{{Path: "gram.event.source", Operator: "not_eq", Values: []string{"hook"}}},
		Limit:       10,
	}, func(result *gen.ListToolUsageTracesResult) bool {
		return len(result.Traces) == 1
	})
	require.Len(t, viaMCP.Traces, 1)
	require.Equal(t, "golang", viaMCP.Traces[0].TargetID)
}

func TestGetToolUsageSummary_CountsMCPSkillLoadUnderSkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-20 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-15 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "dana@example.com",
		hookSource:     "claude-code",
		toolName:       "Skill",
		skillName:      "golang",
		result:         `"ok"`,
		conversationID: "conv-skill",
	})
	insertMCPSkillLoad(t, ctx, ti, projectID, now.Add(-10*time.Minute), "payments", "golang")

	// The fixtures are inserted synchronously and trace_summaries is fed by
	// its materialized view on insert, so the summary is read once.
	res, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))

	require.Equal(t, int64(3), res.Totals.EventCount)
	require.Equal(t, int64(3), res.Totals.SuccessCount)

	targets := toolUsageTargetsByKey(res.Targets)
	hosted := targets["hosted_mcp_server:server:payments"]
	require.NotNil(t, hosted)
	require.Equal(t, int64(1), hosted.EventCount, "a skill load is not a call to the server it was read through")
	skill := targets["skill:skill:golang"]
	require.NotNil(t, skill)
	require.Equal(t, int64(2), skill.EventCount, "hook and MCP loads of one skill are one target")
	require.Equal(t, int64(2), skill.SuccessCount)
}
