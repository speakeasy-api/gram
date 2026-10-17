package activities_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	packagesrepo "github.com/speakeasy-api/gram/server/internal/packages/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	ragrepo "github.com/speakeasy-api/gram/server/internal/rag/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListToolsetsForIndexingRequiresResolvableToolsAndMissingEmbeddings(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "list_toolsets_for_indexing")
	require.NoError(t, err)

	organizationID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Test Org",
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	deploymentID := createCompletedDeployment(t, db, organizationID, project.ID)
	validToolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", "valid-tool")
	createHTTPToolDefinition(t, db, project.ID, deploymentID, validToolURN)

	validToolset := createMCPToolset(t, db, organizationID, project.ID, "valid", []urn.Tool{validToolURN}, true)
	packageProject, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Package Project",
		Slug:           "package-project-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	packageDeploymentID := createCompletedDeployment(t, db, organizationID, packageProject.ID)
	packageToolURN := urn.NewTool(urn.ToolKindHTTP, "package-api", "package-tool")
	createHTTPToolDefinition(t, db, packageProject.ID, packageDeploymentID, packageToolURN)
	packageID, err := packagesrepo.New(db).CreatePackage(ctx, packagesrepo.CreatePackageParams{
		Name:            "test-package-" + uuid.NewString()[:8],
		Title:           pgtype.Text{},
		Summary:         pgtype.Text{},
		Url:             pgtype.Text{},
		DescriptionRaw:  pgtype.Text{},
		DescriptionHtml: pgtype.Text{},
		Keywords:        []string{},
		OrganizationID:  organizationID,
		ProjectID:       packageProject.ID,
		ImageAssetID:    uuid.NullUUID{},
	})
	require.NoError(t, err)
	packageVersion, err := packagesrepo.New(db).CreatePackageVersion(ctx, packagesrepo.CreatePackageVersionParams{
		PackageID:    packageID,
		DeploymentID: packageDeploymentID,
		Major:        1,
		Minor:        0,
		Patch:        0,
		Prerelease:   pgtype.Text{},
		Build:        pgtype.Text{},
		Visibility:   "public",
	})
	require.NoError(t, err)
	_, err = deploymentsrepo.New(db).UpsertDeploymentPackage(ctx, deploymentsrepo.UpsertDeploymentPackageParams{
		DeploymentID: deploymentID,
		PackageID:    packageID,
		VersionID:    packageVersion.ID,
	})
	require.NoError(t, err)
	packageToolset := createMCPToolset(t, db, organizationID, project.ID, "package", []urn.Tool{packageToolURN}, true)
	createMCPToolset(t, db, organizationID, project.ID, "dangling", []urn.Tool{
		urn.NewTool(urn.ToolKindHTTP, "test-api", "deleted-tool"),
	}, true)
	createMCPToolset(t, db, organizationID, project.ID, "empty", []urn.Tool{}, true)

	externalMCPAttachment, err := externalmcprepo.New(db).CreateExternalMCPAttachment(ctx, externalmcprepo.CreateExternalMCPAttachmentParams{
		DeploymentID:            deploymentID,
		RegistryID:              uuid.NullUUID{},
		Name:                    "Proxy",
		Slug:                    "proxy",
		RegistryServerSpecifier: "proxy",
	})
	require.NoError(t, err)
	proxyToolURN, err := urn.ParseTool("tools:externalmcp:proxy:proxy")
	require.NoError(t, err)
	proxyDefinition, err := externalmcprepo.New(db).CreateExternalMCPToolDefinition(ctx, externalmcprepo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    externalMCPAttachment.ID,
		ToolUrn:                    proxyToolURN.String(),
		Type:                       string(externalmcptypes.ExternalMCPToolTypeProxy),
		Name:                       pgtype.Text{},
		Description:                pgtype.Text{},
		Schema:                     nil,
		RemoteUrl:                  "https://example.com/mcp",
		TransportType:              externalmcptypes.TransportTypeStreamableHTTP,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
		HeaderDefinitions:          nil,
		Title:                      pgtype.Text{},
		ReadOnlyHint:               pgtype.Bool{},
		DestructiveHint:            pgtype.Bool{},
		IdempotentHint:             pgtype.Bool{},
		OpenWorldHint:              pgtype.Bool{},
	})
	require.NoError(t, err)
	require.Equal(t, proxyToolURN.String(), proxyDefinition.ToolUrn)
	require.Equal(t, "proxy", proxyDefinition.Type)
	createMCPToolset(t, db, organizationID, project.ID, "proxy-only", []urn.Tool{proxyToolURN}, true)
	mixedToolset := createMCPToolset(t, db, organizationID, project.ID, "mixed-proxy", []urn.Tool{validToolURN, proxyToolURN}, true)
	mixedVersion, err := toolsetsrepo.New(db).GetLatestToolsetVersion(ctx, mixedToolset.ID)
	require.NoError(t, err)
	require.Equal(t, []urn.Tool{validToolURN, proxyToolURN}, mixedVersion.ToolUrns)

	activity := activities.NewListToolsetsForIndexing(db)
	projectIDs, err := activity.ListProjects(ctx, activities.ListProjectsForToolsetIndexingInput{RotationSeed: 1, ProjectLimit: 100})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{project.ID}, projectIDs)

	targets, err := activity.Do(ctx, activities.ListToolsetsForIndexingInput{RotationSeed: 1, ScanLimit: 100, ProjectIDs: projectIDs})
	require.NoError(t, err)
	require.ElementsMatch(t, []activities.ToolsetIndexTarget{{
		ProjectID:      project.ID,
		ToolsetID:      validToolset.ID,
		ToolsetSlug:    "valid",
		ToolsetVersion: 1,
		DeploymentID:   deploymentID,
	}, {
		ProjectID:      project.ID,
		ToolsetID:      packageToolset.ID,
		ToolsetSlug:    "package",
		ToolsetVersion: 1,
		DeploymentID:   deploymentID,
	}}, targets)

	_, err = ragrepo.New(db).InsertToolsetEmbedding(ctx, ragrepo.InsertToolsetEmbeddingParams{
		ProjectID:      project.ID,
		ToolsetID:      validToolset.ID,
		ToolsetVersion: 1,
		EntryKey:       "tools:valid",
		EmbeddingModel: "test-model",
		Embedding1536:  pgvector.NewVector(make([]float32, 1536)),
		Payload:        []byte(fmt.Sprintf(`{"_gramIndexDeploymentId":%q}`, deploymentID)),
		Tags:           []string{},
	})
	require.NoError(t, err)
	indexed, err := ragrepo.New(db).ToolsetToolsAreIndexed(ctx, ragrepo.ToolsetToolsAreIndexedParams{
		ProjectID:      project.ID,
		ToolsetID:      validToolset.ID,
		ToolsetVersion: 1,
	})
	require.NoError(t, err)
	require.True(t, indexed)
	_, err = ragrepo.New(db).InsertToolsetEmbedding(ctx, ragrepo.InsertToolsetEmbeddingParams{
		ProjectID:      project.ID,
		ToolsetID:      packageToolset.ID,
		ToolsetVersion: 1,
		EntryKey:       "tools:package",
		EmbeddingModel: "test-model",
		Embedding1536:  pgvector.NewVector(make([]float32, 1536)),
		Payload:        []byte(fmt.Sprintf(`{"_gramIndexDeploymentId":%q}`, deploymentID)),
		Tags:           []string{},
	})
	require.NoError(t, err)

	targets, err = activity.Do(ctx, activities.ListToolsetsForIndexingInput{RotationSeed: 2, ScanLimit: 100, ProjectIDs: projectIDs})
	require.NoError(t, err)
	require.Empty(t, targets)

	newDeploymentID := createCompletedDeployment(t, db, organizationID, project.ID)
	createHTTPToolDefinition(t, db, project.ID, newDeploymentID, validToolURN)
	_, err = deploymentsrepo.New(db).UpsertDeploymentPackage(ctx, deploymentsrepo.UpsertDeploymentPackageParams{
		DeploymentID: newDeploymentID,
		PackageID:    packageID,
		VersionID:    packageVersion.ID,
	})
	require.NoError(t, err)
	indexed, err = ragrepo.New(db).ToolsetToolsAreIndexed(ctx, ragrepo.ToolsetToolsAreIndexedParams{
		ProjectID:      project.ID,
		ToolsetID:      validToolset.ID,
		ToolsetVersion: 1,
	})
	require.NoError(t, err)
	require.False(t, indexed)
	targets, err = activity.Do(ctx, activities.ListToolsetsForIndexingInput{RotationSeed: 3, ScanLimit: 100, ProjectIDs: projectIDs})
	require.NoError(t, err)
	require.ElementsMatch(t, []activities.ToolsetIndexTarget{{
		ProjectID:      project.ID,
		ToolsetID:      validToolset.ID,
		ToolsetSlug:    "valid",
		ToolsetVersion: 1,
		DeploymentID:   newDeploymentID,
	}, {
		ProjectID:      project.ID,
		ToolsetID:      packageToolset.ID,
		ToolsetSlug:    "package",
		ToolsetVersion: 1,
		DeploymentID:   newDeploymentID,
	}}, targets)
}

