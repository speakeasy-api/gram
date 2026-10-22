package telemetry_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// createHostedMCPServerFixture creates an MCP-enabled toolset and its canonical
// mcp_servers wrapper (id = toolset id).
func createHostedMCPServerFixture(t *testing.T, ctx context.Context, ti *testInstance, name, slug string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.New(ti.conn).CreateToolset(ctx, toolsetsRepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   name,
		Slug:                   slug,
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{String: "acme-" + slug, Valid: true},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	return addToolsetWrapper(t, ctx, ti, toolset.ID, toolset.ID, "acme-"+slug)
}

// addToolsetWrapper adds an mcp_servers row serving the toolset.
func addToolsetWrapper(t *testing.T, ctx context.Context, ti *testInstance, toolsetID, id uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	server, err := mcpserversRepo.New(ti.conn).CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
		ID:                    id,
		ProjectID:             *authCtx.ProjectID,
		Name:                  pgtype.Text{String: slug, Valid: true},
		Slug:                  pgtype.Text{String: slug, Valid: true},
		EnvironmentID:         uuid.NullUUID{},
		UserSessionIssuerID:   uuid.NullUUID{UUID: issuer.ID, Valid: true},
		RemoteMcpServerID:     uuid.NullUUID{},
		TunneledMcpServerID:   uuid.NullUUID{},
		ToolsetID:             uuid.NullUUID{UUID: toolsetID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{},
		ToolVariationsGroupID: uuid.NullUUID{},
		Visibility:            "private",
		NetworkAccessMode:     pgtype.Text{},
	})
	require.NoError(t, err)
	return server.ID
}

