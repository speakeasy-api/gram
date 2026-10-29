package mcpservers_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const summaryServerURL = "https://app.example.com"

func withSummaryServerURL(t *testing.T, ti *testInstance) {
	t.Helper()
	serverURL, err := url.Parse(summaryServerURL)
	require.NoError(t, err)
	ti.service.WithServerURL(serverURL)
}

func summaryAuth(t *testing.T, ctx context.Context) (string, uuid.UUID) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return authCtx.ActiveOrganizationID, *authCtx.ProjectID
}

// seedSummaryToolset creates a toolset with one version per URN list, in order.
func seedSummaryToolset(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID uuid.UUID, versions ...[]urn.Tool) toolsetsrepo.Toolset {
	t.Helper()
	toolsets := toolsetsrepo.New(ti.conn)
	slug := "summary-" + uuid.NewString()[:8]
	toolset, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: organizationID, ProjectID: projectID, Name: slug, Slug: slug,
	})
	require.NoError(t, err)
	predecessor := uuid.NullUUID{}
	for i, urns := range versions {
		version, err := toolsets.CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
			ToolsetID: toolset.ID, Version: int64(i + 1), ToolUrns: urns, ResourceUrns: []urn.Resource{}, PredecessorID: predecessor,
		})
		require.NoError(t, err)
		predecessor = uuid.NullUUID{UUID: version.ID, Valid: true}
	}
	return toolset
}

func seedSummaryServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, toolsetID uuid.UUID) mcpserversrepo.McpServer {
	t.Helper()
	slug := "summary-server-" + uuid.NewString()[:8]
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	return server
}

func seedSummaryRemoteServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) mcpserversrepo.McpServer {
	t.Helper()
	slug := "summary-remote-" + uuid.NewString()[:8]
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
		RemoteMcpServerID: uuid.NullUUID{UUID: seedRemoteMcpServer(t, ctx, ti.conn, projectID), Valid: true},
		Visibility:        "private",
	})
	require.NoError(t, err)
	return server
}

func seedSummaryEndpoint(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, serverID uuid.UUID, customDomainID uuid.NullUUID) mcpendpointsrepo.McpEndpoint {
	t.Helper()
	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, CustomDomainID: customDomainID,
		McpServerID: uuid.NullUUID{UUID: serverID, Valid: true}, Slug: "summary-ep-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	return endpoint
}

func seedSummaryCustomDomain(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, serving bool) customdomainsrepo.CustomDomain {
	t.Helper()
	domains := customdomainsrepo.New(ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: organizationID, Domain: "summary-" + uuid.NewString()[:8] + ".example.com", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	if serving {
		domain, err = domains.UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
			ID: domain.ID, Verified: true, Activated: true, ProvisionerKind: domain.ProvisionerKind,
		})
		require.NoError(t, err)
	}
	return domain
}

func seedSummaryProject(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) uuid.UUID {
	t.Helper()
	slug := "summary-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: slug, Slug: slug, OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return project.ID
}

func getSummaryServer(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) *types.McpServer {
	t.Helper()
	got, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ID: conv.PtrEmpty(id.String())})
	require.NoError(t, err)
	return got
}

func listSummaryServers(t *testing.T, ctx context.Context, ti *testInstance, forOrg bool) map[string]*types.McpServer {
	t.Helper()
	var servers []*types.McpServer
	if forOrg {
		result, err := ti.service.ListMcpServersForOrg(ctx, &gen.ListMcpServersForOrgPayload{})
		require.NoError(t, err)
		servers = result.McpServers
	} else {
		result, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{})
		require.NoError(t, err)
		servers = result.McpServers
	}
	byID := make(map[string]*types.McpServer, len(servers))
	for _, server := range servers {
		byID[server.ID] = server
	}
	return byID
}

func platformURL(slug string) *string {
	return conv.PtrEmpty(summaryServerURL + "/mcp/" + slug)
}

func requireNoEnrichment(t *testing.T, got *types.McpServer) {
	t.Helper()
	require.Nil(t, got.Toolset)
	require.Nil(t, got.PrimaryEndpointID)
	require.Nil(t, got.PrimaryEndpointURL)
	require.Nil(t, got.PlatformEndpointSlug)
	require.False(t, got.HasEndpoint != nil && *got.HasEndpoint, "a foreign tenant's endpoint never counts")
}

