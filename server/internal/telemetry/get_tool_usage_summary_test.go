package telemetry_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/stretchr/testify/require"
)

func TestGetToolUsageSummary_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	result, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.NotNil(t, result)
	require.Equal(t, int64(0), result.Totals.EventCount)
	require.Equal(t, int64(0), result.Totals.SuccessCount)
	require.Equal(t, int64(0), result.Totals.FailureCount)
	require.Equal(t, float64(0), result.Totals.FailureRate)
	require.Equal(t, int64(0), result.Totals.UniqueTools)
	require.Equal(t, int64(0), result.Totals.UniqueUsers)
	require.Equal(t, int64(0), result.Totals.UniqueTargets)
	require.Empty(t, result.Targets)
	require.Empty(t, result.Users)
	require.Empty(t, result.TargetTimeSeries)
	require.Empty(t, result.UserTimeSeries)
	require.Empty(t, result.UsersByTarget)
	require.Empty(t, result.TargetToolBreakdown)
}

func TestGetToolUsageSummary_AggregatesHostedShadowLocalAndSkills(t *testing.T) {
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
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-19 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  500,
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-15 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "bob@example.com",
		hookSource:     "mcp",
		toolSource:     "shadow-db",
		toolName:       "query",
		result:         `"ok"`,
		conversationID: "conv-shadow",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-10 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "carol@example.com",
		hookSource:     "local",
		toolName:       "Read",
		result:         `"ok"`,
		conversationID: "conv-local",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-5 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "dana@example.com",
		hookSource:     "local",
		toolName:       "Skill",
		result:         `"ok"`,
		skillName:      "golang",
		conversationID: "conv-skill",
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.NotNil(t, result)
	require.Equal(t, int64(5), result.Totals.EventCount)
	require.Equal(t, int64(4), result.Totals.SuccessCount)
	require.Equal(t, int64(1), result.Totals.FailureCount)
	require.Equal(t, 0.2, result.Totals.FailureRate)
	require.Equal(t, int64(4), result.Totals.UniqueTools)
	require.Equal(t, int64(4), result.Totals.UniqueUsers)
	require.Equal(t, int64(4), result.Totals.UniqueTargets)

	targets := toolUsageTargetsByKey(result.Targets)
	hosted := targets["hosted_mcp_server:server:payments"]
	require.NotNil(t, hosted)
	require.Equal(t, "payments", hosted.TargetLabel)
	require.Equal(t, int64(2), hosted.EventCount)
	require.Equal(t, int64(1), hosted.SuccessCount)
	require.Equal(t, int64(1), hosted.FailureCount)
	require.Equal(t, 0.5, hosted.FailureRate)

	shadow := targets["shadow_mcp_server:server:shadow-db"]
	require.NotNil(t, shadow)
	require.Equal(t, int64(1), shadow.EventCount)

	local := targets["local_tool:local_tools:local"]
	require.NotNil(t, local)
	require.Equal(t, "Local Tools", local.TargetLabel)

	skill := targets["skill:skill:golang"]
	require.NotNil(t, skill)
	require.Equal(t, "golang", skill.TargetLabel)

	require.NotEmpty(t, result.TargetTimeSeries)
	require.NotEmpty(t, result.UserTimeSeries)
	require.NotEmpty(t, result.UsersByTarget)
	require.NotEmpty(t, result.TargetToolBreakdown)
}

func TestGetToolUsageSummary_ClassifiesHookObservedHostedMCP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	toolsets := toolsetsRepo.New(ti.conn)
	_, err := toolsets.CreateToolset(ctx, toolsetsRepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Hosted Payments",
		Slug:                   "hosted-payments",
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{String: "acme-hosted-payments", Valid: true},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-5 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "alice@example.com",
		hookSource:     "mcp",
		toolSource:     "hosted.example.com",
		toolName:       "charge",
		result:         `"ok"`,
		mcpMatch:       "acme-hosted-payments",
		mcpServerURL:   "https://app.example.com/mcp/acme-hosted-payments",
		conversationID: "conv-hosted-hook",
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	targets := toolUsageTargetsByKey(result.Targets)
	require.NotNil(t, targets["hosted_mcp_server:server:hosted-payments"])
	require.Nil(t, targets["shadow_mcp_server:server:hosted.example.com"])
}

type hostedToolEventParams struct {
	projectID   string
	timestamp   time.Time
	toolsetSlug string
	toolName    string
	userEmail   string
	statusCode  int
}

func insertHostedToolEvent(t *testing.T, ctx context.Context, ti *testInstance, p hostedToolEventParams) {
	t.Helper()

	attrs := map[string]any{
		"gram.event.source":              "hosted",
		"gram.tool.name":                 p.toolName,
		"gram.toolset.slug":              p.toolsetSlug,
		"http.response.status_code":      p.statusCode,
		"http.server.request.duration":   0.05,
		"user.email":                     p.userEmail,
		"gen_ai.tool.call.result":        `"ok"`,
		"gen_ai.tool.call.id":            uuid.New().String(),
		"gen_ai.conversation.id":         uuid.New().String(),
		"gen_ai.response.finish_reasons": []string{"tool_calls"},
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	spanID := uuid.New().String()[:16]
	err = ti.chClient.InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
		ID:                   uuid.New().String(),
		TimeUnixNano:         p.timestamp.UnixNano(),
		ObservedTimeUnixNano: p.timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "hosted tool event",
		TraceID:              nil,
		SpanID:               &spanID,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        p.projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "tools:http:gram:" + p.toolName,
		ServiceName:          "gram-http-gateway",
		ServiceVersion:       nil,
		GramChatID:           nil,
	})
	require.NoError(t, err)
}

func toolUsageTargetsByKey(rows []*gen.ToolUsageTargetSummary) map[string]*gen.ToolUsageTargetSummary {
	result := make(map[string]*gen.ToolUsageTargetSummary, len(rows))
	for _, row := range rows {
		result[string(row.TargetType)+":"+string(row.TargetKind)+":"+row.TargetID] = row
	}
	return result
}