func TestListToolsetsForIndexingSkipsBlockedOrganizations(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "list_blocked_toolsets_for_indexing")
	require.NoError(t, err)

	organizationID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Test Org",
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	deploymentID := createCompletedDeployment(t, db, organizationID, project.ID)
	toolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", "valid-tool")
	createHTTPToolDefinition(t, db, project.ID, deploymentID, toolURN)
	toolset := createMCPToolset(t, db, organizationID, project.ID, "valid", []urn.Tool{toolURN}, true)

	orphanOrganizationID := "missing-" + uuid.NewString()[:8]
	orphanProject, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Orphan Project",
		Slug:           "orphan-" + uuid.NewString()[:8],
		OrganizationID: orphanOrganizationID,
	})
	require.NoError(t, err)
	orphanDeploymentID := createCompletedDeployment(t, db, orphanOrganizationID, orphanProject.ID)
	createHTTPToolDefinition(t, db, orphanProject.ID, orphanDeploymentID, toolURN)
	createMCPToolset(t, db, orphanOrganizationID, orphanProject.ID, "orphan", []urn.Tool{toolURN}, true)

	activity := activities.NewListToolsetsForIndexing(db)
	assertEligible := func(want bool) {
		t.Helper()

		projectIDs, listErr := activity.ListProjects(ctx, activities.ListProjectsForToolsetIndexingInput{RotationSeed: 1, ProjectLimit: 100})
		require.NoError(t, listErr)
		targets, listErr := activity.Do(ctx, activities.ListToolsetsForIndexingInput{
			RotationSeed: 1,
			ScanLimit:    100,
			ProjectIDs:   []uuid.UUID{project.ID, orphanProject.ID},
		})
		require.NoError(t, listErr)

		if want {
			require.Equal(t, []uuid.UUID{project.ID}, projectIDs)
			require.Equal(t, []activities.ToolsetIndexTarget{{
				ProjectID:      project.ID,
				ToolsetID:      toolset.ID,
				ToolsetSlug:    "valid",
				ToolsetVersion: 1,
				DeploymentID:   deploymentID,
			}}, targets)
		} else {
			require.Empty(t, projectIDs)
			require.Empty(t, targets)
		}
	}

	// A missing chat key remains eligible so the first embedding can provision it.
	assertEligible(true)

	keyQueries := openrouterrepo.New(db)
	_, err = keyQueries.CreateOpenRouterAPIKey(ctx, openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        "internal",
		KeyEncrypted:   pgtype.Text{String: "fixture", Valid: true},
		KeyHash:        uuid.NewString(),
		MonthlyCredits: 0,
	})
	require.NoError(t, err)
	fixtureQueries := testrepo.New(db)
	require.NoError(t, fixtureQueries.SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: organizationID,
		KeyType:        "internal",
		Disabled:       true,
		DisableCauses:  []string{"admin_lock"},
	}))
	assertEligible(true)

	_, err = keyQueries.CreateOpenRouterAPIKey(ctx, openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        "chat",
		KeyEncrypted:   pgtype.Text{String: "fixture", Valid: true},
		KeyHash:        uuid.NewString(),
		MonthlyCredits: 0,
	})
	require.NoError(t, err)
	require.NoError(t, fixtureQueries.SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: organizationID,
		KeyType:        "chat",
		Disabled:       false,
		DisableCauses:  []string{"admin_lock"},
	}))
	assertEligible(false)

	require.NoError(t, fixtureQueries.SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: organizationID,
		KeyType:        "chat",
		Disabled:       true,
		DisableCauses:  []string{},
	}))
	assertEligible(true)

	require.NoError(t, fixtureQueries.SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: organizationID,
		KeyType:        "chat",
		Disabled:       true,
		DisableCauses:  nil,
	}))
	assertEligible(false)

	require.NoError(t, fixtureQueries.SoftDeleteOpenRouterAPIKeyFixture(ctx, testrepo.SoftDeleteOpenRouterAPIKeyFixtureParams{
		OrganizationID: organizationID,
		KeyType:        "chat",
	}))
	assertEligible(true)

	_, err = projectsrepo.New(db).DeleteProject(ctx, project.ID)
	require.NoError(t, err)
	assertEligible(false)
}

