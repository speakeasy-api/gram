package telemetry_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	tunneledmcpRepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/assert"
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
	require.InDelta(t, 0, result.Totals.FailureRate, 0)
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
	skillTraceID := uuid.New().String()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-5 * time.Minute),
		traceID:        skillTraceID,
		userEmail:      "dana@example.com",
		hookSource:     "local",
		toolName:       "Skill",
		skillName:      "golang",
		conversationID: "conv-skill",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-4 * time.Minute),
		traceID:        skillTraceID,
		userEmail:      "dana@example.com",
		hookSource:     "local",
		toolName:       "Skill",
		skillName:      "golang",
		result:         `"ok"`,
		conversationID: "conv-skill",
	})

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err, "cause: %v", errors.Unwrap(err)) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}

		assert.Equal(c, int64(5), res.Totals.EventCount)
		assert.Equal(c, int64(4), res.Totals.SuccessCount)
		assert.Equal(c, int64(1), res.Totals.FailureCount)
		assert.InEpsilon(c, 0.2, res.Totals.FailureRate, 0.001)
		assert.Equal(c, int64(4), res.Totals.UniqueTools)
		assert.Equal(c, int64(4), res.Totals.UniqueUsers)
		assert.Equal(c, int64(4), res.Totals.UniqueTargets)

		targets := toolUsageTargetsByKey(res.Targets)
		hosted := targets["hosted_mcp_server:server:payments"]
		if assert.NotNil(c, hosted) {
			assert.Equal(c, "payments", hosted.TargetLabel)
			assert.Equal(c, int64(2), hosted.EventCount)
			assert.Equal(c, int64(1), hosted.SuccessCount)
			assert.Equal(c, int64(1), hosted.FailureCount)
			assert.InEpsilon(c, 0.5, hosted.FailureRate, 0.001)
		}

		shadow := targets["shadow_mcp_server:server:shadow-db"]
		if assert.NotNil(c, shadow) {
			assert.Equal(c, int64(1), shadow.EventCount)
		}

		local := targets["local_tool:local_tools:local"]
		if assert.NotNil(c, local) {
			assert.Equal(c, "Local Tools", local.TargetLabel)
		}

		skill := targets["skill:skill:golang"]
		if assert.NotNil(c, skill) {
			assert.Equal(c, "golang", skill.TargetLabel)
		}

		assert.NotEmpty(c, res.TargetTimeSeries)
		assert.NotEmpty(c, res.UserTimeSeries)
		assert.NotEmpty(c, res.UsersByTarget)
		assert.NotEmpty(c, res.TargetToolBreakdown)
	}, 10*time.Second, 200*time.Millisecond)
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err, "cause: %v", errors.Unwrap(err)) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.NotEmpty(c, res.Targets) {
			return
		}

		targets := toolUsageTargetsByKey(res.Targets)
		assert.NotNil(c, targets["hosted_mcp_server:server:hosted-payments"])
		assert.Nil(c, targets["shadow_mcp_server:server:hosted.example.com"])
	}, 10*time.Second, 200*time.Millisecond)
}

func TestGetToolUsageSummary_ClassifiesDirectTunneledMCP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	fixture := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{
		name: "Tunneled Postgres MCP",
		slug: "postgres-tunnel",
	})
	now := time.Now().UTC()
	insertDirectMCPToolEvent(t, ctx, ti, directMCPToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-5 * time.Minute),
		sourceID:    fixture.sourceID.String(),
		mcpServerID: fixture.mcpServerID.String(),
		toolName:    "query",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
			From:        now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:          now.Add(1 * time.Hour).Format(time.RFC3339),
			TargetTypes: []gen.ToolUsageTargetType{"tunneled_mcp_server"},
		})
		if !assert.NoError(c, err, "cause: %v", errors.Unwrap(err)) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Equal(c, int64(1), res.Totals.EventCount) {
			return
		}

		targets := toolUsageTargetsByKey(res.Targets)
		tunneled := targets["tunneled_mcp_server:server:postgres-tunnel"]
		if assert.NotNil(c, tunneled) {
			assert.Equal(c, "Tunneled Postgres MCP", tunneled.TargetLabel)
			assert.Equal(c, int64(1), tunneled.EventCount)
			assert.Equal(c, int64(1), tunneled.SuccessCount)
			assert.Equal(c, int64(0), tunneled.FailureCount)
		}
		assert.Nil(c, targets["shadow_mcp_server:server:"+fixture.sourceID.String()])
	}, 10*time.Second, 200*time.Millisecond)
}

