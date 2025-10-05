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

func TestToolsetsService_UpdateToolset_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 3, "expected at least 3 tools from petstore")

	// Create a toolset with one tool
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Original Toolset",
		Description:            conv.Ptr("Original description"),
		ToolUrns:               []string{tools[0].ToolUrn.String()},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.Len(t, created.HTTPTools, 1, "should start with 1 HTTP tool")

	// Update the toolset with different tools
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   conv.Ptr("Updated Toolset"),
		Description:            conv.Ptr("Updated description"),
		DefaultEnvironmentSlug: nil,
		ToolUrns:               []string{tools[1].ToolUrn.String(), tools[2].ToolUrn.String()},
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpIsPublic:            nil,
		McpEnabled:             nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Updated Toolset", result.Name)
	require.Equal(t, "Updated description", *result.Description)
	require.Len(t, result.HTTPTools, 2, "should have 2 HTTP tools after update")
	require.Equal(t, string(created.Slug), string(result.Slug)) // Slug should remain the same

	// Verify the tool URNs were updated
	toolUrns := make([]string, len(result.HTTPTools))
	for i, tool := range result.HTTPTools {
		toolUrns[i] = tool.ToolUrn
	}
	require.ElementsMatch(t, []string{tools[1].ToolUrn.String(), tools[2].ToolUrn.String()}, toolUrns)
}

func TestToolsetsService_UpdateToolset_PartialUpdate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 1, "expected at least 1 tool from petstore")

	// Create a toolset first with a tool
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Original Toolset",
		Description:            conv.Ptr("Original description"),
		ToolUrns:               []string{tools[0].ToolUrn.String()},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Update only the name (ToolUrns is nil, so tools should remain unchanged)
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   conv.Ptr("Updated Name Only"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Updated Name Only", result.Name)
	require.Equal(t, "Original description", *result.Description)   // Should remain unchanged
	require.Len(t, result.HTTPTools, 1, "should still have 1 tool") // Should remain unchanged
	require.Equal(t, tools[0].ToolUrn.String(), result.HTTPTools[0].ToolUrn)
}

func TestToolsetsService_UpdateToolset_WithEnvironment(t *testing.T) {
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
		Name:           "Update Test Environment",
		Slug:           "update-test-env",
		Description:    pgtype.Text{String: "Update test environment", Valid: true},
	})
	require.NoError(t, err)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset for Env Update",
		Description:            nil,
		ToolUrns:               []string{},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Update with environment
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: (*types.Slug)(conv.Ptr("update-test-env")),
		ToolUrns:               nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "update-test-env", string(*result.DefaultEnvironmentSlug))
}

func TestToolsetsService_UpdateToolset_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   "non-existent-slug",
		Name:                   conv.Ptr("New Name"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolset not found")
}

func TestToolsetsService_UpdateToolset_InvalidEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset for Invalid Env",
		Description:            nil,
		ToolUrns:               []string{},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Try to update with non-existent environment
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: (*types.Slug)(conv.Ptr("non-existent-env")),
		ToolUrns:               nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "error finding environment")
}

func TestToolsetsService_UpdateToolset_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   "some-slug",
		Name:                   conv.Ptr("New Name"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_UpdateToolset_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   "some-slug",
		Name:                   conv.Ptr("New Name"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_UpdateToolset_EmptyToolUrns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools from petstore")

	// Create a toolset with tools
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset with Tools",
		Description:            nil,
		ToolUrns:               []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.Len(t, created.HTTPTools, 2, "should start with 2 tools")

	// Update to have empty tool URNs (remove all tools)
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               []string{},
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.HTTPTools, "should have no tools after clearing")
}

func TestToolsetsService_UpdateToolset_McpEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "MCP Toolset",
		Description:            conv.Ptr("Toolset for MCP testing"),
		ToolUrns:               []string{},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Update to enable MCP
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpIsPublic:            nil,
		McpEnabled:             conv.Ptr(true),
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, *result.McpEnabled)
}
