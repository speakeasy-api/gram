package agent_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/installmode"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedToolsetServer adds a toolset-backed MCP server with the given MCP slug
// to a plugin and returns the toolset and the URL its package, and so the
// agent poll, addresses it by. Each test has its own database, so fixed slugs
// do not collide across tests.
func seedToolsetServer(t *testing.T, ti *testInstance, pluginID uuid.UUID, slug, displayName string) (uuid.UUID, string) {
	t.Helper()
	return seedProjectToolsetServer(t, ti, ti.projectID, pluginID, slug, displayName)
}

// seedProjectToolsetServer is seedToolsetServer for a toolset in another
// project of the same org.
func seedProjectToolsetServer(t *testing.T, ti *testInstance, projectID, pluginID uuid.UUID, slug, displayName string) (uuid.UUID, string) {
	t.Helper()
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID:         ti.orgID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{Valid: false},
	})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: projectID, Name: pgtype.Text{String: slug, Valid: true}, Slug: pgtype.Text{String: slug, Valid: true},
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	testenv.SetHostedMCP(t, t.Context(), ti.conn, projectID, toolset.ID, testenv.HostedMCP{Slug: slug})
	addToolsetServer(t, ti, pluginID, toolset.ID, displayName)
	return toolset.ID, testServerURL + "/mcp/" + slug
}

func addToolsetServer(t *testing.T, ti *testInstance, pluginID, toolsetID uuid.UUID, displayName string) {
	t.Helper()
	_, err := pluginsrepo.New(ti.conn).AddPluginServer(t.Context(), pluginsrepo.AddPluginServerParams{
		PluginID:    pluginID,
		McpServerID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		DisplayName: displayName,
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
}

func mcpServersByName(res *gen.GetPluginsResult) map[string]string {
	out := make(map[string]string, len(res.McpServers))
	for _, s := range res.McpServers {
		out[s.Name] = s.URL
	}
	return out
}

func TestGetPlugins_AgentKeyListsAssignedPluginServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, actor := withAgentKeyAuth(t, ctx, ti, "CI agent")

	agentTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "agent-tool")
	assignPlugin(t, ctx, ti.conn, agentTool, ti.orgID, actor.String())
	_, linearURL := seedToolsetServer(t, ti, agentTool, "team-linear", "Team Linear")

	wildcardTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "wildcard-tool")
	assignPlugin(t, ctx, ti.conn, wildcardTool, ti.orgID, "*")
	_, githubURL := seedToolsetServer(t, ti, wildcardTool, "GitHub_Enterprise", "GitHub")

	humanTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "human-tool")
	assignPlugin(t, ctx, ti.conn, humanTool, ti.orgID, "email:"+mockidp.MockUserEmail)
	seedToolsetServer(t, ti, humanTool, "slack", "Slack")

	res, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Equal(t, map[string]string{"speakeasy-team-linear": linearURL, "speakeasy-github-enterprise": githubURL}, mcpServersByName(res),
		"servers come from the agent's and the org wildcard's plugins, named speakeasy-<slug>")
	for _, s := range res.McpServers {
		require.Empty(t, s.Tools, "plugin servers apply to every managed tool")
	}
}

func TestGetPlugins_HumanPollSendsEmptyMCPServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	tool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "tool")
	assignPlugin(t, ctx, ti.conn, tool, ti.orgID, "*")
	seedToolsetServer(t, ti, tool, "github", "GitHub")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.NotNil(t, res.McpServers, "people get an explicit empty list, never an absent one")
	require.Empty(t, res.McpServers, "people reach plugin servers through the plugin itself")
}

func TestGetPlugins_AgentWithoutServersSendsEmptyList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, _ := withAgentKeyAuth(t, ctx, ti, "CI agent")

	res, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.NotNil(t, res.McpServers, "an empty list tells the device to remove entries it wrote earlier")
	require.Empty(t, res.McpServers)
}