func TestGetToolUsageSummary_FiltersByHookSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	// A hook event from the "cowork" agent.
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-15 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "alice@example.com",
		hookSource:     "cowork",
		toolSource:     "shadow-db",
		toolName:       "query",
		result:         `"ok"`,
		conversationID: "conv-cowork",
	})
	// A hook event from a different agent.
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-10 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "bob@example.com",
		hookSource:     "cursor",
		toolName:       "Read",
		result:         `"ok"`,
		conversationID: "conv-cursor",
	})
	// A direct hosted MCP call has no hook source and must be excluded when a
	// hook source filter is set.
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-5 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge",
		userEmail:   "carol@example.com",
		statusCode:  200,
	})

	payload := &gen.GetToolUsageSummaryPayload{
		From:        now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:          now.Add(1 * time.Hour).Format(time.RFC3339),
		HookSources: []string{"cowork"},
	}

	// Poll until ClickHouse reflects the inserts (eventual consistency). Only the
	// cowork event matches the filter, so the count settles at exactly 1.
	var result *gen.GetToolUsageSummaryResult
	require.Eventually(t, func() bool {
		var err error
		result, err = ti.service.GetToolUsageSummary(ctx, payload)
		return err == nil && result != nil && result.Totals.EventCount == 1
	}, 10*time.Second, 200*time.Millisecond, "expected only the cowork hook event in the filtered summary")

	targets := toolUsageTargetsByKey(result.Targets)
	require.NotNil(t, targets["shadow_mcp_server:server:shadow-db"])
	require.Nil(t, targets["local_tool:local_tools:local"])
	require.Nil(t, targets["hosted_mcp_server:server:payments"])
}

func TestGetToolUsageFilterOptions_ReturnsUncappedShadowServersAndUsers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	for i := range 30 {
		insertHookEvent(t, ctx, hookEventParams{
			projectID:      projectID,
			deploymentID:   uuid.New().String(),
			timestamp:      now.Add(time.Duration(-i) * time.Minute),
			traceID:        uuid.New().String(),
			userEmail:      fmt.Sprintf("user-%02d@example.com", i),
			hookSource:     "mcp",
			toolSource:     fmt.Sprintf("shadow-%02d", i),
			toolName:       "query",
			result:         `"ok"`,
			conversationID: fmt.Sprintf("conv-shadow-%02d", i),
		})
	}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Empty(c, res.HostedServers)
		assert.Len(c, res.ShadowServers, 30)
		assert.Len(c, res.Users, 30)
	}, 10*time.Second, 200*time.Millisecond)

	userOptionsOnly, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{
		From:        now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:          now.Add(1 * time.Hour).Format(time.RFC3339),
		OptionTypes: []gen.ToolUsageFilterOptionType{"users"},
	})

	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Empty(t, userOptionsOnly.HostedServers)
	require.Empty(t, userOptionsOnly.ShadowServers)
	require.Len(t, userOptionsOnly.Users, 30)

	summary, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, summary.Targets, 25)
}

