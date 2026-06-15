package telemetry_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestListToolUsageTraces_ReturnsHostedShadowLocalAndSkills(t *testing.T) {
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
		userEmail:      "bob@example.com",
		hookSource:     "cursor",
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
		hookSource:     "claude-code",
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
		hookSource:     "claude-code",
		toolName:       "Skill",
		result:         `"ok"`,
		skillName:      "golang",
		conversationID: "conv-skill",
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From:  now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:    now.Add(1 * time.Hour).Format(time.RFC3339),
		Limit: 10,
		Sort:  "desc",
	})

	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.NotNil(t, result)
	require.Len(t, result.Traces, 4)

	byTarget := make(map[string]*gen.ToolUsageTraceSummary)
	for _, row := range result.Traces {
		byTarget[string(row.TargetType)+":"+row.TargetID] = row
	}

	hosted := byTarget["hosted_mcp_server:payments"]
	require.NotNil(t, hosted)
	require.NotEmpty(t, hosted.ID)
	require.NotNil(t, hosted.LogGroup)
	require.NotEmpty(t, hosted.LogGroup.Value)
	require.Equal(t, "payments", hosted.TargetLabel)
	require.Equal(t, "charge", hosted.ToolName)
	require.Equal(t, "alice@example.com", hosted.UserLabel)
	require.Nil(t, hosted.HookSource)

	shadow := byTarget["shadow_mcp_server:shadow-db"]
	require.NotNil(t, shadow)
	require.Equal(t, "cursor", *shadow.HookSource)
	require.Equal(t, "success", *shadow.HookStatus)

	local := byTarget["local_tool:local"]
	require.NotNil(t, local)
	require.Equal(t, "Local Tools", local.TargetLabel)

	skill := byTarget["skill:golang"]
	require.NotNil(t, skill)
	require.Equal(t, "golang", skill.ToolName)
}

func TestListToolUsageTraces_FiltersByTargetsUsersAndHookSource(t *testing.T) {
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
		userEmail:      "bob@example.com",
		hookSource:     "cursor",
		toolSource:     "shadow-db",
		toolName:       "query",
		result:         `"ok"`,
		conversationID: "conv-shadow",
		customAttrs:    map[string]any{"gram.trigger.instance_id": "trigger_123"},
	})

	time.Sleep(200 * time.Millisecond)

	hostedOnly, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From:               now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:                 now.Add(1 * time.Hour).Format(time.RFC3339),
		TargetTypes:        []gen.ToolUsageTargetType{"hosted_mcp_server"},
		HostedToolsetSlugs: []string{"payments"},
		Limit:              10,
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, hostedOnly.Traces, 1)
	require.Equal(t, gen.ToolUsageTargetType("hosted_mcp_server"), hostedOnly.Traces[0].TargetType)

	attributeQuery := "conv-shadow"
	shadowCursorOnly, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From:              now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:                now.Add(1 * time.Hour).Format(time.RFC3339),
		ShadowServerNames: []string{"shadow-db"},
		HookSources:       []string{"cursor"},
		UserFilters:       []*gen.ToolUsageUserFilter{{Kind: "email", Key: "bob@example.com"}},
		Query:             &attributeQuery,
		Limit:             10,
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, shadowCursorOnly.Traces, 1)
	require.Equal(t, "shadow-db", shadowCursorOnly.Traces[0].TargetID)
	require.Equal(t, "cursor", *shadowCursorOnly.Traces[0].HookSource)

	triggerFiltered, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		Filters: []*gen.LogFilter{
			{
				Path:     "gram.trigger.instance_id",
				Operator: "eq",
				Values:   []string{"trigger_123"},
			},
		},
		Limit: 10,
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, triggerFiltered.Traces, 1)
	require.Equal(t, "shadow-db", triggerFiltered.Traces[0].TargetID)
}

