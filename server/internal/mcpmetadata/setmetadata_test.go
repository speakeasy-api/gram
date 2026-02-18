package mcpmetadata_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	assets_repo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestService_SetMcpMetadata(t *testing.T) {
	t.Parallel()

	t.Run("creates metadata for toolset", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-mcp",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		payload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              types.Slug(toolset.Slug),
			LogoAssetID:              nil,
			ExternalDocumentationURL: new("https://docs.example.com"),
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		result, err := ti.service.SetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.NotEmpty(t, result.ID)
		require.Equal(t, toolset.ID.String(), result.ToolsetID)
		require.NotNil(t, result.ExternalDocumentationURL)
		require.Equal(t, "https://docs.example.com", *result.ExternalDocumentationURL)
		require.Nil(t, result.LogoAssetID)
	})

	t.Run("updates existing metadata", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-mcp-update",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		firstPayload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              types.Slug(toolset.Slug),
			LogoAssetID:              nil,
			ExternalDocumentationURL: new("https://docs.example.com/v1"),
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		firstResult, err := ti.service.SetMcpMetadata(ctx, firstPayload)
		require.NoError(t, err)
		require.NotNil(t, firstResult)

		secondPayload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              types.Slug(toolset.Slug),
			LogoAssetID:              nil,
			ExternalDocumentationURL: new("https://docs.example.com/v2"),
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		secondResult, err := ti.service.SetMcpMetadata(ctx, secondPayload)
		require.NoError(t, err)
		require.NotNil(t, secondResult)

		require.Equal(t, firstResult.ID, secondResult.ID)
		require.Equal(t, toolset.ID.String(), secondResult.ToolsetID)
		require.NotNil(t, secondResult.ExternalDocumentationURL)
		require.Equal(t, "https://docs.example.com/v2", *secondResult.ExternalDocumentationURL)
	})

	t.Run("sets logo asset ID", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-mcp-logo",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		assetsRepo := assets_repo.New(ti.conn)
		asset, err := assetsRepo.CreateAsset(ctx, assets_repo.CreateAssetParams{
			Name:          "test-logo.png",
			Url:           "https://example.com/logo.png",
			ProjectID:     *authCtx.ProjectID,
			Sha256:        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			Kind:          "image",
			ContentType:   "image/png",
			ContentLength: 1024,
		})
		require.NoError(t, err)

		logoAssetID := asset.ID.String()

		payload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              types.Slug(toolset.Slug),
			LogoAssetID:              &logoAssetID,
			ExternalDocumentationURL: nil,
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		result, err := ti.service.SetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.NotEmpty(t, result.ID)
		require.Equal(t, toolset.ID.String(), result.ToolsetID)
		require.NotNil(t, result.LogoAssetID)
		require.Equal(t, logoAssetID, *result.LogoAssetID)
		require.Nil(t, result.ExternalDocumentationURL)
	})

	t.Run("sets server instructions", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-mcp-instructions",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		instructions := "You have tools for searching the Test Hub. Use them wisely."

		payload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              types.Slug(toolset.Slug),
			LogoAssetID:              nil,
			ExternalDocumentationURL: nil,
			Instructions:             &instructions,
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		result, err := ti.service.SetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.NotEmpty(t, result.ID)
		require.Equal(t, toolset.ID.String(), result.ToolsetID)
		require.NotNil(t, result.Instructions)
		require.Equal(t, instructions, *result.Instructions)
		require.Nil(t, result.LogoAssetID)
		require.Nil(t, result.ExternalDocumentationURL)
	})
}
