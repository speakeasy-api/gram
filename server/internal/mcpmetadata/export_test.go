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

func TestService_ExportMcpMetadata_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a toolset with MCP enabled
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test Export Toolset",
		Slug:                   "test-export",
		Description:            conv.ToPGText("A test toolset for export"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "test-export-mcp", Valid: true},
		McpEnabled:             true,
	})
	require.NoError(t, err, "create toolset")

	// Export the MCP metadata
	result, err := ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		ToolsetSlug: types.Slug(toolset.Slug),
	})
	require.NoError(t, err, "export mcp metadata")
	require.NotNil(t, result, "result should not be nil")

	// Verify the export structure
	require.Equal(t, toolset.Name, result.Name, "name should match")
	require.Equal(t, toolset.Slug, result.Slug, "slug should match")
	require.Contains(t, result.ServerURL, toolset.McpSlug.String, "server URL should contain MCP slug")

	// Verify authentication info
	require.NotNil(t, result.Authentication, "authentication should not be nil")

	// Verify tools list (should be empty for new toolset)
	require.NotNil(t, result.Tools, "tools should not be nil")
}

func TestService_ExportMcpMetadata_ToolsetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	// Try to export non-existent toolset
	_, err := ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		ToolsetSlug: types.Slug("non-existent-toolset"),
	})
	require.Error(t, err, "should return error for non-existent toolset")
	require.Contains(t, err.Error(), "toolset not found", "error message should indicate toolset not found")
}

func TestService_ExportMcpMetadata_McpNotEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a toolset with MCP disabled
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test Disabled MCP Toolset",
		Slug:                   "test-disabled-mcp",
		Description:            conv.ToPGText("A test toolset with MCP disabled"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err, "create toolset")

	// Try to export - should fail because MCP is not enabled
	_, err = ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		ToolsetSlug: types.Slug(toolset.Slug),
	})
	require.Error(t, err, "should return error for toolset with MCP disabled")
	require.Contains(t, err.Error(), "MCP is not enabled", "error message should indicate MCP is not enabled")
}

func TestService_ExportMcpMetadata_WithMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a toolset with MCP enabled
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test Export With Metadata",
		Slug:                   "test-export-metadata",
		Description:            conv.ToPGText("A toolset with metadata"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "test-meta-mcp", Valid: true},
		McpEnabled:             true,
	})
	require.NoError(t, err, "create toolset")

	// Set MCP metadata
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:              types.Slug(toolset.Slug),
		ExternalDocumentationURL: conv.Ptr("https://docs.example.com"),
		Instructions:             conv.Ptr("Use this server to interact with our API."),
	})
	require.NoError(t, err, "set mcp metadata")

	// Export the MCP metadata
	result, err := ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		ToolsetSlug: types.Slug(toolset.Slug),
	})
	require.NoError(t, err, "export mcp metadata")

	// Verify metadata is included in export
	require.NotNil(t, result.DocumentationURL, "documentation URL should not be nil")
	require.Equal(t, "https://docs.example.com", *result.DocumentationURL, "documentation URL should match")
	require.NotNil(t, result.Instructions, "instructions should not be nil")
	require.Equal(t, "Use this server to interact with our API.", *result.Instructions, "instructions should match")
}
