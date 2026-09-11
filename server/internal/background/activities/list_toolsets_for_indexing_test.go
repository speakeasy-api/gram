package activities_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	ragrepo "github.com/speakeasy-api/gram/server/internal/rag/repo"
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

	validToolset := createMCPToolset(t, db, organizationID, project.ID, "valid", []urn.Tool{validToolURN})
	createMCPToolset(t, db, organizationID, project.ID, "dangling", []urn.Tool{
		urn.NewTool(urn.ToolKindHTTP, "test-api", "deleted-tool"),
	})
	createMCPToolset(t, db, organizationID, project.ID, "empty", []urn.Tool{})

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
	createMCPToolset(t, db, organizationID, project.ID, "proxy-only", []urn.Tool{proxyToolURN})
	mixedToolset := createMCPToolset(t, db, organizationID, project.ID, "mixed-proxy", []urn.Tool{validToolURN, proxyToolURN})
	mixedVersion, err := toolsetsrepo.New(db).GetLatestToolsetVersion(ctx, mixedToolset.ID)
	require.NoError(t, err)
	require.Equal(t, []urn.Tool{validToolURN, proxyToolURN}, mixedVersion.ToolUrns)

	activity := activities.NewListToolsetsForIndexing(db)
	targets, err := activity.Do(ctx, activities.ListToolsetsForIndexingInput{RotationSeed: 1, ScanLimit: 100})
	require.NoError(t, err)
	require.Equal(t, []activities.ToolsetIndexTarget{{
		ProjectID:     project.ID,
		ToolsetSlug:   "valid",
		IndexRevision: fmt.Sprintf("1:%s", deploymentID),
	}}, targets)

	_, err = ragrepo.New(db).InsertToolsetEmbedding(ctx, ragrepo.InsertToolsetEmbeddingParams{
		ProjectID:      project.ID,
		ToolsetID:      validToolset.ID,
		ToolsetVersion: 1,
		EntryKey:       "tools:valid",
		EmbeddingModel: "test-model",
		Embedding1536:  pgvector.NewVector(make([]float32, 1536)),
		Payload:        []byte("{}"),
		Tags:           []string{},
	})
	require.NoError(t, err)

	targets, err = activity.Do(ctx, activities.ListToolsetsForIndexingInput{RotationSeed: 2, ScanLimit: 100})
	require.NoError(t, err)
	require.Empty(t, targets)

	newDeploymentID := createCompletedDeployment(t, db, organizationID, project.ID)
	createHTTPToolDefinition(t, db, project.ID, newDeploymentID, validToolURN)
	targets, err = activity.Do(ctx, activities.ListToolsetsForIndexingInput{RotationSeed: 3, ScanLimit: 100})
	require.NoError(t, err)
	require.Equal(t, []activities.ToolsetIndexTarget{{
		ProjectID:     project.ID,
		ToolsetSlug:   "valid",
		IndexRevision: fmt.Sprintf("1:%s", newDeploymentID),
	}}, targets)
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
		McpEnabled:             true,
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
