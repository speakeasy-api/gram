package mcpmetadata_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestService_GetMcpMetadata_WithInstructions(t *testing.T) {
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
		Slug:                   "test-mcp-get",
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	instructions := "You have tools for searching the Test Hub. Use them wisely."
	docURL := "https://docs.example.com"

	// Set metadata first
	setPayload := &gen.SetMcpMetadataPayload{
		ToolsetSlug:              types.Slug(toolset.Slug),
		LogoAssetID:              nil,
		ExternalDocumentationURL: &docURL,
		Instructions:             &instructions,
		SessionToken:             nil,
		ProjectSlugInput:         nil,
	}

	_, err = ti.service.SetMcpMetadata(ctx, setPayload)
	require.NoError(t, err)

	// Now fetch it
	getPayload := &gen.GetMcpMetadataPayload{
		ToolsetSlug:      types.Slug(toolset.Slug),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}

	result, err := ti.service.GetMcpMetadata(ctx, getPayload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Metadata)

	require.NotEmpty(t, result.Metadata.ID)
	require.Equal(t, toolset.ID.String(), result.Metadata.ToolsetID)
	require.NotNil(t, result.Metadata.Instructions)
	require.Equal(t, instructions, *result.Metadata.Instructions)
	require.NotNil(t, result.Metadata.ExternalDocumentationURL)
	require.Equal(t, docURL, *result.Metadata.ExternalDocumentationURL)
	require.Nil(t, result.Metadata.LogoAssetID)
}

func TestService_GetMcpMetadata_WithoutInstructions(t *testing.T) {
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
		Slug:                   "test-mcp-get-no-instructions",
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	docURL := "https://docs.example.com"

	// Set metadata without instructions
	setPayload := &gen.SetMcpMetadataPayload{
		ToolsetSlug:              types.Slug(toolset.Slug),
		LogoAssetID:              nil,
		ExternalDocumentationURL: &docURL,
		Instructions:             nil,
		SessionToken:             nil,
		ProjectSlugInput:         nil,
	}

	_, err = ti.service.SetMcpMetadata(ctx, setPayload)
	require.NoError(t, err)

	// Now fetch it
	getPayload := &gen.GetMcpMetadataPayload{
		ToolsetSlug:      types.Slug(toolset.Slug),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}

	result, err := ti.service.GetMcpMetadata(ctx, getPayload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Metadata)

	require.NotEmpty(t, result.Metadata.ID)
	require.Equal(t, toolset.ID.String(), result.Metadata.ToolsetID)
	require.Nil(t, result.Metadata.Instructions)
	require.NotNil(t, result.Metadata.ExternalDocumentationURL)
	require.Equal(t, docURL, *result.Metadata.ExternalDocumentationURL)
}
