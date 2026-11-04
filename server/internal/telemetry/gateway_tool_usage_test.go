package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/agentgateway"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
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
	seedGatewayTraffic(t, ctx, ti, ti.projectID, gateway.ID.String(), kept.mcpServerID.String(), removed.mcpServerID.String(), now)
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gateway.ID.String(), memberID: uuid.NewString(), tool: "ping", status: 200, at: now})

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
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gateway.ID.String(), memberID: member.mcpServerID.String(), sourceID: member.sourceID.String(), tool: "query", status: 200, at: now.Add(-4 * time.Minute)})
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

	all := requireToolUsageTraces(t, ctx, ti, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10}, func(result *gen.ListToolUsageTracesResult) bool {
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

// A member's calls are logged in the member's project, whichever gateway
// routed them, so a gateway in another project of the organization, an agent
// gateway, and the organization gateway are all named; an id no gateway of the
// organization derives reads as unknown rather than as the raw id. Each named
// gateway also says where its page is, so the dashboard can link to it.
func TestListToolUsageTraces_NamesGatewaysAcrossTheOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	now := time.Now().UTC()

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	otherGateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      ti.orgID,
		ProjectID:           otherProject.ID,
		Name:                "Other Project Gateway",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          visibility.Private,
	})
	require.NoError(t, err)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: ti.orgID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Support bot",
	})
	require.NoError(t, err)
	organization, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, ti.orgID)
	require.NoError(t, err)

	member := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Member", slug: "member-" + uuid.NewString()[:8]})
	want := map[string]gatewayRef{
		otherGateway.ID.String():                                        {name: "Other Project Gateway", kind: "project", projectSlug: otherProject.Slug},
		agentgateway.GatewayID(agent.ID).String():                       {name: "Support bot gateway", kind: "agent", agentID: agent.ID.String()},
		orggateway.GatewayID(ti.orgID).String():                         {name: organization.Name, kind: "organization"},
		orggateway.ProjectGatewayID(ti.orgID, otherProject.ID).String(): {name: "Other project", kind: "organization", projectSlug: otherProject.Slug},
		uuid.NewString():                                                {name: "Unknown gateway"},
	}
	for gatewayID := range want {
		logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: member.mcpServerID.String(), sourceID: member.sourceID.String(), tool: "query", status: 200, at: now.Add(-time.Minute)})
	}
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	result := requireToolUsageTraces(t, ctx, ti, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10}, func(result *gen.ListToolUsageTracesResult) bool {
		return len(result.Traces) == len(want)
	})
	got := map[string]gatewayRef{}
	for _, trace := range result.Traces {
		require.NotNil(t, trace.ViaMetaMcpServerID)
		require.NotNil(t, trace.ViaMetaMcpServerName)
		got[*trace.ViaMetaMcpServerID] = gatewayRef{
			name:        *trace.ViaMetaMcpServerName,
			kind:        conv.PtrValOr(trace.ViaMetaMcpServerKind, ""),
			projectSlug: conv.PtrValOr(trace.ViaMetaMcpServerProjectSlug, ""),
			agentID:     conv.PtrValOr(trace.ViaMetaMcpServerAgentID, ""),
		}
	}
	require.Equal(t, want, got)

	// The gateway filter offers every gateway that resolves, and not an id
	// a client could have stamped on its own.
	options, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{From: from, To: to, OptionTypes: []gen.ToolUsageFilterOptionType{"gateways"}})
	require.NoError(t, err)
	offered := map[string]string{}
	for _, option := range options.Gateways {
		offered[option.MetaMcpServerID] = option.Name
	}
	requireGatewaysOffered(t, options, want)
}

// A deleted gateway keeps its name, and so does the organization gateway of a
// deleted project, but neither has a page left to link to.
func TestListToolUsageTraces_NamesDeletedGatewaysWithoutLinks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()

	deletedGateway := createGateway(t, ctx, ti, "Retired Gateway")
	_, err := metamcprepo.New(ti.conn).DeleteMetaMCPServer(ctx, metamcprepo.DeleteMetaMCPServerParams{
		ID:             deletedGateway.ID,
		OrganizationID: ti.orgID,
		ProjectID:      uuid.MustParse(ti.projectID),
	})
	require.NoError(t, err)

	deletedProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Closed project", Slug: "closed-" + uuid.NewString()[:8], OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	gatewayInDeletedProject, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      ti.orgID,
		ProjectID:           deletedProject.ID,
		Name:                "Closed Project Gateway",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          visibility.Private,
	})
	require.NoError(t, err)
	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, deletedProject.ID)
	require.NoError(t, err)

	member := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Member", slug: "member-" + uuid.NewString()[:8]})
	want := map[string]gatewayRef{
		deletedGateway.ID.String():                                        {name: "Retired Gateway"},
		gatewayInDeletedProject.ID.String():                               {name: "Closed Project Gateway"},
		orggateway.ProjectGatewayID(ti.orgID, deletedProject.ID).String(): {name: "Closed project"},
	}
	result, options := listGatewayTraces(t, ctx, ti, member, want, now)
	require.Equal(t, want, result)
	requireGatewaysOffered(t, options, want)
}

