package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mv"
	packagesrepo "github.com/speakeasy-api/gram/server/internal/packages/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	templatesrepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedSummaryDeployment creates a completed deployment, which becomes the project's latest.
func seedSummaryDeployment(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	queries := deploymentsrepo.New(ti.conn)
	key := "summary-" + uuid.NewString()
	_, err := queries.CreateDeployment(ctx, deploymentsrepo.CreateDeploymentParams{
		IdempotencyKey: key, UserID: "summary-user", OrganizationID: organizationID, ProjectID: projectID,
	})
	require.NoError(t, err)
	deployment, err := queries.GetDeploymentByIdempotencyKey(ctx, deploymentsrepo.GetDeploymentByIdempotencyKeyParams{IdempotencyKey: key, ProjectID: projectID})
	require.NoError(t, err)
	for _, status := range []string{"created", "pending", "completed"} {
		_, err = queries.TransitionDeployment(ctx, deploymentsrepo.TransitionDeploymentParams{
			DeploymentID: deployment.Deployment.ID, Status: status, ProjectID: projectID, Event: "test", Message: "summary test",
		})
		require.NoError(t, err)
	}
	return deployment.Deployment.ID
}

func seedSummaryHTTPTool(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, deploymentID uuid.UUID, toolURN urn.Tool, name string) {
	t.Helper()
	_, err := deploymentsrepo.New(ti.conn).CreateOpenAPIv3ToolDefinition(ctx, deploymentsrepo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID: projectID, DeploymentID: deploymentID, ToolUrn: toolURN, Name: name,
		Summary: name, Description: name, Tags: []string{}, Security: []byte("[]"),
		HttpMethod: "GET", Path: "/" + name, SchemaVersion: "3.0.0", Schema: []byte("{}"),
		HeaderSettings: []byte("{}"), QuerySettings: []byte("{}"), PathSettings: []byte("{}"),
		ServerEnvVar: "SUMMARY_SERVER_URL",
	})
	require.NoError(t, err)
}

func seedSummaryFunctionTool(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID uuid.UUID, deploymentID uuid.UUID, toolURN urn.Tool, name string) {
	t.Helper()
	asset, err := assetsrepo.New(ti.conn).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name: name + ".zip", Url: "https://example.com/" + name + ".zip", ProjectID: projectID, OrganizationID: organizationID,
		Sha256: uuid.NewString(), Kind: "functions", ContentType: "application/zip", ContentLength: 1,
	})
	require.NoError(t, err)
	queries := deploymentsrepo.New(ti.conn)
	function, err := queries.UpsertDeploymentFunctionsAsset(ctx, deploymentsrepo.UpsertDeploymentFunctionsAssetParams{
		DeploymentID: deploymentID, AssetID: asset.ID, Name: name, Slug: name, Runtime: "nodejs:22",
	})
	require.NoError(t, err)
	_, err = queries.CreateFunctionsTool(ctx, deploymentsrepo.CreateFunctionsToolParams{
		DeploymentID: deploymentID, FunctionID: function.ID, ToolUrn: toolURN, ProjectID: projectID, Runtime: "nodejs:22",
		Name: name, Description: name, InputSchema: []byte(`{"type":"object"}`), Variables: []byte("{}"), AuthInput: []byte("{}"),
		Tags: []string{}, Meta: []byte("{}"),
	})
	require.NoError(t, err)
}

// seedSummaryExternalTools attaches one external MCP to the deployment; a :proxy URN gets a proxy definition.
func seedSummaryExternalTools(t *testing.T, ctx context.Context, ti *testInstance, deploymentID uuid.UUID, slug string, tools ...urn.Tool) {
	t.Helper()
	repo := externalmcprepo.New(ti.conn)
	attachment, err := repo.CreateExternalMCPAttachment(ctx, externalmcprepo.CreateExternalMCPAttachmentParams{
		DeploymentID: deploymentID, Name: slug, Slug: slug, RegistryServerSpecifier: "io.example/" + slug,
	})
	require.NoError(t, err)
	for _, tool := range tools {
		params := externalmcprepo.CreateExternalMCPToolDefinitionParams{
			ExternalMcpAttachmentID: attachment.ID, ToolUrn: tool.String(), Type: string(externalmcptypes.ExternalMCPToolTypeProxy),
			RemoteUrl: "https://example.com/mcp", TransportType: externalmcptypes.TransportTypeStreamableHTTP, OauthVersion: "none",
		}
		if tool.Name != "proxy" {
			params.Type = string(externalmcptypes.ExternalMCPToolTypeDirect)
			params.Name = pgtype.Text{String: tool.Name, Valid: true}
			params.Description = pgtype.Text{String: tool.Name, Valid: true}
			params.Schema = []byte(`{"type":"object"}`)
		}
		_, err := repo.CreateExternalMCPToolDefinition(ctx, params)
		require.NoError(t, err)
	}
}