// A toolset that is not MCP-enabled is still served, and so needs a search
// index, while a live, non-disabled server other than its own hosted address
// fronts it.
func TestListToolsetsForIndexingIncludesToolsetsServedThroughMCPServers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "list_served_toolsets_for_indexing")
	require.NoError(t, err)

	organizationID, project := createIndexingProject(t, db)
	deploymentID := createCompletedDeployment(t, db, organizationID, project.ID)
	toolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", "valid-tool")
	createHTTPToolDefinition(t, db, project.ID, deploymentID, toolURN)

	enabled := createMCPToolset(t, db, organizationID, project.ID, "enabled", []urn.Tool{toolURN}, true)

	fronted := createMCPToolset(t, db, organizationID, project.ID, "fronted", []urn.Tool{toolURN}, false)
	frontingServer := createToolsetMCPServer(t, db, project.ID, uuid.New(), fronted.ID, "private")

	ownAddressOnly := createMCPToolset(t, db, organizationID, project.ID, "own-address-only", []urn.Tool{toolURN}, false)
	createToolsetMCPServer(t, db, project.ID, ownAddressOnly.ID, ownAddressOnly.ID, "private")

	deletedServer := createMCPToolset(t, db, organizationID, project.ID, "deleted-server", []urn.Tool{toolURN}, false)
	deleted := createToolsetMCPServer(t, db, project.ID, uuid.New(), deletedServer.ID, "private")
	_, err = mcpserversrepo.New(db).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: deleted.ID, ProjectID: project.ID})
	require.NoError(t, err)

	disabledServer := createMCPToolset(t, db, organizationID, project.ID, "disabled-server", []urn.Tool{toolURN}, false)
	createToolsetMCPServer(t, db, project.ID, uuid.New(), disabledServer.ID, "disabled")

	target := func(toolset toolsetsrepo.Toolset) activities.ToolsetIndexTarget {
		return activities.ToolsetIndexTarget{
			ProjectID:      project.ID,
			ToolsetID:      toolset.ID,
			ToolsetSlug:    types.Slug(toolset.Slug),
			ToolsetVersion: 1,
			DeploymentID:   deploymentID,
		}
	}

	activity := activities.NewListToolsetsForIndexing(db)
	targets, err := activity.Do(ctx, activities.ListToolsetsForIndexingInput{RotationSeed: 1, ScanLimit: 100, ProjectIDs: []uuid.UUID{project.ID}})
	require.NoError(t, err)
	require.ElementsMatch(t, []activities.ToolsetIndexTarget{target(enabled), target(fronted)}, targets)

	// Once the fronting server is deleted nothing serves the toolset, so it
	// drops out while the MCP-enabled one stays.
	_, err = mcpserversrepo.New(db).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: frontingServer.ID, ProjectID: project.ID})
	require.NoError(t, err)
	targets, err = activity.Do(ctx, activities.ListToolsetsForIndexingInput{RotationSeed: 2, ScanLimit: 100, ProjectIDs: []uuid.UUID{project.ID}})
	require.NoError(t, err)
	require.Equal(t, []activities.ToolsetIndexTarget{target(enabled)}, targets)
}