func TestListToolUsageTraces_PaginatesWithOpaqueCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	for i, toolName := range []string{"first", "second", "third"} {
		insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
			projectID:   projectID,
			timestamp:   now.Add(time.Duration(i) * time.Minute),
			toolsetSlug: "payments",
			toolName:    toolName,
			userEmail:   "alice@example.com",
			statusCode:  200,
		})
	}

	time.Sleep(200 * time.Millisecond)

	page1, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From:  now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:    now.Add(1 * time.Hour).Format(time.RFC3339),
		Limit: 2,
		Sort:  "desc",
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, page1.Traces, 2)
	require.NotNil(t, page1.NextCursor)
	require.NotEmpty(t, *page1.NextCursor)

	page2, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From:   now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:     now.Add(1 * time.Hour).Format(time.RFC3339),
		Cursor: page1.NextCursor,
		Limit:  2,
		Sort:   "desc",
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, page2.Traces, 1)
	require.Nil(t, page2.NextCursor)

	seen := map[string]bool{}
	for _, trace := range append(page1.Traces, page2.Traces...) {
		require.False(t, seen[trace.ID])
		seen[trace.ID] = true
	}
}

func TestListToolUsageTraces_PrefersWorstStatusInGroupedTrace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()
	traceID := uuid.New().String()

	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-2 * time.Minute),
		traceID:        traceID,
		userEmail:      "alice@example.com",
		hookSource:     "cursor",
		toolSource:     "shadow-db",
		toolName:       "query",
		result:         `"ok"`,
		conversationID: "conv-status",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-1 * time.Minute),
		traceID:        traceID,
		userEmail:      "alice@example.com",
		hookSource:     "cursor",
		toolSource:     "shadow-db",
		toolName:       "query",
		conversationID: "conv-status",
		customAttrs: map[string]any{
			"gram.hook.block_reason": "policy denied",
		},
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From:  now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:    now.Add(1 * time.Hour).Format(time.RFC3339),
		Limit: 10,
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, result.Traces, 1)
	require.NotNil(t, result.Traces[0].HookStatus)
	require.Equal(t, "blocked", *result.Traces[0].HookStatus)
	require.NotNil(t, result.Traces[0].BlockReason)
	require.Equal(t, "policy denied", *result.Traces[0].BlockReason)
}

func TestListToolUsageTraces_IncludesTriggerOnlyRowsForTriggerFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertTriggerOnlyLog(t, ctx, ti, triggerOnlyLogParams{
		projectID:         projectID,
		timestamp:         now.Add(-1 * time.Minute),
		triggerInstanceID: "trigger_123",
		triggerEventID:    "event_123",
		body:              "trigger delivered",
	})

	time.Sleep(200 * time.Millisecond)

	result, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		Filters: []*gen.LogFilter{
			{
				Path:     "gram.trigger.instance_id",
				Operator: "eq",
				Values:   []string{"trigger_123"},
			},
		},
		Limit: 10,
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, result.Traces, 1)
	require.Equal(t, gen.ToolUsageTargetType("local_tool"), result.Traces[0].TargetType)
	require.Equal(t, "local", result.Traces[0].TargetID)
	require.NotNil(t, result.Traces[0].LogGroup)
	require.Equal(t, gen.ToolUsageTraceLogGroupKind("trigger_event_id"), result.Traces[0].LogGroup.Kind)
	require.Equal(t, "event_123", result.Traces[0].LogGroup.Value)
}

type triggerOnlyLogParams struct {
	projectID         string
	timestamp         time.Time
	triggerInstanceID string
	triggerEventID    string
	body              string
}

func insertTriggerOnlyLog(t *testing.T, ctx context.Context, ti *testInstance, p triggerOnlyLogParams) {
	t.Helper()

	attrs := map[string]any{
		"gram.event.source":            "trigger",
		"gram.trigger.instance_id":     p.triggerInstanceID,
		"gram.trigger.event_id":        p.triggerEventID,
		"gram.trigger.delivery_status": "sent",
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	err = ti.chClient.InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
		ID:                   uuid.New().String(),
		TimeUnixNano:         p.timestamp.UnixNano(),
		ObservedTimeUnixNano: p.timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 p.body,
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        p.projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "triggers:" + p.triggerEventID,
		ServiceName:          "gram-triggers",
		ServiceVersion:       nil,
		GramChatID:           nil,
	})
	require.NoError(t, err)
}
