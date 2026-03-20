package mcpmetadata_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environments_repo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestService_DetachMcpEnvironment(t *testing.T) {
	t.Parallel()

	t.Run("clears default environment from metadata", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)
		envRepo := environments_repo.New(ti.conn)
		mcpRepo := mcpmetadata_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-detach",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		// Create an environment
		env, err := envRepo.CreateEnvironment(ctx, environments_repo.CreateEnvironmentParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			Name:           "Test Env",
			Slug:           "test-env",
			Description:    conv.ToPGText("A test environment"),
		})
		require.NoError(t, err)

		// Attach the environment via setMcpMetadata
		envID := env.ID.String()
		_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
			ToolsetSlug:          types.Slug(toolset.Slug),
			DefaultEnvironmentID: &envID,
			SessionToken:         nil,
			ProjectSlugInput:     nil,
		})
		require.NoError(t, err)

		// Verify it's attached
		metadata, err := mcpRepo.GetMetadataForToolset(ctx, toolset.ID)
		require.NoError(t, err)
		require.True(t, metadata.DefaultEnvironmentID.Valid)
		require.Equal(t, env.ID, metadata.DefaultEnvironmentID.UUID)

		// Detach the environment
		err = ti.service.DetachMcpEnvironment(ctx, &gen.DetachMcpEnvironmentPayload{
			ToolsetSlug:      types.Slug(toolset.Slug),
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)

		// Verify it's detached
		metadata, err = mcpRepo.GetMetadataForToolset(ctx, toolset.ID)
		require.NoError(t, err)
		require.False(t, metadata.DefaultEnvironmentID.Valid)
	})

	t.Run("preserves environment configs after detach", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)
		envRepo := environments_repo.New(ti.conn)
		mcpRepo := mcpmetadata_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-detach-configs",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		// Create an environment
		env, err := envRepo.CreateEnvironment(ctx, environments_repo.CreateEnvironmentParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			Name:           "Test Env",
			Slug:           "test-env-configs",
			Description:    conv.ToPGText("A test environment"),
		})
		require.NoError(t, err)

		// Attach the environment and add environment configs
		envID := env.ID.String()
		_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
			ToolsetSlug:          types.Slug(toolset.Slug),
			DefaultEnvironmentID: &envID,
			EnvironmentConfigs: []*types.McpEnvironmentConfigInput{
				{VariableName: "API_KEY", ProvidedBy: "system"},
				{VariableName: "SECRET", ProvidedBy: "user"},
			},
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)

		// Verify configs exist
		metadataBefore, err := mcpRepo.GetMetadataForToolset(ctx, toolset.ID)
		require.NoError(t, err)
		configs, err := mcpRepo.ListEnvironmentConfigs(ctx, metadataBefore.ID)
		require.NoError(t, err)
		require.Len(t, configs, 2)

		// Detach the environment
		err = ti.service.DetachMcpEnvironment(ctx, &gen.DetachMcpEnvironmentPayload{
			ToolsetSlug:      types.Slug(toolset.Slug),
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)

		// Verify environment is detached
		metadata, err := mcpRepo.GetMetadataForToolset(ctx, toolset.ID)
		require.NoError(t, err)
		require.False(t, metadata.DefaultEnvironmentID.Valid)

		// Verify configs are still intact
		configs, err = mcpRepo.ListEnvironmentConfigs(ctx, metadata.ID)
		require.NoError(t, err)
		require.Len(t, configs, 2)
	})
}