func TestGetToolUsageFilterOptions_ClassifiesHookObservedHostedMCP(t *testing.T) {
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{
			From: now.Add(-1 * time.Hour).Format(time.RFC3339),
			To:   now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.HostedServers, 1) {
			return
		}
		assert.Equal(c, "hosted-payments", res.HostedServers[0].ToolsetSlug)
		assert.Equal(c, "Hosted Payments", res.HostedServers[0].ToolsetName)
		assert.Equal(c, int64(1), res.HostedServers[0].EventCount)
		assert.Empty(c, res.ShadowServers)
		if assert.Len(c, res.Users, 1) {
			assert.Equal(c, "alice@example.com", res.Users[0].UserKey)
		}
	}, 10*time.Second, 200*time.Millisecond)

	hostedOptionsOnly, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{
		From:        now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:          now.Add(1 * time.Hour).Format(time.RFC3339),
		OptionTypes: []gen.ToolUsageFilterOptionType{"hosted_servers"},
	})

	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, hostedOptionsOnly.HostedServers, 1)
	require.Empty(t, hostedOptionsOnly.ShadowServers)
	require.Empty(t, hostedOptionsOnly.Users)
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
	// Each hosted tool call is one gateway.toolCall span, so it carries a unique
	// trace_id (recorded by ToolProxy.Do). trace_id is FixedString(32) — strip the
	// UUID hyphens to get 32 hex chars. This is what lands the event in the
	// trace_summaries materialized view that now backs the tool-usage queries.
	traceID := strings.ReplaceAll(uuid.New().String(), "-", "")
	err = ti.chClient.InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
		ID:                   uuid.New().String(),
		TimeUnixNano:         p.timestamp.UnixNano(),
		ObservedTimeUnixNano: p.timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "hosted tool event",
		TraceID:              &traceID,
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

type tunneledMCPServerFixtureParams struct {
	name string
	slug string
}

type tunneledMCPServerFixture struct {
	sourceID    uuid.UUID
	mcpServerID uuid.UUID
}

func createTunneledMCPServerFixture(t *testing.T, ctx context.Context, ti *testInstance, p tunneledMCPServerFixtureParams) tunneledMCPServerFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sourceID := uuid.New()
	source, err := tunneledmcpRepo.New(ti.conn).CreateServer(ctx, tunneledmcpRepo.CreateServerParams{
		ID:        sourceID,
		ProjectID: *authCtx.ProjectID,
		Name:      p.name,
		KeyHash:   "test-key-hash-" + sourceID.String(),
		KeyPrefix: "gram_tunnel_test",
	})
	require.NoError(t, err)

	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	mcpServerID := uuid.New()
	server, err := mcpserversRepo.New(ti.conn).CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
		ID:                    mcpServerID,
		ProjectID:             *authCtx.ProjectID,
		Name:                  pgtype.Text{String: p.name, Valid: true},
		Slug:                  pgtype.Text{String: p.slug, Valid: true},
		EnvironmentID:         uuid.NullUUID{},
		UserSessionIssuerID:   uuid.NullUUID{UUID: issuer.ID, Valid: true},
		RemoteMcpServerID:     uuid.NullUUID{},
		TunneledMcpServerID:   uuid.NullUUID{UUID: source.ID, Valid: true},
		ToolsetID:             uuid.NullUUID{},
		ToolVariationsGroupID: uuid.NullUUID{},
		Visibility:            "private",
	})
	require.NoError(t, err)

	return tunneledMCPServerFixture{
		sourceID:    source.ID,
		mcpServerID: server.ID,
	}
}

type directMCPToolEventParams struct {
	projectID   string
	timestamp   time.Time
	sourceID    string
	mcpServerID string
	toolName    string
	userEmail   string
	statusCode  int
}

func insertDirectMCPToolEvent(t *testing.T, ctx context.Context, ti *testInstance, p directMCPToolEventParams) {
	t.Helper()

	attrs := map[string]any{
		"gram.event.source":              "mcp",
		"gram.tool.name":                 p.toolName,
		"gram.tool_call.source":          p.sourceID,
		"gram.remote_mcp_server.id":      p.sourceID,
		"gram.mcp_server.id":             p.mcpServerID,
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
	traceID := strings.ReplaceAll(uuid.New().String(), "-", "")
	err = ti.chClient.InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
		ID:                   uuid.New().String(),
		TimeUnixNano:         p.timestamp.UnixNano(),
		ObservedTimeUnixNano: p.timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "direct MCP tool event",
		TraceID:              &traceID,
		SpanID:               &spanID,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        p.projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "tools:externalmcp:" + p.sourceID + ":" + p.toolName,
		ServiceName:          "gram-remote-mcp",
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
