package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/gen/toolsets"
	"github.com/speakeasy-api/gram/gen/types"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	environmentsRepo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/testenv/testrepo"
)

func TestToolsetsService_GetToolset_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools from petstore")

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset",
		Description:            conv.Ptr("A test toolset"),
		HTTPToolNames:          []string{tools[0].Name, tools[1].Name},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Get the toolset
	result, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, created.ID, result.ID)
	require.Equal(t, "Test Toolset", result.Name)
	require.Equal(t, "test-toolset", string(result.Slug))
	require.Equal(t, "A test toolset", *result.Description)
	require.Len(t, result.HTTPTools, 2, "should have 2 HTTP tools")
	
	// Verify tools are properly populated
	for _, tool := range result.HTTPTools {
		require.NotEmpty(t, tool.ID)
		require.NotEmpty(t, tool.Name)
		// Summary and Description may be empty depending on the OpenAPI spec
	}
	
	require.NotNil(t, result.CreatedAt)
	require.NotNil(t, result.UpdatedAt)
}

func TestToolsetsService_GetToolset_WithEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create an environment first
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	envRepo := environmentsRepo.New(ti.conn)
	_, err := envRepo.CreateEnvironment(ctx, environmentsRepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Get Test Environment",
		Slug:           "get-test-env",
		Description:    pgtype.Text{String: "Get test environment", Valid: true},
	})
	require.NoError(t, err)

	// Create a toolset with environment
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset with Env",
		Description:            conv.Ptr("A toolset with environment"),
		HTTPToolNames:          []string{"showPetById"},
		DefaultEnvironmentSlug: (*types.Slug)(conv.Ptr("get-test-env")),
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Get the toolset
	result, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "get-test-env", string(*result.DefaultEnvironmentSlug))
}

func TestToolsetsService_GetToolset_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Try to get a non-existent toolset
	_, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{
		Slug:             "non-existent-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestToolsetsService_GetToolset_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{
		Slug:             "some-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_GetToolset_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{
		Slug:             "some-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_GetToolset_VerifyAllFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset with all fields
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Complete Toolset",
		Description:            conv.Ptr("A complete toolset with all fields"),
		HTTPToolNames:          []string{"listPets", "createPets", "showPetById", "deletePet"},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Get the toolset and verify all fields
	result, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify basic fields
	require.Equal(t, created.ID, result.ID)
	require.Equal(t, "Complete Toolset", result.Name)
	require.Equal(t, "complete-toolset", string(result.Slug))
	require.Equal(t, "A complete toolset with all fields", *result.Description)
	
	// Verify HTTP tools
	require.Empty(t, result.HTTPTools)
	
	// Verify timestamps
	require.NotNil(t, result.CreatedAt)
	require.NotNil(t, result.UpdatedAt)
	require.Equal(t, created.CreatedAt, result.CreatedAt)
	require.Equal(t, created.UpdatedAt, result.UpdatedAt)

	// Verify organization and project IDs
	require.Equal(t, created.OrganizationID, result.OrganizationID)
	require.Equal(t, created.ProjectID, result.ProjectID)
}