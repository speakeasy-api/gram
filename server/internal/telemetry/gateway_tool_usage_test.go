package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func createGateway(t *testing.T, ctx context.Context, ti *testInstance, name string) metamcprepo.MetaMcpServer {
	t.Helper()
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      ti.orgID,
		ProjectID:           uuid.MustParse(ti.projectID),
		Name:                name,
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          visibility.Private,
	})
	require.NoError(t, err)
	return gateway
}

func addGatewayMember(t *testing.T, ctx context.Context, ti *testInstance, gatewayID, mcpServerID uuid.UUID, sortOrder int32) metamcprepo.MetaMcpServerMember {
	t.Helper()
	member, err := metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       uuid.MustParse(ti.projectID),
		MetaMcpServerID: gatewayID,
		McpServerID:     mcpServerID,
		SortOrder:       sortOrder,
	})
	require.NoError(t, err)
	return member
}

func TestGetMetaMcpServerUsage_ExcludesRemovedMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	gateway := createGateway(t, ctx, ti, "Members Gateway")
	kept := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Kept", slug: "kept-" + uuid.NewString()[:8]})
	removed := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Removed", slug: "removed-" + uuid.NewString()[:8]})
	addGatewayMember(t, ctx, ti, gateway.ID, kept.mcpServerID, 0)
	removedMember := addGatewayMember(t, ctx, ti, gateway.ID, removed.mcpServerID, 1)
	_, err := metamcprepo.New(ti.conn).DeleteMetaMCPMember(ctx, metamcprepo.DeleteMetaMCPMemberParams{
		ID:        removedMember.ID,
		ProjectID: uuid.MustParse(ti.projectID),
	})
	require.NoError(t, err)

	// Traffic reached both members while the removed one was still attached;
	// a member that never existed on the gateway must not surface either.
	seedGatewayTraffic(ctx, ti, ti.projectID, gateway.ID.String(), kept.mcpServerID.String(), removed.mcpServerID.String(), now)
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gateway.ID.String(), memberID: uuid.NewString(), tool: "ping", status: 200, at: now})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	result, err := ti.service.GetMetaMcpServerUsage(ctx, &gen.GetMetaMcpServerUsagePayload{
		MetaMcpServerID: gateway.ID.String(),
		From:            now.Add(-time.Hour).Format(time.RFC3339),
		To:              now.Add(time.Hour).Format(time.RFC3339),
	})
	require.NoError(t, err)

	require.Equal(t, int64(4), result.Funnel.ExecuteTool, "the funnel still counts every dispatch")
	require.Len(t, result.Members, 1, "only members still on the gateway are listed")
	require.Equal(t, kept.mcpServerID.String(), result.Members[0].McpServerID)
	require.Equal(t, int64(2), result.Members[0].ToolCalls)
}

func TestGetMetaMcpServerUsage_RejectsMalformedGatewayID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	_, err := ti.service.GetMetaMcpServerUsage(ctx, &gen.GetMetaMcpServerUsagePayload{
		MetaMcpServerID: "not-a-uuid",
		From:            now.Add(-time.Hour).Format(time.RFC3339),
		To:              now.Add(time.Hour).Format(time.RFC3339),
	})
	require.Error(t, err)
}

// gatewayTrafficFixture is one gateway with an endpoint, one tunneled member,
// a hook-observed execute_tool call against the gateway URL, one dispatch to
// the member through the gateway, and an unrelated shadow call.
type gatewayTrafficFixture struct {
	gateway    metamcprepo.MetaMcpServer
	member     tunneledMCPServerFixture
	memberSlug string
}

func seedHookObservedGateway(t *testing.T, ctx context.Context, ti *testInstance, now time.Time) gatewayTrafficFixture {
	t.Helper()

	gateway := createGateway(t, ctx, ti, "Acme Gateway")
	slug := "acme-gateway-" + uuid.NewString()[:8]
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       uuid.MustParse(ti.projectID),
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MetaMcpServerID: uuid.NullUUID{UUID: gateway.ID, Valid: true},
		Slug:            slug,
	})
	require.NoError(t, err)
	memberSlug := "acme-member-" + uuid.NewString()[:8]
	member := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Acme Member", slug: memberSlug})
	addGatewayMember(t, ctx, ti, gateway.ID, member.mcpServerID, 0)

	// What the Claude Code hooks record for a call on the gateway: the
	// configured server name as the source and the endpoint URL as the match.
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      ti.projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-5 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "alice@example.com",
		hookSource:     "claude-code",
		toolSource:     slug,
		toolName:       "execute_tool",
		result:         `"ok"`,
		mcpMatch:       "https://app.example.com/mcp/" + slug,
		mcpServerURL:   "https://app.example.com/mcp/" + slug,
		conversationID: "conv-gateway",
	})
	// The dispatch the gateway made for that call, attributed to the member.
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gateway.ID.String(), memberID: member.mcpServerID.String(), sourceID: member.sourceID.String(), tool: "query", status: 200, at: now.Add(-4 * time.Minute)})
	// Unrelated shadow traffic that no gateway filter may include.
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      ti.projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-3 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "bob@example.com",
		hookSource:     "claude-code",
		toolSource:     "shadow-db",
		toolName:       "query",
		result:         `"ok"`,
		conversationID: "conv-shadow",
	})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	return gatewayTrafficFixture{gateway: gateway, member: member, memberSlug: memberSlug}
}