// Project discovery applies the same served-toolset predicate, so a project
// whose only served toolset sits behind an mcp_servers row is still swept.
func TestListProjectsForToolsetIndexingIncludesProjectsServedThroughMCPServers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "list_served_projects_for_indexing")
	require.NoError(t, err)

	organizationID, project := createIndexingProject(t, db)
	toolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", "valid-tool")
	fronted := createMCPToolset(t, db, organizationID, project.ID, "fronted", []urn.Tool{toolURN}, false)
	server := createToolsetMCPServer(t, db, project.ID, uuid.New(), fronted.ID, "private")

	activity := activities.NewListToolsetsForIndexing(db)
	projectIDs, err := activity.ListProjects(ctx, activities.ListProjectsForToolsetIndexingInput{RotationSeed: 1, ProjectLimit: 100})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{project.ID}, projectIDs)

	_, err = mcpserversrepo.New(db).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: server.ID, ProjectID: project.ID})
	require.NoError(t, err)
	projectIDs, err = activity.ListProjects(ctx, activities.ListProjectsForToolsetIndexingInput{RotationSeed: 2, ProjectLimit: 100})
	require.NoError(t, err)
	require.Empty(t, projectIDs)
}

// createIndexingProject creates an organization and a project in it.
func createIndexingProject(t *testing.T, db *pgxpool.Pool) (string, projectsrepo.Project) {
	t.Helper()

	organizationID := "org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Test Org",
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return organizationID, project
}