// seedSummaryPackageTool publishes a package from another project with one HTTP tool and installs it on consumerDeploymentID.
func seedSummaryPackageTool(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, consumerDeploymentID uuid.UUID, toolURN urn.Tool, name string) {
	t.Helper()
	publisherProjectID := seedSummaryProject(t, ctx, ti, organizationID)
	publisherDeploymentID := seedSummaryDeployment(t, ctx, ti, organizationID, publisherProjectID)
	seedSummaryHTTPTool(t, ctx, ti, publisherProjectID, publisherDeploymentID, toolURN, name)
	packages := packagesrepo.New(ti.conn)
	packageID, err := packages.CreatePackage(ctx, packagesrepo.CreatePackageParams{
		Name: "summary-" + uuid.NewString()[:8], Keywords: []string{}, OrganizationID: organizationID, ProjectID: publisherProjectID,
	})
	require.NoError(t, err)
	version, err := packages.CreatePackageVersion(ctx, packagesrepo.CreatePackageVersionParams{
		PackageID: packageID, DeploymentID: publisherDeploymentID, Major: 1, Visibility: "public",
	})
	require.NoError(t, err)
	_, err = deploymentsrepo.New(ti.conn).UpsertDeploymentPackage(ctx, deploymentsrepo.UpsertDeploymentPackageParams{
		DeploymentID: consumerDeploymentID, PackageID: packageID, VersionID: version.ID,
	})
	require.NoError(t, err)
}

func addSummaryToolsetVersion(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsetsrepo.Toolset, version int64, urns []urn.Tool) {
	t.Helper()
	_, err := toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID: toolset.ID, Version: version, ToolUrns: urns, ResourceUrns: []urn.Resource{},
	})
	require.NoError(t, err)
}

func describeSummaryToolset(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, toolset toolsetsrepo.Toolset) *types.Toolset {
	t.Helper()
	described, err := mv.DescribeToolset(ctx, testenv.NewLogger(t), ti.conn, mv.ProjectID(projectID), mv.ToolsetSlug(toolset.Slug), nil, nil)
	require.NoError(t, err)
	return described
}