func TestMcpServerSummary_HostedAcrossReads(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withSummaryServerURL(t, ti)
	orgID, projectID := summaryAuth(t, ctx)

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID,
		[]urn.Tool{urn.NewTool(urn.ToolKindHTTP, "stale", "old")},
		[]urn.Tool{
			urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets"),
			urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets"),
			urn.NewTool(urn.ToolKindHTTP, "petstore", "get_pet"),
			urn.NewTool(urn.ToolKindExternalMCP, "zeta", "proxy"),
			urn.NewTool(urn.ToolKindExternalMCP, "acme", "search"),
			urn.NewTool(urn.ToolKindExternalMCP, "acme", "proxy"),
		},
	)
	deployment := seedSummaryDeployment(t, ctx, ti, orgID, projectID)
	seedSummaryHTTPTool(t, ctx, ti, projectID, deployment, urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets"), "list_pets")
	seedSummaryHTTPTool(t, ctx, ti, projectID, deployment, urn.NewTool(urn.ToolKindHTTP, "petstore", "get_pet"), "get_pet")
	seedSummaryExternalTools(t, ctx, ti, deployment, "acme", urn.NewTool(urn.ToolKindExternalMCP, "acme", "search"), urn.NewTool(urn.ToolKindExternalMCP, "acme", "proxy"))
	seedSummaryExternalTools(t, ctx, ti, deployment, "zeta", urn.NewTool(urn.ToolKindExternalMCP, "zeta", "proxy"))
	_, err := toolsetsrepo.New(ti.conn).CreateToolsetOrigin(ctx, toolsetsrepo.CreateToolsetOriginParams{
		OrganizationID: orgID, ToolsetID: toolset.ID, RegistrySpecifier: "io.example/acme",
	})
	require.NoError(t, err)
	hosted := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)
	remote := seedSummaryRemoteServer(t, ctx, ti, projectID)
	older := seedSummaryEndpoint(t, ctx, ti, projectID, hosted.ID, uuid.NullUUID{})
	seedSummaryEndpoint(t, ctx, ti, projectID, hosted.ID, uuid.NullUUID{})

	requireHosted := func(t *testing.T, got *types.McpServer) {
		t.Helper()
		require.NotNil(t, got.Toolset)
		require.Equal(t, toolset.ID.String(), got.Toolset.ID)
		require.Equal(t, toolset.Slug, got.Toolset.Slug)
		require.Equal(t, toolset.Name, got.Toolset.Name)
		require.Equal(t, 3, got.Toolset.ToolCount, "latest version, live, distinct, proxies excluded")
		require.Equal(t, toolset.ToolSelectionMode, got.Toolset.ToolSelectionMode)
		require.Equal(t, []string{"acme", "zeta"}, got.Toolset.ExternalMcpSlugs)
		require.False(t, got.Toolset.ExternalMcpProxyOnly)
		require.Nil(t, got.Toolset.ExternalOauthServerID)
		require.Equal(t, conv.PtrEmpty("io.example/acme"), got.Toolset.RegistrySpecifier)
		require.Equal(t, conv.PtrEmpty(older.ID.String()), got.PrimaryEndpointID)
		require.Equal(t, platformURL(older.Slug), got.PrimaryEndpointURL)
		require.Equal(t, conv.PtrEmpty(older.Slug), got.PlatformEndpointSlug)
	}

	requireHosted(t, getSummaryServer(t, ctx, ti, hosted.ID))
	got, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ToolsetID: conv.PtrEmpty(toolset.ID.String())})
	require.NoError(t, err)
	requireHosted(t, got)
	requireNoEnrichment(t, getSummaryServer(t, ctx, ti, remote.ID))

	for _, forOrg := range []bool{false, true} {
		byID := listSummaryServers(t, ctx, ti, forOrg)
		requireHosted(t, byID[hosted.ID.String()])
		requireNoEnrichment(t, byID[remote.ID.String()])
	}
}