// The telemetry endpoints check only the project being viewed, so a gateway
// in a project the caller may not read, or serving an agent they may not
// read, is neither named nor placed. The organization gateway is named after
// the organization they belong to, and an agent's owner sees its gateway
// without an agent grant.
func TestListToolUsageTraces_HidesGatewaysTheCallerCannotRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	now := time.Now().UTC()

	currentProject, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, uuid.MustParse(ti.projectID))
	require.NoError(t, err)
	ownGateway := createGateway(t, ctx, ti, "Own Gateway")
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Private project", Slug: "private-" + uuid.NewString()[:8], OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	otherGateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      ti.orgID,
		ProjectID:           otherProject.ID,
		Name:                "Private Project Gateway",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          visibility.Private,
	})
	require.NoError(t, err)
	hiddenAgent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: ti.orgID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "Hidden bot",
	})
	require.NoError(t, err)
	grantedAgent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: ti.orgID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "Granted bot",
	})
	require.NoError(t, err)
	organization, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, ti.orgID)
	require.NoError(t, err)

	// Only the viewed project and one agent are readable.
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeLogRead, ti.projectID),
		authz.NewGrant(authz.ScopeProjectRead, ti.projectID),
		authz.NewGrant(authz.ScopeAgentRead, grantedAgent.ID.String()),
	)
	// Someone other than the agents' owner is looking.
	viewer := *authCtx
	viewer.UserID = "viewer-" + uuid.NewString()[:8]
	viewerCtx := contextvalues.SetAuthContext(ctx, &viewer)

	member := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Member", slug: "member-" + uuid.NewString()[:8]})
	hidden := gatewayRef{name: "Gateway in another project"}
	want := map[string]gatewayRef{
		ownGateway.ID.String():   {name: "Own Gateway", kind: "project", projectSlug: currentProject.Slug},
		otherGateway.ID.String(): hidden,
		orggateway.ProjectGatewayID(ti.orgID, otherProject.ID).String(): hidden,
		orggateway.GatewayID(ti.orgID).String():                         {name: organization.Name, kind: "organization"},
		agentgateway.GatewayID(hiddenAgent.ID).String():                 hidden,
		agentgateway.GatewayID(grantedAgent.ID).String():                {name: "Granted bot gateway", kind: "agent", agentID: grantedAgent.ID.String()},
	}
	result, options := listGatewayTraces(t, viewerCtx, ti, member, want, now)
	require.Equal(t, want, result)
	requireGatewaysOffered(t, options, want)

	// The owner sees their agent's gateway with no agent grant.
	owner, _ := listGatewayTraces(t, ctx, ti, member, nil, now)
	require.Equal(t, gatewayRef{name: "Hidden bot gateway", kind: "agent", agentID: hiddenAgent.ID.String()}, owner[agentgateway.GatewayID(hiddenAgent.ID).String()])
}

// listGatewayTraces logs one dispatch through each gateway in want (none when
// want is nil) and returns what the traces and filter options say about them.
func listGatewayTraces(t *testing.T, ctx context.Context, ti *testInstance, member tunneledMCPServerFixture, want map[string]gatewayRef, now time.Time) (map[string]gatewayRef, *gen.GetToolUsageFilterOptionsResult) {
	t.Helper()

	for gatewayID := range want {
		logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: member.mcpServerID.String(), sourceID: member.sourceID.String(), tool: "query", status: 200, at: now.Add(-time.Minute)})
	}
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	result, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 50})
	require.NoError(t, err)
	got := map[string]gatewayRef{}
	for _, trace := range result.Traces {
		require.NotNil(t, trace.ViaMetaMcpServerID)
		require.NotNil(t, trace.ViaMetaMcpServerName)
		got[*trace.ViaMetaMcpServerID] = gatewayRef{
			name:        *trace.ViaMetaMcpServerName,
			kind:        conv.PtrValOr(trace.ViaMetaMcpServerKind, ""),
			projectSlug: conv.PtrValOr(trace.ViaMetaMcpServerProjectSlug, ""),
			agentID:     conv.PtrValOr(trace.ViaMetaMcpServerAgentID, ""),
		}
	}

	options, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{From: from, To: to, OptionTypes: []gen.ToolUsageFilterOptionType{"gateways"}})
	require.NoError(t, err)
	return got, options
}

