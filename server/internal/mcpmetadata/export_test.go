package mcpmetadata_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestService_ExportMcpMetadata_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a toolset published at /mcp/test-export-mcp via its wrapper.
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test Export Toolset",
		Slug:                   "test-export",
		Description:            conv.ToPGText("A test toolset for export"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err, "create toolset")
	createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{
		toolsetID:    uuid.NullUUID{UUID: toolset.ID, Valid: true},
		endpointSlug: "test-export-mcp",
	})

	// Export the MCP metadata using the endpoint slug
	result, err := ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		McpSlug: types.Slug("test-export-mcp"),
	})
	require.NoError(t, err, "export mcp metadata")
	require.NotNil(t, result, "result should not be nil")

	// Verify the export structure
	require.Equal(t, toolset.Name, result.Name, "name should match")
	require.Equal(t, toolset.Slug, result.Slug, "slug should match")
	require.Contains(t, result.ServerURL, "test-export-mcp", "server URL should contain the endpoint slug")

	// Verify authentication info
	require.NotNil(t, result.Authentication, "authentication should not be nil")

	// Verify tools list (should be empty for new toolset)
	require.NotNil(t, result.Tools, "tools should not be nil")
}

func TestService_ExportMcpMetadata_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	// Try to export non-existent MCP server
	_, err := ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		McpSlug: types.Slug("non-existent-mcp"),
	})
	require.Error(t, err, "should return error for non-existent MCP server")
	require.Contains(t, err.Error(), "MCP server not found", "error message should indicate MCP server not found")
}

func TestService_ExportMcpMetadata_McpNotEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a toolset whose wrapper is disabled — addressable but unavailable.
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test Disabled MCP Toolset",
		Slug:                   "test-disabled-mcp",
		Description:            conv.ToPGText("A test toolset with MCP disabled"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err, "create toolset")
	createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{
		toolsetID:    uuid.NullUUID{UUID: toolset.ID, Valid: true},
		endpointSlug: "disabled-mcp-slug",
		visibility:   mcpservers.VisibilityDisabled,
	})

	// Try to export - should fail because the wrapper is disabled
	_, err = ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		McpSlug: types.Slug("disabled-mcp-slug"),
	})
	require.Error(t, err, "should return error for toolset with MCP disabled")
	require.Contains(t, err.Error(), "MCP server not found", "error should indicate server not found")
}

func TestService_ExportMcpMetadata_WithMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a toolset published at /mcp/test-meta-mcp via its wrapper.
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test Export With Metadata",
		Slug:                   "test-export-metadata",
		Description:            conv.ToPGText("A toolset with metadata"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err, "create toolset")
	createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{
		toolsetID:    uuid.NullUUID{UUID: toolset.ID, Valid: true},
		endpointSlug: "test-meta-mcp",
	})

	// Set MCP metadata (uses toolset slug)
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
		ExternalDocumentationURL: new("https://docs.example.com"),
		Instructions:             new("Use this server to interact with our API."),
	})
	require.NoError(t, err, "set mcp metadata")

	// Export the MCP metadata using the endpoint slug
	result, err := ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		McpSlug: types.Slug("test-meta-mcp"),
	})
	require.NoError(t, err, "export mcp metadata")

	// Verify metadata is included in export
	require.NotNil(t, result.DocumentationURL, "documentation URL should not be nil")
	require.Equal(t, "https://docs.example.com", *result.DocumentationURL, "documentation URL should match")
	require.NotNil(t, result.Instructions, "instructions should not be nil")
	require.Equal(t, "Use this server to interact with our API.", *result.Instructions, "instructions should match")
}

func TestService_ExportMcpMetadata_WithCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)
	domainsRepo := customdomains_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a custom domain for the organization
	customDomain, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "mcp.example.com",
		IngressName:    pgtype.Text{String: "test-ingress", Valid: true},
		CertSecretName: pgtype.Text{String: "test-cert", Valid: true},
		IpAllowlist:    []string{},
	})
	require.NoError(t, err, "create custom domain")

	// Activate and verify the custom domain
	customDomain, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             customDomain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "test-ingress", Valid: true},
		CertSecretName: pgtype.Text{String: "test-cert", Valid: true},
	})
	require.NoError(t, err, "activate custom domain")

	// Create a toolset published on the custom domain via its wrapper endpoint.
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test Custom Domain Export",
		Slug:                   "test-custom-domain",
		Description:            conv.ToPGText("A toolset with custom domain"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err, "create toolset")
	createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{
		toolsetID:      uuid.NullUUID{UUID: toolset.ID, Valid: true},
		endpointSlug:   "custom-domain-mcp",
		customDomainID: uuid.NullUUID{UUID: customDomain.ID, Valid: true},
	})

	// Export the MCP metadata
	result, err := ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		McpSlug: types.Slug("custom-domain-mcp"),
	})
	require.NoError(t, err, "export mcp metadata")

	// Verify the server URL uses the custom domain
	require.Contains(t, result.ServerURL, customDomain.Domain, "server URL should contain custom domain")
	require.Equal(t, "https://mcp.example.com/mcp/custom-domain-mcp", result.ServerURL, "server URL should use custom domain")
}