func TestGetPlugins_AgentSkipsAvailablePluginServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, actor := withAgentKeyAuth(t, ctx, ti, "CI agent")

	offered := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "offered")
	assignPluginWithMode(t, ti, offered, actor.String(), installmode.Available)
	seedToolsetServer(t, ti, offered, "offered", "Offered")

	required := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "required")
	assignPluginWithMode(t, ti, required, actor.String(), installmode.Required)
	_, requiredURL := seedToolsetServer(t, ti, required, "required", "Required")

	res, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"speakeasy-required": requiredURL}, mcpServersByName(res),
		"nobody on an agent's machine can turn on an available plugin")
}

func TestGetPlugins_AgentListsSharedServerOnceAndSuffixesNameCollisions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, actor := withAgentKeyAuth(t, ctx, ti, "CI agent")

	// Both slugs reduce to speakeasy-dup-linear. Suffixes follow URL order, so
	// /mcp/dup-linear keeps the bare name whichever plugin lists it.
	first := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "a-first")
	assignPlugin(t, ctx, ti.conn, first, ti.orgID, actor.String())
	sharedToolset, underscoreURL := seedToolsetServer(t, ti, first, "dup_linear", "Linear")

	second := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "b-second")
	assignPlugin(t, ctx, ti.conn, second, ti.orgID, actor.String())
	_, dashURL := seedToolsetServer(t, ti, second, "dup-linear", "Linear")

	// The same toolset in a third plugin is one server, not two entries.
	third := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "c-third")
	assignPlugin(t, ctx, ti.conn, third, ti.orgID, actor.String())
	addToolsetServer(t, ti, third, sharedToolset, "Linear (shared)")

	res, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Len(t, res.McpServers, 2)
	require.Equal(t, map[string]string{"speakeasy-dup-linear": dashURL, "speakeasy-dup-linear-2": underscoreURL}, mcpServersByName(res))
}

func TestGetPlugins_AgentSuffixNeverTakesAnotherServersNaturalName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, actor := withAgentKeyAuth(t, ctx, ti, "CI agent")
	tool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "tool")
	assignPlugin(t, ctx, ti.conn, tool, ti.orgID, actor.String())

	// speakeasy-linear-2 is this server's own name, not a suffix.
	_, ownURL := seedToolsetServer(t, ti, tool, "linear-2", "Linear 2")
	_, dashURL := seedToolsetServer(t, ti, tool, "linear", "Linear")
	before, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"speakeasy-linear": dashURL, "speakeasy-linear-2": ownURL}, mcpServersByName(before))

	// A second server that reduces to speakeasy-linear skips the reserved -2.
	_, underscoreURL := seedToolsetServer(t, ti, tool, "linear_", "Linear (eu)")
	after, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Len(t, after.McpServers, 3)
	byName := mcpServersByName(after)
	require.Equal(t, ownURL, byName["speakeasy-linear-2"])
	require.Contains(t, []string{dashURL, underscoreURL}, byName["speakeasy-linear"])
	require.Contains(t, []string{dashURL, underscoreURL}, byName["speakeasy-linear-3"])
}

func TestGetPlugins_AgentMCPServerNamesStayStable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, actor := withAgentKeyAuth(t, ctx, ti, "CI agent")

	tool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "m-tool")
	assignPlugin(t, ctx, ti.conn, tool, ti.orgID, actor.String())
	_, linearURL := seedToolsetServer(t, ti, tool, "linear", "Linear")

	before, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"speakeasy-linear": linearURL}, mcpServersByName(before))

	// A plugin that sorts first and shares the display name must not take the
	// existing server's name: names come from slugs, suffixes from name and URL.
	earlier := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "a-earlier")
	assignPlugin(t, ctx, ti.conn, earlier, ti.orgID, actor.String())
	_, otherURL := seedToolsetServer(t, ti, earlier, "other-linear", "Linear")

	after, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"speakeasy-linear": linearURL, "speakeasy-other-linear": otherURL}, mcpServersByName(after))
}

func TestGetPlugins_AgentMCPServerChangesChangeETag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, actor := withAgentKeyAuth(t, ctx, ti, "CI agent")
	tool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "tool")
	seedToolsetServer(t, ti, tool, "github", "GitHub")

	unassigned, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)

	assignPlugin(t, ctx, ti.conn, tool, ti.orgID, actor.String())
	assigned, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Len(t, assigned.McpServers, 1)
	require.NotEqual(t, unassigned.Etag, assigned.Etag, "an assignment change reaches devices on their next poll")

	seedToolsetServer(t, ti, tool, "slack", "Slack")
	withServer, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Len(t, withServer.McpServers, 2)
	require.NotEqual(t, assigned.Etag, withServer.Etag)

	again, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Equal(t, withServer.Etag, again.Etag, "an unchanged policy keeps its ETag")
}