// requireGatewaysOffered checks the gateway filter offers every gateway that
// resolves, under the name its traces carry, and not an id a client could
// have stamped on its own.
func requireGatewaysOffered(t *testing.T, options *gen.GetToolUsageFilterOptionsResult, want map[string]gatewayRef) {
	t.Helper()

	offered := map[string]string{}
	for _, option := range options.Gateways {
		offered[option.MetaMcpServerID] = option.Name
	}
	for gatewayID, gateway := range want {
		if gateway.name == "Unknown gateway" {
			require.NotContains(t, offered, gatewayID)
			continue
		}
		require.Equal(t, gateway.name, offered[gatewayID])
	}
}

// gatewayRef is what a trace says about the gateway that dispatched it.
type gatewayRef struct {
	name        string
	kind        string
	projectSlug string
	agentID     string
}

func TestGetToolUsageSummary_ClassifiesAndFiltersGateway(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	fixture := seedHookObservedGateway(t, ctx, ti, now)
	gatewayID := fixture.gateway.ID.String()
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	res, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{From: from, To: to, MetaMcpServerIds: []string{gatewayID}})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, int64(2), res.Totals.EventCount)
	targets := toolUsageTargetsByKey(res.Targets)
	observed := targets[repo.ToolUsageTargetTypeMetaMCP+":server:"+gatewayID]
	require.NotNil(t, observed)
	require.Equal(t, "Acme Gateway", observed.TargetLabel)
	require.Equal(t, int64(1), observed.EventCount)
	require.NotNil(t, targets["tunneled_mcp_server:server:"+fixture.memberSlug], "the dispatch stays attributed to the member")
	require.Nil(t, targets["shadow_mcp_server:server:shadow-db"])

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

// openClawGatewayCall is one OpenClaw tool call on an MCP server: a pre-call
// row, then a post-call row carrying the server name.
type openClawGatewayCall struct {
	conversationID string
	toolName       string
	serverName     string
	url            string
	// preServerName is the server name on the pre-call row. A relay that
	// resolves the server from the tool name sends it, an older one sends none.
	preServerName string
	// preURLOnly leaves the URL and the server name off the post-call row, as
	// for a call blocked before it ran, whose result names no server.
	preURLOnly bool
}

func insertOpenClawGatewayCall(t *testing.T, ctx context.Context, ti *testInstance, at time.Time, call openClawGatewayCall) {
	t.Helper()
	traceID := uuid.New().String()
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      ti.projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      at,
		traceID:        traceID,
		userEmail:      "alice@example.com",
		hookSource:     "openclaw",
		toolSource:     call.preServerName,
		toolName:       call.toolName,
		mcpMatch:       call.url,
		mcpServerURL:   call.url,
		conversationID: call.conversationID,
	})
	postURL, postServerName := call.url, call.serverName
	if call.preURLOnly {
		postURL, postServerName = "", ""
	}
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      ti.projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      at.Add(time.Second),
		traceID:        traceID,
		userEmail:      "alice@example.com",
		hookSource:     "openclaw",
		toolSource:     postServerName,
		toolName:       call.toolName,
		result:         `"ok"`,
		mcpMatch:       postURL,
		mcpServerURL:   postURL,
		conversationID: call.conversationID,
	})
}