func TestMcpServerSummary_ProxyOnlyAndEmptyToolsets(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgID, projectID := summaryAuth(t, ctx)

	proxyOnly := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{urn.NewTool(urn.ToolKindExternalMCP, "acme", "proxy")})
	empty := seedSummaryToolset(t, ctx, ti, orgID, projectID)
	proxyServer := seedSummaryServer(t, ctx, ti, projectID, proxyOnly.ID)
	emptyServer := seedSummaryServer(t, ctx, ti, projectID, empty.ID)

	got := getSummaryServer(t, ctx, ti, proxyServer.ID)
	require.NotNil(t, got.Toolset)
	require.Equal(t, 0, got.Toolset.ToolCount)
	require.True(t, got.Toolset.ExternalMcpProxyOnly)
	require.Equal(t, []string{"acme"}, got.Toolset.ExternalMcpSlugs)
	require.Nil(t, got.PrimaryEndpointID)

	got = getSummaryServer(t, ctx, ti, emptyServer.ID)
	require.NotNil(t, got.Toolset)
	require.Equal(t, 0, got.Toolset.ToolCount)
	require.False(t, got.Toolset.ExternalMcpProxyOnly)
	require.Empty(t, got.Toolset.ExternalMcpSlugs)
	require.Nil(t, got.Toolset.RegistrySpecifier)
}

func TestMcpServerSummary_DeletedToolsetHasNoSummary(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgID, projectID := summaryAuth(t, ctx)

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets")})
	server := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)
	_, err := toolsetsrepo.New(ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: toolset.Slug, ProjectID: projectID})
	require.NoError(t, err)

	require.Nil(t, getSummaryServer(t, ctx, ti, server.ID).Toolset)
}

func TestMcpServerSummary_ForeignTenantRowsNeverAttach(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withSummaryServerURL(t, ti)
	orgID, projectID := summaryAuth(t, ctx)

	foreignOrgID := "org-" + uuid.NewString()
	require.NoError(t, organizationsrepo.New(ti.conn).CreateOrganizationMetadata(ctx, organizationsrepo.CreateOrganizationMetadataParams{
		ID: foreignOrgID, Name: "Foreign Organization", Slug: "foreign-" + uuid.NewString(),
	}))
	foreignProjectID := seedSummaryProject(t, ctx, ti, foreignOrgID)
	otherProjectID := seedSummaryProject(t, ctx, ti, orgID)

	foreignToolset := seedSummaryToolset(t, ctx, ti, foreignOrgID, foreignProjectID, []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "x", "y")})
	otherProjectToolset := seedSummaryToolset(t, ctx, ti, orgID, otherProjectID, []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "x", "y")})
	foreignBacked := seedSummaryServer(t, ctx, ti, projectID, foreignToolset.ID)
	otherProjectBacked := seedSummaryServer(t, ctx, ti, projectID, otherProjectToolset.ID)
	seedSummaryEndpoint(t, ctx, ti, foreignProjectID, foreignBacked.ID, uuid.NullUUID{})
	seedSummaryEndpoint(t, ctx, ti, otherProjectID, otherProjectBacked.ID, uuid.NullUUID{})

	for _, server := range []mcpserversrepo.McpServer{foreignBacked, otherProjectBacked} {
		requireNoEnrichment(t, getSummaryServer(t, ctx, ti, server.ID))
	}
	// The list only keeps toolset-backed rows whose toolset is live in the same project.
	byID := listSummaryServers(t, ctx, ti, false)
	require.NotContains(t, byID, foreignBacked.ID.String())
	require.NotContains(t, byID, otherProjectBacked.ID.String())
}