func TestGetPlugins_AgentSkipsServersOfCollapsedMarketplaces(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "default-token")
	agentCtx, actor := withAgentKeyAuth(t, ctx, ti, "CI agent")

	// beta's name collides with the default project's, so its marketplace is
	// not served and its plugin is dropped: its server must go with it.
	beta := seedProject(t, ctx, ti.conn, ti.orgID, "beta")
	setMarketplaceOverride(t, ctx, ti.conn, beta, wantMarketplace)
	publishMarketplace(t, ctx, ti.conn, beta, "beta-token")
	betaTool := seedPlugin(t, ctx, ti.conn, ti.orgID, beta, "beta-only-tool")
	assignPlugin(t, ctx, ti.conn, betaTool, ti.orgID, actor.String())
	seedProjectToolsetServer(t, ti, beta, betaTool, "beta-linear", "Linear")

	res, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.NotContains(t, pluginSlugs(res), "beta-only-tool")
	require.Empty(t, res.McpServers, "a server is listed only when its plugin is")
}

// fakeWorkloadTokenTargets resolves token targets from a fixed map of URLs.
type fakeWorkloadTokenTargets map[string]mcp.WorkloadTokenTarget

func (f fakeWorkloadTokenTargets) WorkloadTokenTargetFor(_ context.Context, mcpURL string) (mcp.WorkloadTokenTarget, bool, error) {
	target, ok := f[mcpURL]
	return target, ok, nil
}

// Each entry carries the token endpoint, audience, and resource a workload
// uses to reach it; a server no workload grant reaches is still listed, with
// none. A changed target changes the ETag.
func TestGetPlugins_AgentMCPServersCarryWorkloadTokenTargets(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, actor := withAgentKeyAuth(t, ctx, ti, "CI agent")
	tool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "agent-tool")
	assignPlugin(t, ctx, ti.conn, tool, ti.orgID, actor.String())
	_, sharedURL := seedToolsetServer(t, ti, tool, "shared-linear", "Linear")
	_, perEndpointURL := seedToolsetServer(t, ti, tool, "own-github", "GitHub")
	_, headlessUnreachableURL := seedToolsetServer(t, ti, tool, "no-grant", "No grant")

	targets := fakeWorkloadTokenTargets{
		sharedURL:      {TokenEndpoint: testServerURL + "/oauth/usi/1/token", Audience: testServerURL + "/oauth/usi/1/token", Resource: ""},
		perEndpointURL: {TokenEndpoint: perEndpointURL + "/token", Audience: perEndpointURL + "/token", Resource: perEndpointURL},
	}
	ti.service.WithWorkloadTokenTargets(targets)

	res, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	byURL := make(map[string]*gen.AgentMCPServer, len(res.McpServers))
	for _, s := range res.McpServers {
		byURL[s.URL] = s
	}
	require.Len(t, byURL, 3, "a server no workload grant reaches is still listed")

	shared := byURL[sharedURL]
	require.Equal(t, new(testServerURL+"/oauth/usi/1/token"), shared.TokenEndpoint)
	require.Equal(t, shared.TokenEndpoint, shared.Audience)
	require.Nil(t, shared.Resource, "the issuer-wide grant names no resource")

	own := byURL[perEndpointURL]
	require.Equal(t, new(perEndpointURL+"/token"), own.TokenEndpoint)
	require.Equal(t, new(perEndpointURL), own.Resource)

	none := byURL[headlessUnreachableURL]
	require.Nil(t, none.TokenEndpoint)
	require.Nil(t, none.Audience)
	require.Nil(t, none.Resource)

	targets[perEndpointURL] = mcp.WorkloadTokenTarget{TokenEndpoint: testServerURL + "/oauth/usi/2/token", Audience: testServerURL + "/oauth/usi/2/token", Resource: ""}
	moved, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.NotEqual(t, res.Etag, moved.Etag, "a server moving to another authorization server reaches devices on the next poll")
}
