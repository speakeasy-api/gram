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

func TestToolsetsService_GetToolset_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools from petstore")

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Test Toolset",
		Description:            conv.Ptr("A test toolset"),
		ToolUrns:               []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()},
		ResourceUrns:           nil,
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
	require.Len(t, result.Tools, 2, "should have 2 HTTP tools")

	// Verify tools are properly populated
	for _, tool := range result.Tools {
		baseTool, err := conv.ToBaseTool(tool)
		require.NoError(t, err)
		require.NotEmpty(t, baseTool.ID)
		require.NotEmpty(t, baseTool.Name)
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
		ToolUrns:               []string{},
		ResourceUrns:           nil,
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
		ToolUrns:               []string{},
		ResourceUrns:           nil,
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
	require.Empty(t, result.Tools)

	// Verify timestamps
	require.NotNil(t, result.CreatedAt)
	require.NotNil(t, result.UpdatedAt)
	require.Equal(t, created.CreatedAt, result.CreatedAt)
	require.Equal(t, created.UpdatedAt, result.UpdatedAt)

	// Verify organization and project IDs
	require.Equal(t, created.OrganizationID, result.OrganizationID)
	require.Equal(t, created.ProjectID, result.ProjectID)
}

func TestToolsetsService_GetToolset_WithFunctionTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with function tools
	dep := createFunctionsDeployment(t, ctx, ti)

	// Get function tools from the deployment
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.GreaterOrEqual(t, len(functionTools), 1, "expected at least 1 function tool")

	// Create a toolset with function tools
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Function Toolset",
		Description:            conv.Ptr("A toolset with function tools"),
		ToolUrns:               []string{functionTools[0].ToolUrn.String()},
		ResourceUrns:           nil,
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
	require.Equal(t, "Function Toolset", result.Name)
	require.Len(t, result.Tools, 1, "should have 1 function tool")

	// Verify the function tool is properly populated
	tool := result.Tools[0]
	require.NotNil(t, tool.FunctionToolDefinition, "should be a function tool")
	require.NotEmpty(t, tool.FunctionToolDefinition.ID)
	require.NotEmpty(t, tool.FunctionToolDefinition.Name)
	require.NotEmpty(t, tool.FunctionToolDefinition.DeploymentID)
	require.NotNil(t, tool.FunctionToolDefinition.Schema)
}

func TestToolsetsService_GetToolset_WithResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with functions that include resources
	dep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get resources from the deployment
	repo := testrepo.New(ti.conn)
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Create toolset with resources
	resourceUrns := make([]string, len(resources))
	for i, r := range resources {
		resourceUrns[i] = r.ResourceUrn.String()
	}

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset With Resources",
		Description:            conv.Ptr("A toolset that includes resources"),
		ToolUrns:               []string{},
		ResourceUrns:           resourceUrns,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Get the toolset and verify resources are populated
	result, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, created.ID, result.ID)
	require.Len(t, result.Resources, 3, "resources should be populated")
	require.Len(t, result.ResourceUrns, 3, "resource URNs should be populated")

	// Verify resource names match what we expect from the manifest
	resourceNames := make(map[string]bool)
	for _, r := range result.Resources {
		require.NotNil(t, r.FunctionResourceDefinition, "function resource definition should not be nil")
		resourceNames[r.FunctionResourceDefinition.Name] = true
	}
	require.True(t, resourceNames["user_guide"], "user_guide resource should be present")
	require.True(t, resourceNames["api_reference"], "api_reference resource should be present")
	require.True(t, resourceNames["data_source"], "data_source resource should be present")
}