func TestListToolUsageTraces_ClassifiesHookObservedGateway(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	fixture := seedHookObservedGateway(t, ctx, ti, now)
	gatewayID := fixture.gateway.ID.String()
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	all := waitForToolUsageTraces(t, ctx, ti, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10}, func(result *gen.ListToolUsageTracesResult) bool {
		return len(result.Traces) == 3
	})
	byTarget := map[string]*gen.ToolUsageTraceSummary{}
	for _, trace := range all.Traces {
		byTarget[string(trace.TargetType)+":"+trace.TargetID] = trace
	}
	observed := byTarget[repo.ToolUsageTargetTypeMetaMCP+":"+gatewayID]
	require.NotNil(t, observed, "the hook-observed call classifies as the gateway, not shadow: %v", byTarget)
	require.Equal(t, "Acme Gateway", observed.TargetLabel)
	require.Equal(t, "execute_tool", observed.ToolName)
	require.NotNil(t, byTarget["tunneled_mcp_server:"+fixture.memberSlug], "the dispatch stays attributed to the member")
	require.NotNil(t, byTarget["shadow_mcp_server:shadow-db"])

	// The gateway filter covers the observed call and the dispatch, on both
	// the trace_summaries path and the raw-log path a query forces.
	filtered, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10, MetaMcpServerIds: []string{gatewayID}})
	require.NoError(t, err)
	require.Len(t, filtered.Traces, 2)
	for _, trace := range filtered.Traces {
		require.NotEqual(t, "shadow-db", trace.TargetID)
		if trace.TargetType == gen.ToolUsageTargetType(repo.ToolUsageTargetTypeMetaMCP) {
			require.Nil(t, trace.ViaMetaMcpServerID, "a call on the gateway itself is not routed through one")
			continue
		}
		require.NotNil(t, trace.ViaMetaMcpServerID, "the dispatch names the gateway that routed it")
		require.Equal(t, gatewayID, *trace.ViaMetaMcpServerID)
		require.Equal(t, "Acme Gateway", *trace.ViaMetaMcpServerName)
	}
	require.Nil(t, byTarget["shadow_mcp_server:shadow-db"].ViaMetaMcpServerID)

	query := "conv-gateway"
	raw, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10, Query: &query})
	require.NoError(t, err)
	require.Len(t, raw.Traces, 1)
	require.Equal(t, gen.ToolUsageTargetType(repo.ToolUsageTargetTypeMetaMCP), raw.Traces[0].TargetType)
	require.Equal(t, gatewayID, raw.Traces[0].TargetID)
	require.Equal(t, "Acme Gateway", raw.Traces[0].TargetLabel)

	rawFiltered, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10, Query: &query, MetaMcpServerIds: []string{uuid.NewString()}})
	require.NoError(t, err)
	require.Empty(t, rawFiltered.Traces)
}

func TestGetToolUsageSummary_ClassifiesAndFiltersGateway(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	fixture := seedHookObservedGateway(t, ctx, ti, now)
	gatewayID := fixture.gateway.ID.String()
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{From: from, To: to, MetaMcpServerIds: []string{gatewayID}})
		if !assert.NoError(c, err) || !assert.NotNil(c, res) {
			return
		}
		if !assert.Equal(c, int64(2), res.Totals.EventCount) {
			return
		}
		targets := toolUsageTargetsByKey(res.Targets)
		observed := targets[repo.ToolUsageTargetTypeMetaMCP+":server:"+gatewayID]
		if assert.NotNil(c, observed) {
			assert.Equal(c, "Acme Gateway", observed.TargetLabel)
			assert.Equal(c, int64(1), observed.EventCount)
		}
		assert.NotNil(c, targets["tunneled_mcp_server:server:"+fixture.memberSlug], "the dispatch stays attributed to the member")
		assert.Nil(c, targets["shadow_mcp_server:server:shadow-db"])
	}, 10*time.Second, 200*time.Millisecond)

	options, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{From: from, To: to})
	require.NoError(t, err)
	require.Len(t, options.Gateways, 1)
	require.Equal(t, gatewayID, options.Gateways[0].MetaMcpServerID)
	require.Equal(t, "Acme Gateway", options.Gateways[0].Name)
	require.Equal(t, int64(2), options.Gateways[0].EventCount)
	require.Len(t, options.ShadowServers, 1, "the gateway no longer appears as a shadow server")
	require.Equal(t, "shadow-db", options.ShadowServers[0].ServerName)

	gatewaysOnly, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{From: from, To: to, OptionTypes: []gen.ToolUsageFilterOptionType{"gateways"}})
	require.NoError(t, err)
	require.Len(t, gatewaysOnly.Gateways, 1)
	require.Empty(t, gatewaysOnly.ShadowServers)
	require.Empty(t, gatewaysOnly.HostedServers)
	require.Empty(t, gatewaysOnly.Users)
}
