package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestToolsetsService_ListToolsets_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 4, "expected at least 4 tools from petstore")

	// Create a few toolsets
	toolset1, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "First Toolset",
		Description:            new("First test toolset"),
		ToolUrns:               []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	toolset2, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Second Toolset",
		Description:            new("Second test toolset"),
		ToolUrns:               []string{tools[2].ToolUrn.String(), tools[3].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// List toolsets
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 2)

	// Check that both toolsets are present and have HTTP tools populated
	toolsetIDs := make(map[string]bool)
	for _, ts := range result.Toolsets {
		toolsetIDs[ts.ID] = true
		require.NotEmpty(t, ts.Tools, "HTTP tools should be populated")
		require.Len(t, ts.Tools, 2, "each toolset should have 2 tools")
	}
	require.True(t, toolsetIDs[toolset1.ID])
	require.True(t, toolsetIDs[toolset2.ID])
}

func TestToolsetsService_ListToolsets_EmptyList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// List toolsets when none exist
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Toolsets)
}

func TestToolsetsService_ListToolsets_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_ListToolsets_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_ListToolsets_VerifyDetails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset with specific details
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Detailed Toolset",
		Description:            new("A toolset with details"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// List toolsets and verify details
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 1)

	toolset := result.Toolsets[0]
	require.Equal(t, created.ID, toolset.ID)
	require.Equal(t, "Detailed Toolset", toolset.Name)
	require.Equal(t, "detailed-toolset", string(toolset.Slug))
	require.Equal(t, "A toolset with details", *toolset.Description)
	require.Empty(t, toolset.Tools)
	require.NotNil(t, toolset.CreatedAt)
	require.NotNil(t, toolset.UpdatedAt)
}

func TestToolsetsService_ListToolsets_WithResources(t *testing.T) {
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

	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset With Resources",
		Description:            new("A toolset that includes resources"),
		ToolUrns:               []string{},
		ResourceUrns:           resourceUrns,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// List toolsets and verify resources are populated
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 1)

	ts := result.Toolsets[0]
	require.Equal(t, toolset.ID, ts.ID)
	require.Len(t, ts.Resources, 3, "resources should be populated")
	require.Len(t, ts.ResourceUrns, 3, "resource URNs should be populated")

	// Verify resource names match what we expect from the manifest
	resourceNames := make(map[string]bool)
	for _, r := range ts.Resources {
		resourceNames[r.Name] = true
	}
	require.True(t, resourceNames["user_guide"], "user_guide resource should be present")
	require.True(t, resourceNames["api_reference"], "api_reference resource should be present")
	require.True(t, resourceNames["data_source"], "data_source resource should be present")
}

func TestToolsetsService_ListToolsets_MixedToolsAndResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with OpenAPI (for tools)
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

	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Mixed Toolset",
		Description:            new("A toolset with both tools and resources"),
		ToolUrns:               toolUrns,
		ResourceUrns:           resourceUrns,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// List toolsets and verify both tools and resources are populated
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 1)

	ts := result.Toolsets[0]
	require.Equal(t, toolset.ID, ts.ID)
	require.Len(t, ts.ToolUrns, 2, "tool URNs should be populated")
	require.Len(t, ts.Resources, 3, "resources should be populated")
	require.Len(t, ts.ResourceUrns, 3, "resource URNs should be populated")

	// Note: Tools field may or may not be populated depending on tool type
	// The important check is that ToolUrns are present
}

func TestToolsetsService_ListToolsets_MultipleToolsetsWithResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with functions that include resources
	dep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get resources from the deployment
	repo := testrepo.New(ti.conn)
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Create first toolset with first 2 resources
	toolset1, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "First Resource Toolset",
		Description:            new("First toolset with resources"),
		ToolUrns:               []string{},
		ResourceUrns:           []string{resources[0].ResourceUrn.String(), resources[1].ResourceUrn.String()},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Create second toolset with last resource
	toolset2, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Second Resource Toolset",
		Description:            new("Second toolset with resources"),
		ToolUrns:               []string{},
		ResourceUrns:           []string{resources[2].ResourceUrn.String()},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// List toolsets
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 2)

	// Verify both toolsets have resources populated correctly
	toolsetMap := make(map[string]*types.ToolsetEntry)
	for _, ts := range result.Toolsets {
		toolsetMap[ts.ID] = ts
	}

	ts1 := toolsetMap[toolset1.ID]
	require.NotNil(t, ts1)
	require.Len(t, ts1.Resources, 2, "first toolset should have 2 resources")
	require.Len(t, ts1.ResourceUrns, 2, "first toolset should have 2 resource URNs")

	ts2 := toolsetMap[toolset2.ID]
	require.NotNil(t, ts2)
	require.Len(t, ts2.Resources, 1, "second toolset should have 1 resource")
	require.Len(t, ts2.ResourceUrns, 1, "second toolset should have 1 resource URN")
}