// createToolsetMCPServer creates a live mcp_servers row with the given id and
// visibility fronting a toolset. Passing the toolset's own id creates its hosted address.
func createToolsetMCPServer(t *testing.T, db *pgxpool.Pool, projectID, serverID, toolsetID uuid.UUID, visibility string) mcpserversrepo.McpServer {
	t.Helper()

	server, err := mcpserversrepo.New(db).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             projectID,
		Name:                  pgtype.Text{},
		Slug:                  pgtype.Text{String: "server-" + uuid.NewString()[:8], Valid: true},
		EnvironmentID:         uuid.NullUUID{},
		UserSessionIssuerID:   uuid.NullUUID{},
		RemoteMcpServerID:     uuid.NullUUID{},
		TunneledMcpServerID:   uuid.NullUUID{},
		ToolsetID:             uuid.NullUUID{UUID: toolsetID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{},
		ToolVariationsGroupID: uuid.NullUUID{},
		Visibility:            visibility,
		NetworkAccessMode:     pgtype.Text{},
	})
	require.NoError(t, err)
	return server
}

func createHTTPToolDefinition(
	t *testing.T,
	db *pgxpool.Pool,
	projectID uuid.UUID,
	deploymentID uuid.UUID,
	toolURN urn.Tool,
) {
	t.Helper()

	_, err := deploymentsrepo.New(db).CreateOpenAPIv3ToolDefinition(t.Context(), deploymentsrepo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:           projectID,
		DeploymentID:        deploymentID,
		Openapiv3DocumentID: uuid.NullUUID{},
		ToolUrn:             toolURN,
		Name:                "valid_tool",
		UntruncatedName:     pgtype.Text{},
		Openapiv3Operation:  pgtype.Text{},
		Summary:             "Valid tool",
		Description:         "A valid test tool",
		Tags:                []string{},
		Confirm:             pgtype.Text{},
		ConfirmPrompt:       pgtype.Text{},
		XGram:               pgtype.Bool{},
		OriginalName:        pgtype.Text{},
		OriginalSummary:     pgtype.Text{},
		OriginalDescription: pgtype.Text{},
		Security:            []byte("[]"),
		HttpMethod:          "GET",
		Path:                "/valid",
		SchemaVersion:       "3.0.0",
		Schema:              []byte("{}"),
		HeaderSettings:      []byte("{}"),
		QuerySettings:       []byte("{}"),
		PathSettings:        []byte("{}"),
		ServerEnvVar:        "TEST_SERVER_URL",
		DefaultServerUrl:    pgtype.Text{},
		RequestContentType:  pgtype.Text{},
		ResponseFilter:      nil,
		ReadOnlyHint:        pgtype.Bool{},
		DestructiveHint:     pgtype.Bool{},
		IdempotentHint:      pgtype.Bool{},
		OpenWorldHint:       pgtype.Bool{},
	})
	require.NoError(t, err)
}

func createMCPToolset(
	t *testing.T,
	db *pgxpool.Pool,
	organizationID string,
	projectID uuid.UUID,
	slug string,
	toolURNs []urn.Tool,
	mcpEnabled bool,
) toolsetsrepo.Toolset {
	t.Helper()

	toolset, err := toolsetsrepo.New(db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{},
		McpEnabled:             mcpEnabled,
	})
	require.NoError(t, err)

	_, err = toolsetsrepo.New(db).CreateToolsetVersion(t.Context(), toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return toolset
}

func createCompletedDeployment(
	t *testing.T,
	db *pgxpool.Pool,
	organizationID string,
	projectID uuid.UUID,
) uuid.UUID {
	t.Helper()

	queries := deploymentsrepo.New(db)
	idempotencyKey := "test-" + uuid.NewString()
	_, err := queries.CreateDeployment(t.Context(), deploymentsrepo.CreateDeploymentParams{
		IdempotencyKey: idempotencyKey,
		UserID:         "test-user",
		OrganizationID: organizationID,
		ProjectID:      projectID,
		GithubRepo:     pgtype.Text{},
		GithubPr:       pgtype.Text{},
		GithubSha:      pgtype.Text{},
		ExternalID:     pgtype.Text{},
		ExternalUrl:    pgtype.Text{},
	})
	require.NoError(t, err)

	deployment, err := queries.GetDeploymentByIdempotencyKey(t.Context(), deploymentsrepo.GetDeploymentByIdempotencyKeyParams{
		IdempotencyKey: idempotencyKey,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	for _, status := range []string{"created", "pending", "completed"} {
		_, err = queries.TransitionDeployment(t.Context(), deploymentsrepo.TransitionDeploymentParams{
			DeploymentID: deployment.Deployment.ID,
			Status:       status,
			ProjectID:    projectID,
			Event:        "test",
			Message:      "test deployment status",
		})
		require.NoError(t, err)
	}

	return deployment.Deployment.ID
}