func TestToolsetsService_GetToolset_MixedToolsAndResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with OpenAPI (for HTTP tools)
	petstoreDep := createPetstoreDeployment(t, ctx, ti)

	// Create deployment with functions that include resources
	resourceDep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get tools from petstore
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(petstoreDep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools from petstore")

	// Get resources from functions deployment
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(resourceDep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Create toolset with both tools and resources
	toolUrns := []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()}
	resourceUrns := make([]string, len(resources))
	for i, r := range resources {
		resourceUrns[i] = r.ResourceUrn.String()
	}

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Mixed Toolset",
		Description:            conv.Ptr("A toolset with both tools and resources"),
		ToolUrns:               toolUrns,
		ResourceUrns:           resourceUrns,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Get the toolset and verify both tools and resources are populated
	result, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, created.ID, result.ID)
	require.Len(t, result.ToolUrns, 2, "tool URNs should be populated")
	require.Len(t, result.Resources, 3, "resources should be populated")
	require.Len(t, result.ResourceUrns, 3, "resource URNs should be populated")

	// Note: Tools array may or may not be populated depending on tool type
	// The important check is that ToolUrns are present

	// Verify resource details
	for _, r := range result.Resources {
		require.NotNil(t, r.FunctionResourceDefinition, "function resource definition should not be nil")
		require.NotEmpty(t, r.FunctionResourceDefinition.ID, "resource ID should not be empty")
		require.NotEmpty(t, r.FunctionResourceDefinition.Name, "resource name should not be empty")
		require.NotEmpty(t, r.FunctionResourceDefinition.URI, "resource URI should not be empty")
		require.NotEmpty(t, r.FunctionResourceDefinition.Description, "resource description should not be empty")
		require.NotEmpty(t, r.FunctionResourceDefinition.CreatedAt, "resource createdAt should not be empty")
		require.NotEmpty(t, r.FunctionResourceDefinition.UpdatedAt, "resource updatedAt should not be empty")
	}
}

func TestToolsetsService_GetToolset_VerifyResourceDetails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with functions that include resources
	dep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get resources from the deployment
	repo := testrepo.New(ti.conn)
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Find the api_reference resource (which has variables)
	var apiRefUrn string
	for _, r := range resources {
		if r.Name == "api_reference" {
			apiRefUrn = r.ResourceUrn.String()
			break
		}
	}
	require.NotEmpty(t, apiRefUrn, "api_reference resource should exist")

	// Create toolset with just the api_reference resource
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Resource Details Toolset",
		Description:            conv.Ptr("Toolset to verify resource details"),
		ToolUrns:               []string{},
		ResourceUrns:           []string{apiRefUrn},
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
	require.Len(t, result.Resources, 1, "should have 1 resource")

	// Verify the api_reference resource details
	resource := result.Resources[0]
	require.NotNil(t, resource.FunctionResourceDefinition, "function resource definition should not be nil")
	def := resource.FunctionResourceDefinition
	require.Equal(t, "api_reference", def.Name)
	require.Equal(t, "file:///docs/api-reference.md", def.URI)
	require.Equal(t, "API reference documentation", def.Description)
	require.Equal(t, "API Reference", *def.Title)
	require.Equal(t, "text/markdown", *def.MimeType)

	// Verify variables are present
	require.NotNil(t, def.Variables, "variables should not be nil")
}

func TestToolsetsService_GetToolset_WithMultipleFunctionToolsAndResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with functions that include both tools and resources
	dep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get function tools from the deployment
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.GreaterOrEqual(t, len(functionTools), 2, "expected at least 2 function tools")

	// Get resources from the deployment
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Create toolset with both function tools and resources
	toolUrns := []string{functionTools[0].ToolUrn.String(), functionTools[1].ToolUrn.String()}
	resourceUrns := make([]string, len(resources))
	for i, r := range resources {
		resourceUrns[i] = r.ResourceUrn.String()
	}

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Function Tools and Resources",
		Description:            conv.Ptr("A toolset with function tools and resources"),
		ToolUrns:               toolUrns,
		ResourceUrns:           resourceUrns,
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
	require.Len(t, result.Tools, 2, "should have 2 function tools")
	require.Len(t, result.ToolUrns, 2, "should have 2 tool URNs")
	require.Len(t, result.Resources, 3, "should have 3 resources")
	require.Len(t, result.ResourceUrns, 3, "should have 3 resource URNs")

	// Verify function tools are properly populated
	for _, tool := range result.Tools {
		require.NotNil(t, tool.FunctionToolDefinition, "should be a function tool")
		require.NotEmpty(t, tool.FunctionToolDefinition.Name)
	}

	// Verify resources are properly populated
	resourceNames := make(map[string]bool)
	for _, r := range result.Resources {
		require.NotNil(t, r.FunctionResourceDefinition, "function resource definition should not be nil")
		resourceNames[r.FunctionResourceDefinition.Name] = true
		require.NotEmpty(t, r.FunctionResourceDefinition.Name)
		require.NotEmpty(t, r.FunctionResourceDefinition.URI)
	}
	require.True(t, resourceNames["user_guide"])
	require.True(t, resourceNames["api_reference"])
	require.True(t, resourceNames["data_source"])
}