func TestListMcpServers_OmitsWrappersOfDeletedToolsets(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgID, projectID := summaryAuth(t, ctx)

	live := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets")})
	deleted := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "petstore", "get_pet")})
	liveServer := seedSummaryServer(t, ctx, ti, projectID, live.ID)
	ghost := seedSummaryServer(t, ctx, ti, projectID, deleted.ID)
	remote := seedSummaryRemoteServer(t, ctx, ti, projectID)
	_, err := toolsetsrepo.New(ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: deleted.Slug, ProjectID: projectID})
	require.NoError(t, err)

	byID := listSummaryServers(t, ctx, ti, false)
	require.Contains(t, byID, liveServer.ID.String())
	require.Contains(t, byID, remote.ID.String())
	require.NotContains(t, byID, ghost.ID.String())

	forOrg := listSummaryServers(t, ctx, ti, true)
	require.Contains(t, forOrg, liveServer.ID.String())
	require.NotContains(t, forOrg, ghost.ID.String())

	filtered, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{ToolsetID: conv.PtrEmpty(deleted.ID.String())})
	require.NoError(t, err)
	require.Empty(t, filtered.McpServers)

	// A direct read still resolves the row so it can be opened and deleted.
	require.Equal(t, ghost.ID.String(), getSummaryServer(t, ctx, ti, ghost.ID).ID)
}

func TestMcpServerSummary_CarriesToolsetDescription(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgID, projectID := summaryAuth(t, ctx)

	toolsets := toolsetsrepo.New(ti.conn)
	slug := "summary-" + uuid.NewString()[:8]
	described, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: orgID, ProjectID: projectID, Name: slug, Slug: slug,
		Description: conv.ToPGText("Answers support questions"),
	})
	require.NoError(t, err)
	plain := seedSummaryToolset(t, ctx, ti, orgID, projectID)
	describedServer := seedSummaryServer(t, ctx, ti, projectID, described.ID)
	plainServer := seedSummaryServer(t, ctx, ti, projectID, plain.ID)

	byID := listSummaryServers(t, ctx, ti, false)
	require.Equal(t, conv.PtrEmpty("Answers support questions"), byID[describedServer.ID.String()].Toolset.Description)
	require.Nil(t, byID[plainServer.ID.String()].Toolset.Description)
}

func TestListMcpServersForOrg_SummariesAcrossProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withSummaryServerURL(t, ti)
	orgID, projectID := summaryAuth(t, ctx)
	otherProjectID := seedSummaryProject(t, ctx, ti, orgID)

	toolsetA := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "a", "one")})
	toolsetB := seedSummaryToolset(t, ctx, ti, orgID, otherProjectID, []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "b", "one"), urn.NewTool(urn.ToolKindHTTP, "b", "two")})
	deploymentA := seedSummaryDeployment(t, ctx, ti, orgID, projectID)
	seedSummaryHTTPTool(t, ctx, ti, projectID, deploymentA, urn.NewTool(urn.ToolKindHTTP, "a", "one"), "a_one")
	deploymentB := seedSummaryDeployment(t, ctx, ti, orgID, otherProjectID)
	seedSummaryHTTPTool(t, ctx, ti, otherProjectID, deploymentB, urn.NewTool(urn.ToolKindHTTP, "b", "one"), "b_one")
	seedSummaryHTTPTool(t, ctx, ti, otherProjectID, deploymentB, urn.NewTool(urn.ToolKindHTTP, "b", "two"), "b_two")
	serverA := seedSummaryServer(t, ctx, ti, projectID, toolsetA.ID)
	serverB := seedSummaryServer(t, ctx, ti, otherProjectID, toolsetB.ID)
	endpointA := seedSummaryEndpoint(t, ctx, ti, projectID, serverA.ID, uuid.NullUUID{})
	endpointB := seedSummaryEndpoint(t, ctx, ti, otherProjectID, serverB.ID, uuid.NullUUID{})

	byID := listSummaryServers(t, ctx, ti, true)
	require.Equal(t, 1, byID[serverA.ID.String()].Toolset.ToolCount)
	require.Equal(t, 2, byID[serverB.ID.String()].Toolset.ToolCount)
	require.Equal(t, conv.PtrEmpty(endpointA.Slug), byID[serverA.ID.String()].PlatformEndpointSlug)
	require.Equal(t, conv.PtrEmpty(endpointB.Slug), byID[serverB.ID.String()].PlatformEndpointSlug)
	require.Equal(t, platformURL(endpointB.Slug), byID[serverB.ID.String()].PrimaryEndpointURL)
}