// OpenClaw names every MCP server by its config key, so an agent gateway
// configured as "speakeasy" looks like any third-party server of that name.
// The URL the call reached decides: an agent, organization or project gateway
// URL classifies the call as that gateway, labelled as LoadGatewayLabels names
// it, on the trace_summaries path and the raw-log path a query forces, even
// when the pre-call row carries the URL but no server name. A third-party
// server named "speakeasy" at another URL stays a shadow server. Its pre-call
// row names the server, as the relay that resolves servers from tool names
// sends it: trace_summaries keeps any() of tool_source, so a pre-call row
// without the name can still leave the summary path without a source.
func TestListToolUsageTraces_ClassifiesOpenClawGatewayCallsByURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	now := time.Now().UTC()

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: ti.orgID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Support bot",
	})
	require.NoError(t, err)
	organization, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, ti.orgID)
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, uuid.MustParse(ti.projectID))
	require.NoError(t, err)

	agentGatewayID := agentgateway.GatewayID(agent.ID).String()
	agentGatewayURL := "https://app.example.com/agent-mcp/" + agent.ID.String()
	orgGatewayURL := "https://app.example.com/mcp/gateway/" + organization.Slug
	calls := []struct {
		call       openClawGatewayCall
		targetType string
		targetID   string
		label      string
	}{
		{
			call:       openClawGatewayCall{conversationID: "conv-agent-ok", toolName: "speakeasy__list_servers", serverName: "speakeasy", url: agentGatewayURL, preServerName: "", preURLOnly: false},
			targetType: repo.ToolUsageTargetTypeMetaMCP, targetID: agentGatewayID, label: "Support bot gateway",
		},
		{
			call:       openClawGatewayCall{conversationID: "conv-agent-blocked", toolName: "speakeasy__delete_server", serverName: "speakeasy", url: agentGatewayURL, preServerName: "", preURLOnly: true},
			targetType: repo.ToolUsageTargetTypeMetaMCP, targetID: agentGatewayID, label: "Support bot gateway",
		},
		{
			call:       openClawGatewayCall{conversationID: "conv-org", toolName: "org__search", serverName: "org", url: orgGatewayURL, preServerName: "", preURLOnly: false},
			targetType: repo.ToolUsageTargetTypeMetaMCP, targetID: orggateway.GatewayID(ti.orgID).String(), label: organization.Name,
		},
		{
			call:       openClawGatewayCall{conversationID: "conv-project", toolName: "proj__search", serverName: "proj", url: orgGatewayURL + "/projects/" + project.Slug, preServerName: "", preURLOnly: false},
			targetType: repo.ToolUsageTargetTypeMetaMCP, targetID: orggateway.ProjectGatewayID(ti.orgID, project.ID).String(), label: project.Name,
		},
		{
			call:       openClawGatewayCall{conversationID: "conv-third-party", toolName: "speakeasy__lookup", serverName: "speakeasy", url: "https://mcp.third-party.example/speakeasy", preServerName: "speakeasy", preURLOnly: false},
			targetType: repo.ToolUsageTargetTypeShadowMCP, targetID: "speakeasy", label: "speakeasy",
		},
	}
	for i, tc := range calls {
		insertOpenClawGatewayCall(t, ctx, ti, now.Add(-time.Duration(len(calls)-i)*time.Minute), tc.call)
	}
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	all := requireToolUsageTraces(t, ctx, ti, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 20}, func(result *gen.ListToolUsageTracesResult) bool {
		return len(result.Traces) == len(calls)
	})
	byTool := map[string]*gen.ToolUsageTraceSummary{}
	for _, trace := range all.Traces {
		byTool[trace.ToolName] = trace
	}
	for _, tc := range calls {
		summary := byTool[tc.call.toolName]
		require.NotNil(t, summary, "summary path: %s is missing: %v", tc.call.toolName, byTool)
		require.Equal(t, gen.ToolUsageTargetType(tc.targetType), summary.TargetType, "summary path: %s", tc.call.toolName)
		require.Equal(t, tc.targetID, summary.TargetID, "summary path: %s", tc.call.toolName)
		require.Equal(t, tc.label, summary.TargetLabel, "summary path: %s", tc.call.toolName)
		require.Nil(t, summary.ViaMetaMcpServerID, "a call on a gateway is not routed through one")

		query := tc.call.conversationID
		raw, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10, Query: &query})
		require.NoError(t, err)
		require.Len(t, raw.Traces, 1, "raw path: %s must not split across targets", tc.call.toolName)
		require.Equal(t, gen.ToolUsageTargetType(tc.targetType), raw.Traces[0].TargetType, "raw path: %s", tc.call.toolName)
		require.Equal(t, tc.targetID, raw.Traces[0].TargetID, "raw path: %s", tc.call.toolName)
		require.Equal(t, tc.label, raw.Traces[0].TargetLabel, "raw path: %s", tc.call.toolName)
	}

	filtered, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10, MetaMcpServerIds: []string{agentGatewayID}})
	require.NoError(t, err)
	require.Len(t, filtered.Traces, 2, "the agent gateway filter covers both agent gateway calls")
}
