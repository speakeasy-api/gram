package toolsets_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
)

func TestToolsetsService_UpdateToolset_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Original Toolset",
		Description:            conv.Ptr("Original description"),
		HTTPToolNames:          []string{"listPets"},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Update the toolset
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   conv.Ptr("Updated Toolset"),
		Description:            conv.Ptr("Updated description"),
		DefaultEnvironmentSlug: nil,
		HTTPToolNames:          []string{"listPets", "createPets", "deletePet"},
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
	require.Empty(t, result.HTTPTools)
	require.Equal(t, string(created.Slug), string(result.Slug)) // Slug should remain the same
}

func TestToolsetsService_UpdateToolset_PartialUpdate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Original Toolset",
		Description:            conv.Ptr("Original description"),
		HTTPToolNames:          []string{"listPets"},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Update only the name
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   conv.Ptr("Updated Name Only"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		HTTPToolNames:          nil,
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
	require.Equal(t, "Original description", *result.Description) // Should remain unchanged
	require.Empty(t, result.HTTPTools)                            // Should remain unchanged
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
		HTTPToolNames:          []string{"showPetById"},
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
		HTTPToolNames:          nil,
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
		HTTPToolNames:          nil,
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
		HTTPToolNames:          []string{"showPetById"},
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
		HTTPToolNames:          nil,
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
		HTTPToolNames:          nil,
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
		HTTPToolNames:          nil,
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

func TestToolsetsService_UpdateToolset_EmptyHTTPToolNames(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset with Tools",
		Description:            nil,
		HTTPToolNames:          []string{"listPets", "createPets"},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Update to have empty HTTP tool names
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		HTTPToolNames:          []string{},
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.HTTPTools)
}

func TestToolsetsService_UpdateToolset_McpEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "MCP Toolset",
		Description:            conv.Ptr("Toolset for MCP testing"),
		HTTPToolNames:          []string{"listPets"},
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
		HTTPToolNames:          nil,
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