func TestListMcpServersForOrg_SummariesOnlyForReadableServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgID, projectID := summaryAuth(t, ctx)

	readableToolset := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "a", "one")})
	hiddenToolset := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "b", "one")})
	readable := seedSummaryServer(t, ctx, ti, projectID, readableToolset.ID)
	hidden := seedSummaryServer(t, ctx, ti, projectID, hiddenToolset.ID)
	seedSummaryEndpoint(t, ctx, ti, projectID, readable.ID, uuid.NullUUID{})
	seedSummaryEndpoint(t, ctx, ti, projectID, hidden.ID, uuid.NullUUID{})

	scoped := withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeOrgRead, orgID),
		authz.NewGrant(authz.ScopeMCPRead, readableToolset.ID.String()),
	)
	byID := listSummaryServers(t, scoped, ti, true)
	require.Len(t, byID, 2, "org:read still lists every server")
	require.NotNil(t, byID[readable.ID.String()].Toolset)
	require.NotNil(t, byID[readable.ID.String()].PrimaryEndpointID)
	requireNoEnrichment(t, byID[hidden.ID.String()])
}

func TestMcpServerSummary_PrimaryEndpointRanking(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withSummaryServerURL(t, ti)
	orgID, projectID := summaryAuth(t, ctx)

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID)
	server := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)
	platform := seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{})

	// A soft-deleted domain cannot serve, so its endpoint never wins.
	deletedDomain := seedSummaryCustomDomain(t, ctx, ti, orgID, true)
	seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{UUID: deletedDomain.ID, Valid: true})
	require.NoError(t, customdomainsrepo.New(ti.conn).DeleteCustomDomain(ctx, orgID))
	got := getSummaryServer(t, ctx, ti, server.ID)
	require.Equal(t, conv.PtrEmpty(platform.ID.String()), got.PrimaryEndpointID)

	domain := seedSummaryCustomDomain(t, ctx, ti, orgID, true)
	nonRoot := seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{UUID: domain.ID, Valid: true})
	got = getSummaryServer(t, ctx, ti, server.ID)
	require.Equal(t, conv.PtrEmpty(nonRoot.ID.String()), got.PrimaryEndpointID)
	require.Equal(t, conv.PtrEmpty("https://"+domain.Domain+"/mcp/"+nonRoot.Slug), got.PrimaryEndpointURL)
	require.Equal(t, conv.PtrEmpty(platform.Slug), got.PlatformEndpointSlug, "platform slug never names a custom-domain endpoint")

	root := seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{UUID: domain.ID, Valid: true})
	require.NoError(t, customdomainsrepo.New(ti.conn).SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID: root.ID, CustomDomainID: domain.ID,
	}))
	for _, got := range []*types.McpServer{getSummaryServer(t, ctx, ti, server.ID), listSummaryServers(t, ctx, ti, false)[server.ID.String()]} {
		require.Equal(t, conv.PtrEmpty(root.ID.String()), got.PrimaryEndpointID)
		require.Equal(t, conv.PtrEmpty("https://"+domain.Domain), got.PrimaryEndpointURL)
	}
}

func TestMcpServerSummary_UnservedCustomDomainNeverWins(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withSummaryServerURL(t, ti)
	orgID, projectID := summaryAuth(t, ctx)

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID)
	server := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)
	platform := seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{})
	domain := seedSummaryCustomDomain(t, ctx, ti, orgID, false)
	seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{UUID: domain.ID, Valid: true})

	got := getSummaryServer(t, ctx, ti, server.ID)
	require.Equal(t, conv.PtrEmpty(platform.ID.String()), got.PrimaryEndpointID)
	require.Equal(t, platformURL(platform.Slug), got.PrimaryEndpointURL)
}

func TestGetMcpServer_ToolsetSelectorSlugMatchesPrimaryPlatformEndpoint(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withSummaryServerURL(t, ti)
	orgID, projectID := summaryAuth(t, ctx)

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID)
	server := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)
	oldest := seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{})
	seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{})
	seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{})

	got, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ToolsetID: conv.PtrEmpty(toolset.ID.String())})
	require.NoError(t, err)
	require.Equal(t, conv.PtrEmpty(oldest.Slug), got.PlatformEndpointSlug)
	require.Equal(t, conv.PtrEmpty(oldest.ID.String()), got.PrimaryEndpointID)
	require.Equal(t, platformURL(oldest.Slug), got.PrimaryEndpointURL)
}