func TestToolUsage_FiltersByMCPServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	billingID := createHostedMCPServerFixture(t, ctx, ti, "Billing", "billing")
	otherID := createHostedMCPServerFixture(t, ctx, ti, "Other", "other")
	tunneled := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{
		name: "Tunneled Postgres MCP",
		slug: "postgres-tunnel",
	})

	// Stamped with the server id.
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-20 * time.Minute),
		toolsetSlug: "billing",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
		mcpServerID: billingID.String(),
	})
	// Recorded before hosted servers stamped an id.
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-18 * time.Minute),
		toolsetSlug: "billing",
		toolName:    "refund",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	// Hook-observed call against the hosted server's URL.
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-16 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "bob@example.com",
		hookSource:     "mcp",
		toolSource:     "billing.example.com",
		toolName:       "invoice",
		result:         `"ok"`,
		mcpMatch:       "acme-billing",
		mcpServerURL:   "https://app.example.com/mcp/acme-billing",
		conversationID: "conv-billing-hook",
	})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-14 * time.Minute),
		toolsetSlug: "other",
		toolName:    "noop",
		userEmail:   "carol@example.com",
		statusCode:  200,
		mcpServerID: otherID.String(),
	})
	insertDirectMCPToolEvent(t, ctx, ti, directMCPToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-12 * time.Minute),
		sourceID:    tunneled.sourceID.String(),
		mcpServerID: tunneled.mcpServerID.String(),
		toolName:    "query",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	billingTools := []string{"charge", "invoice", "refund"}

	traceTools := func(traces []*gen.ToolUsageTraceSummary) []string {
		names := make([]string, 0, len(traces))
		for _, trace := range traces {
			names = append(names, trace.ToolName)
		}
		return names
	}

	t.Run("it lists every billing trace by server id, including unstamped rows", func(t *testing.T) {
		t.Parallel()
		res, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
			From:         from,
			To:           to,
			McpServerIds: []string{billingID.String()},
			Limit:        10,
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		require.ElementsMatch(t, billingTools, traceTools(res.Traces))
		for _, trace := range res.Traces {
			require.NotNil(t, trace.McpServerID)
			require.Equal(t, billingID.String(), *trace.McpServerID)
		}
	})

	t.Run("it lists the same traces by the legacy toolset slug", func(t *testing.T) {
		t.Parallel()
		res, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
			From:               from,
			To:                 to,
			HostedToolsetSlugs: []string{"billing"},
			Limit:              10,
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		require.ElementsMatch(t, billingTools, traceTools(res.Traces))
	})

	t.Run("it lists a tunneled server by its server id", func(t *testing.T) {
		t.Parallel()
		res, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
			From:         from,
			To:           to,
			McpServerIds: []string{tunneled.mcpServerID.String()},
			Limit:        10,
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		require.Len(t, res.Traces, 1)
		require.Equal(t, gen.ToolUsageTargetType("tunneled_mcp_server"), res.Traces[0].TargetType)
		require.Equal(t, tunneled.mcpServerID.String(), *res.Traces[0].McpServerID)
	})

	t.Run("it filters raw-path traces by server id", func(t *testing.T) {
		t.Parallel()
		query := "tools:"
		res, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
			From:         from,
			To:           to,
			McpServerIds: []string{billingID.String()},
			Query:        &query,
			Limit:        10,
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		require.ElementsMatch(t, []string{"charge", "refund"}, traceTools(res.Traces))
		for _, trace := range res.Traces {
			require.Equal(t, billingID.String(), *trace.McpServerID)
		}
	})

	t.Run("it summarizes by server id and tags targets with it", func(t *testing.T) {
		t.Parallel()
		res, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
			From:         from,
			To:           to,
			McpServerIds: []string{billingID.String()},
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		require.Equal(t, int64(3), res.Totals.EventCount)
		targets := toolUsageTargetsByKey(res.Targets)
		billing := targets["hosted_mcp_server:server:billing"]
		require.NotNil(t, billing)
		require.Equal(t, billingID.String(), *billing.McpServerID)
		require.Len(t, res.Targets, 1)
	})

	t.Run("it summarizes the raw path by server id", func(t *testing.T) {
		t.Parallel()
		query := "tools:"
		res, err := ti.service.GetToolUsageTotals(ctx, &gen.GetToolUsageTotalsPayload{
			From:         from,
			To:           to,
			McpServerIds: []string{billingID.String()},
			Query:        &query,
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		require.Equal(t, int64(2), res.Totals.EventCount)
	})

	t.Run("it returns server ids on hosted filter options", func(t *testing.T) {
		t.Parallel()
		res, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{
			From:        from,
			To:          to,
			OptionTypes: []gen.ToolUsageFilterOptionType{"hosted_servers"},
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		ids := map[string]string{}
		for _, option := range res.HostedServers {
			if option.McpServerID != nil {
				ids[option.ToolsetSlug] = *option.McpServerID
			}
		}
		require.Equal(t, billingID.String(), ids["billing"])
		require.Equal(t, otherID.String(), ids["other"])
	})
}

func TestToolUsage_ToolsetServedByTwoServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	canonicalID := createHostedMCPServerFixture(t, ctx, ti, "Shared", "shared")
	secondID := addToolsetWrapper(t, ctx, ti, canonicalID, uuid.New(), "acme-shared-two")

	for _, row := range []struct {
		tool     string
		serverID string
	}{
		{tool: "via_canonical", serverID: canonicalID.String()},
		{tool: "via_second", serverID: secondID.String()},
		{tool: "unstamped", serverID: ""},
	} {
		insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
			projectID:   projectID,
			timestamp:   now.Add(-10 * time.Minute),
			toolsetSlug: "shared",
			toolName:    row.tool,
			userEmail:   "alice@example.com",
			statusCode:  200,
			mcpServerID: row.serverID,
		})
	}

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	listTools := func(t *testing.T, ids []string) []string {
		t.Helper()
		res, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
			From: from, To: to, McpServerIds: ids, Limit: 10,
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		names := make([]string, 0, len(res.Traces))
		for _, trace := range res.Traces {
			names = append(names, trace.ToolName)
		}
		return names
	}

	t.Run("it matches unstamped rows to the canonical wrapper", func(t *testing.T) {
		t.Parallel()
		require.ElementsMatch(t, []string{"via_canonical", "unstamped"}, listTools(t, []string{canonicalID.String()}))
	})

	t.Run("it matches the second wrapper's own calls only", func(t *testing.T) {
		t.Parallel()
		require.ElementsMatch(t, []string{"via_second"}, listTools(t, []string{secondID.String()}))
	})

	t.Run("it matches an uppercase id", func(t *testing.T) {
		t.Parallel()
		require.ElementsMatch(t, []string{"via_second"}, listTools(t, []string{strings.ToUpper(secondID.String())}))
	})

	t.Run("it leaves the toolset option and target on the slug", func(t *testing.T) {
		t.Parallel()
		options, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{
			From: from, To: to, OptionTypes: []gen.ToolUsageFilterOptionType{"hosted_servers"},
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		require.Len(t, options.HostedServers, 1)
		require.Equal(t, "shared", options.HostedServers[0].ToolsetSlug)
		require.Nil(t, options.HostedServers[0].McpServerID)

		summary, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{From: from, To: to})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		target := toolUsageTargetsByKey(summary.Targets)["hosted_mcp_server:server:shared"]
		require.NotNil(t, target)
		require.Equal(t, int64(3), target.EventCount)
		require.Nil(t, target.McpServerID)
	})
}

func TestToolUsage_DeletedCanonicalWrapperIsNotTheFallback(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	canonicalID := createHostedMCPServerFixture(t, ctx, ti, "Rewrapped", "rewrapped")
	_, err := mcpserversRepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversRepo.DeleteMCPServerParams{
		ID:        canonicalID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	liveID := addToolsetWrapper(t, ctx, ti, canonicalID, uuid.New(), "acme-rewrapped-live")

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-10 * time.Minute),
		toolsetSlug: "rewrapped",
		toolName:    "legacy_call",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})

	res, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From:         now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:           now.Add(1 * time.Hour).Format(time.RFC3339),
		McpServerIds: []string{liveID.String()},
		Limit:        10,
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Len(t, res.Traces, 1)
	require.Equal(t, liveID.String(), *res.Traces[0].McpServerID)
}
