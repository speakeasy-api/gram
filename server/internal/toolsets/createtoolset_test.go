package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestToolsetsService_CreateToolset_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get the tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.Len(t, tools, 4, "expected 4 tools from petstore")

	// Extract tool URNs
	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	// Test creating a toolset with tools from the deployment
	result, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset",
		Description:            new("A test toolset"),
		ToolUrns:               toolUrns[:2], // Use first two tools
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Test Toolset", result.Name)
	require.Equal(t, "test-toolset", string(result.Slug))
	require.Equal(t, "A test toolset", *result.Description)
	require.Len(t, result.Tools, 2, "should have 2 HTTP tools")
	require.NotNil(t, result.ID)
	require.NotNil(t, result.CreatedAt)
	require.NotNil(t, result.UpdatedAt)

	// Verify the tools are correctly populated
	toolSetUrns := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		baseTool, err := conv.ToBaseTool(tool)
		require.NoError(t, err)
		toolSetUrns[i] = baseTool.ToolUrn
		require.NotEmpty(t, baseTool.ID)
		require.NotEmpty(t, baseTool.Name)
		require.NotEmpty(t, baseTool.ToolUrn)
		// Summary and Description may be empty depending on the OpenAPI spec
	}
	require.ElementsMatch(t, toolUrns[:2], toolSetUrns)
}

func TestToolsetsService_CreateToolset_WithDefaultEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get a tool from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.NotEmpty(t, tools, "expected tools from petstore")

	// Create an environment first
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create an environment in the test database
	envRepo := environmentsRepo.New(ti.conn)
	_, err = envRepo.CreateEnvironment(ctx, environmentsRepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Test Environment",
		Slug:           "test-env",
		Description:    pgtype.Text{String: "Test environment", Valid: true},
	})
	require.NoError(t, err)

	result, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset with Env",
		Description:            new("A test toolset with environment"),
		ToolUrns:               []string{tools[0].ToolUrn.String()}, // Use first tool from deployment
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: (*types.Slug)(new("test-env")),
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Test Toolset with Env", result.Name)
	require.Equal(t, "test-toolset-with-env", string(result.Slug))
	require.Equal(t, "test-env", string(*result.DefaultEnvironmentSlug))
	require.Len(t, result.Tools, 1, "should have 1 HTTP tool")
	baseTool, err := conv.ToBaseTool(result.Tools[0])
	require.NoError(t, err)
	require.Equal(t, tools[0].ToolUrn.String(), baseTool.ToolUrn)
}

func TestToolsetsService_CreateToolset_DuplicateSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools from petstore")

	// Create first toolset
	_, err = ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset",
		Description:            nil,
		ToolUrns:               []string{tools[0].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Try to create another with the same name (will generate same slug)
	_, err = ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset",
		Description:            nil,
		ToolUrns:               []string{tools[1].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolset slug already exists")
}

func TestToolsetsService_CreateToolset_InvalidEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	_, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: (*types.Slug)(new("non-existent-env")),
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "error finding environment")
}

func TestToolsetsService_CreateToolset_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_CreateToolset_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_CreateToolset_EmptyToolUrns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	result, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset Empty Tools",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Test Toolset Empty Tools", result.Name)
	require.Empty(t, result.Tools)
}