func TestGetMcpServer_PrimaryEndpointWithoutServerURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgID, projectID := summaryAuth(t, ctx)

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID)
	server := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)
	endpoint := seedSummaryEndpoint(t, ctx, ti, projectID, server.ID, uuid.NullUUID{})

	got := getSummaryServer(t, ctx, ti, server.ID)
	require.Equal(t, conv.PtrEmpty(endpoint.ID.String()), got.PrimaryEndpointID)
	require.Nil(t, got.PrimaryEndpointURL)
}

func TestCreateAndUpdateMcpServer_ResponsesCarrySummary(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withSummaryServerURL(t, ti)
	orgID, projectID := summaryAuth(t, ctx)

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{
		urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets"),
		urn.NewTool(urn.ToolKindExternalMCP, "acme", "proxy"),
	})
	deployment := seedSummaryDeployment(t, ctx, ti, orgID, projectID)
	seedSummaryHTTPTool(t, ctx, ti, projectID, deployment, urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets"), "list_pets")
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name: "summary create", ToolsetID: conv.PtrEmpty(toolset.ID.String()), Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.Toolset)
	require.Equal(t, toolset.ID.String(), created.Toolset.ID)
	require.Equal(t, 1, created.Toolset.ToolCount)
	require.Equal(t, []string{"acme"}, created.Toolset.ExternalMcpSlugs)
	require.Nil(t, created.PrimaryEndpointID, "a new server has no endpoint yet")

	endpoint := seedSummaryEndpoint(t, ctx, ti, projectID, uuid.MustParse(created.ID), uuid.NullUUID{})
	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, ToolsetID: conv.PtrEmpty(toolset.ID.String()), Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Toolset)
	require.Equal(t, 1, updated.Toolset.ToolCount)
	require.Equal(t, conv.PtrEmpty(endpoint.ID.String()), updated.PrimaryEndpointID)
	require.Equal(t, platformURL(endpoint.Slug), updated.PrimaryEndpointURL)
	require.Equal(t, conv.PtrEmpty(endpoint.Slug), updated.PlatformEndpointSlug)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, projectID).String()
	remote, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name: "summary remote create", RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	requireNoEnrichment(t, remote)

	remoteEndpoint := seedSummaryEndpoint(t, ctx, ti, projectID, uuid.MustParse(remote.ID), uuid.NullUUID{})
	remoteUpdated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: remote.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, remoteUpdated.Toolset)
	require.Equal(t, conv.PtrEmpty(remoteEndpoint.ID.String()), remoteUpdated.PrimaryEndpointID)
}

func TestMcpServerSummary_HasEndpointCountsAnyLiveEndpoint(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withSummaryServerURL(t, ti)
	orgID, projectID := summaryAuth(t, ctx)

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID)
	bare := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)
	platform := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)
	seedSummaryEndpoint(t, ctx, ti, projectID, platform.ID, uuid.NullUUID{})
	pending := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)
	domain := seedSummaryCustomDomain(t, ctx, ti, orgID, false)
	seedSummaryEndpoint(t, ctx, ti, projectID, pending.ID, uuid.NullUUID{UUID: domain.ID, Valid: true})

	want := map[uuid.UUID]bool{bare.ID: false, platform.ID: true, pending.ID: true}
	for _, forOrg := range []bool{false, true} {
		byID := listSummaryServers(t, ctx, ti, forOrg)
		for id, has := range want {
			got := byID[id.String()]
			require.NotNil(t, got)
			require.NotNil(t, got.HasEndpoint)
			require.Equal(t, has, *got.HasEndpoint)
		}
	}
	got := getSummaryServer(t, ctx, ti, pending.ID)
	require.NotNil(t, got.HasEndpoint)
	require.True(t, *got.HasEndpoint, "an endpoint on a domain that is not live yet still counts")
	require.Nil(t, got.PrimaryEndpointID)
}
