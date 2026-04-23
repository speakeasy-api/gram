package telemetry_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestListHooksTraces_IncludesSkillMetadataFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	traceID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	now := time.Now().UTC()
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-2 * time.Minute),
		traceID:      &traceID,
		gramURN:      "tools:hooks:skill",
		severity:     "INFO",
		serviceName:  "test-service",
		toolName:     new("Skill"),
		eventSource:  new("hook"),
		customAttrs: map[string]any{
			"gram.skill.scope":             "project",
			"gram.skill.discovery_root":    "project_agents",
			"gram.skill.source_type":       "local_filesystem",
			"gram.skill.id":                "skill-123",
			"gram.skill.version_id":        "version-456",
			"gram.skill.resolution_status": "resolved",
			"gen_ai.tool.call.arguments":   `{"skill":"golang"}`,
			"gram.hook.event":              "PostToolUse",
		},
	})

	// ClickHouse eventual consistency for materialized views
	time.Sleep(500 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	result, err := ti.service.ListHooksTraces(ctx, &gen.ListHooksTracesPayload{
		From:  from,
		To:    to,
		Limit: 100,
		Sort:  "desc",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Traces, 1)

	trace := result.Traces[0]
	require.Equal(t, traceID, trace.TraceID)
	require.Equal(t, "golang", requireNonNil(t, trace.SkillName))
	require.Equal(t, "project", requireNonNil(t, trace.SkillScope))
	require.Equal(t, "project_agents", requireNonNil(t, trace.SkillDiscoveryRoot))
	require.Equal(t, "local_filesystem", requireNonNil(t, trace.SkillSourceType))
	require.Equal(t, "skill-123", requireNonNil(t, trace.SkillID))
	require.Equal(t, "version-456", requireNonNil(t, trace.SkillVersionID))
	require.Equal(t, "resolved", requireNonNil(t, trace.SkillResolutionStatus))
	require.Equal(t, "success", requireNonNil(t, trace.HookStatus))
}

func TestListHooksTraces_OmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	skillTraceID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	localTraceID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	now := time.Now().UTC()

	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-2 * time.Minute),
		traceID:      &skillTraceID,
		gramURN:      "tools:hooks:skill",
		severity:     "INFO",
		serviceName:  "test-service",
		toolName:     new("Skill"),
		eventSource:  new("hook"),
		customAttrs: map[string]any{
			"gram.skill.scope":             "project",
			"gram.skill.discovery_root":    "project_agents",
			"gram.skill.source_type":       "local_filesystem",
			"gram.skill.id":                "skill-123",
			"gram.skill.version_id":        "version-456",
			"gram.skill.resolution_status": "resolved",
			"gen_ai.tool.call.arguments":   `{"skill":"golang"}`,
			"gram.hook.event":              "PostToolUse",
		},
	})

	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-1 * time.Minute),
		traceID:      &localTraceID,
		gramURN:      "tools:hooks:local",
		severity:     "INFO",
		serviceName:  "test-service",
		eventSource:  new("hook"),
		customAttrs: map[string]any{
			"gram.hook.event": "PostToolUse",
		},
	})

	// ClickHouse eventual consistency for materialized views
	time.Sleep(500 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	result, err := ti.service.ListHooksTraces(ctx, &gen.ListHooksTracesPayload{
		From:  from,
		To:    to,
		Limit: 100,
		Sort:  "desc",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	var trace *gen.HookTraceSummary
	for _, item := range result.Traces {
		if item.TraceID == localTraceID {
			trace = item
			break
		}
	}
	require.NotNil(t, trace)
	require.Equal(t, "success", requireNonNil(t, trace.HookStatus))
	require.Nil(t, trace.SkillName)
	require.Nil(t, trace.SkillScope)
	require.Nil(t, trace.SkillDiscoveryRoot)
	require.Nil(t, trace.SkillSourceType)
	require.Nil(t, trace.SkillID)
	require.Nil(t, trace.SkillVersionID)
	require.Nil(t, trace.SkillResolutionStatus)
}

func requireNonNil(t *testing.T, v *string) string {
	t.Helper()
	require.NotNil(t, v)
	return *v
}