func TestMcpServerSummary_CountsOnlyToolsTheToolsetServes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgID, projectID := summaryAuth(t, ctx)

	removed := urn.NewTool(urn.ToolKindHTTP, "petstore", "removed_op")
	renamedOld := urn.NewTool(urn.ToolKindHTTP, "petstore", "old_name")
	live := urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets")
	sameName := urn.NewTool(urn.ToolKindHTTP, "mirror", "list_pets")
	packaged := urn.NewTool(urn.ToolKindHTTP, "vendor", "get_rates")
	direct := urn.NewTool(urn.ToolKindExternalMCP, "acme", "search")
	prompt := urn.NewTool(urn.ToolKindPrompt, "prompts", "greet")
	never := urn.NewTool(urn.ToolKindHTTP, "gone", "never_deployed")
	unregisteredPlatform := urn.NewTool(urn.ToolKindPlatform, "nowhere", "missing")
	platformTools := platformtools.ListPlatformTools()
	require.NotEmpty(t, platformTools)
	platform := platformTools[0].ToolURN()

	// The older deployment still has the tools a source removal or rename dropped.
	older := seedSummaryDeployment(t, ctx, ti, orgID, projectID)
	seedSummaryHTTPTool(t, ctx, ti, projectID, older, removed, "removed_op")
	seedSummaryHTTPTool(t, ctx, ti, projectID, older, renamedOld, "old_name")

	latest := seedSummaryDeployment(t, ctx, ti, orgID, projectID)
	seedSummaryHTTPTool(t, ctx, ti, projectID, latest, live, "list_pets")
	seedSummaryHTTPTool(t, ctx, ti, projectID, latest, sameName, "list_pets")
	seedSummaryExternalTools(t, ctx, ti, latest, "acme", direct)
	seedSummaryPackageTool(t, ctx, ti, orgID, latest, packaged, "get_rates")
	_, err := templatesrepo.New(ti.conn).CreateTemplate(ctx, templatesrepo.CreateTemplateParams{
		ProjectID: projectID, ToolUrn: prompt, Name: "greet", Prompt: "Hello", Arguments: []byte("{}"), ToolsHint: []string{}, ToolUrnsHint: []string{},
	})
	require.NoError(t, err)

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{
		removed, renamedOld, live, sameName, packaged, direct, prompt, never, platform, unregisteredPlatform,
	})
	server := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)

	// list_pets (two URNs, one name), get_rates from the package, acme search, greet, and the registered platform tool.
	const want = 5
	described := describeSummaryToolset(t, ctx, ti, projectID, toolset)
	require.Len(t, described.Tools, want)
	require.Equal(t, want, getSummaryServer(t, ctx, ti, server.ID).Toolset.ToolCount)
	for _, forOrg := range []bool{false, true} {
		require.Equal(t, len(described.Tools), listSummaryServers(t, ctx, ti, forOrg)[server.ID.String()].Toolset.ToolCount)
	}

	// A live proxy is still excluded, though the toolset page lists it.
	proxy := urn.NewTool(urn.ToolKindExternalMCP, "zeta", "proxy")
	seedSummaryExternalTools(t, ctx, ti, latest, "zeta", proxy)
	addSummaryToolsetVersion(t, ctx, ti, toolset, 2, []urn.Tool{live, packaged, direct, prompt, platform, proxy})
	got := getSummaryServer(t, ctx, ti, server.ID).Toolset
	require.Equal(t, want, got.ToolCount)
	require.False(t, got.ExternalMcpProxyOnly)
	require.Len(t, describeSummaryToolset(t, ctx, ti, projectID, toolset).Tools, want+1)
}

func TestMcpServerSummary_NoDeploymentCountsOnlyPlatformTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgID, projectID := summaryAuth(t, ctx)

	platformTools := platformtools.ListPlatformTools()
	require.NotEmpty(t, platformTools)
	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{
		urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets"),
		platformTools[0].ToolURN(),
	})
	server := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)

	require.Equal(t, 1, getSummaryServer(t, ctx, ti, server.ID).Toolset.ToolCount)
	require.Len(t, describeSummaryToolset(t, ctx, ti, projectID, toolset).Tools, 1)
}

func TestMcpServerSummary_CountsOnlyLiveFunctionAndUndeletedHTTPTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgID, projectID := summaryAuth(t, ctx)

	staleFunction := urn.NewTool(urn.ToolKindFunction, "weather", "old_forecast")
	liveFunction := urn.NewTool(urn.ToolKindFunction, "weather", "forecast")
	live := urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets")
	deleted := urn.NewTool(urn.ToolKindHTTP, "petstore", "delete_pet")

	older := seedSummaryDeployment(t, ctx, ti, orgID, projectID)
	seedSummaryFunctionTool(t, ctx, ti, orgID, projectID, older, staleFunction, "old_forecast")

	latest := seedSummaryDeployment(t, ctx, ti, orgID, projectID)
	seedSummaryFunctionTool(t, ctx, ti, orgID, projectID, latest, liveFunction, "forecast")
	seedSummaryHTTPTool(t, ctx, ti, projectID, latest, live, "list_pets")
	seedSummaryHTTPTool(t, ctx, ti, projectID, latest, deleted, "delete_pet")
	require.NoError(t, testrepo.New(ti.conn).SoftDeleteHTTPToolDefinitionFixture(ctx, testrepo.SoftDeleteHTTPToolDefinitionFixtureParams{
		ProjectID: projectID, DeploymentID: latest, ToolUrn: deleted,
	}))

	toolset := seedSummaryToolset(t, ctx, ti, orgID, projectID, []urn.Tool{staleFunction, liveFunction, live, deleted})
	server := seedSummaryServer(t, ctx, ti, projectID, toolset.ID)

	const want = 2
	require.Len(t, describeSummaryToolset(t, ctx, ti, projectID, toolset).Tools, want)
	require.Equal(t, want, getSummaryServer(t, ctx, ti, server.ID).Toolset.ToolCount)
	for _, forOrg := range []bool{false, true} {
		require.Equal(t, want, listSummaryServers(t, ctx, ti, forOrg)[server.ID.String()].Toolset.ToolCount)
	}
}
