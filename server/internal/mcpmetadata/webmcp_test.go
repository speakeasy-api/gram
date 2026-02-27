package mcpmetadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestSetMcpMetadata_WebMCPEnabled_Roundtrip(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := ti.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "WebMCP Roundtrip Test",
		Slug:                   "webmcp-roundtrip",
		Description:            conv.ToPGText("A toolset for testing webmcp_enabled round-trip"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	// Set metadata with webmcp_enabled = true
	enabled := true
	result, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:      types.Slug(toolset.Slug),
		WebmcpEnabled:    &enabled,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.WebmcpEnabled)
	require.True(t, *result.WebmcpEnabled)

	// Read it back via get
	getResult, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{
		ToolsetSlug:      types.Slug(toolset.Slug),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, getResult)
	require.NotNil(t, getResult.Metadata)
	require.NotNil(t, getResult.Metadata.WebmcpEnabled)
	require.True(t, *getResult.Metadata.WebmcpEnabled)

	// Update to false
	disabled := false
	updateResult, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:      types.Slug(toolset.Slug),
		WebmcpEnabled:    &disabled,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updateResult)
	require.NotNil(t, updateResult.WebmcpEnabled)
	require.False(t, *updateResult.WebmcpEnabled)
}

func TestSetMcpMetadata_WebMCPEnabled_DefaultsFalse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := ti.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "WebMCP Default Test",
		Slug:                   "webmcp-default",
		Description:            conv.ToPGText("A toolset for testing webmcp_enabled default"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	// Set metadata without specifying webmcp_enabled (should default to false)
	result, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:      types.Slug(toolset.Slug),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.WebmcpEnabled)
	require.False(t, *result.WebmcpEnabled)
}

func TestServeInstallPage_WebMCPScript_Absent_WhenDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a public toolset
	toolset, err := ti.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "WebMCP Disabled Test",
		Slug:                   "webmcp-disabled",
		McpSlug:                conv.ToPGText("webmcp-disabled"),
		Description:            conv.ToPGText("A toolset with WebMCP disabled"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	_, err = ti.conn.Exec(ctx,
		"UPDATE toolsets SET mcp_is_public = true WHERE id = $1", toolset.ID)
	require.NoError(t, err)

	// Set metadata with webmcp_enabled = false (explicit)
	disabled := false
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:      types.Slug(toolset.Slug),
		WebmcpEnabled:    &disabled,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// Serve the install page
	req := httptest.NewRequest("GET", "/mcp/webmcp-disabled/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", "webmcp-disabled")
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = ti.service.ServeInstallPage(rr, req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.NotContains(t, body, "gram-webmcp-tools", "WebMCP tool JSON block should not be present when disabled")
	assert.NotContains(t, body, "navigator.modelContext", "WebMCP script should not be present when disabled")
}

func TestServeInstallPage_WebMCPScript_Present_WhenEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a public toolset
	toolset, err := ti.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "WebMCP Enabled Test",
		Slug:                   "webmcp-enabled",
		McpSlug:                conv.ToPGText("webmcp-enabled"),
		Description:            conv.ToPGText("A toolset with WebMCP enabled"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	_, err = ti.conn.Exec(ctx,
		"UPDATE toolsets SET mcp_is_public = true WHERE id = $1", toolset.ID)
	require.NoError(t, err)

	// Set metadata with webmcp_enabled = true
	enabled := true
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:      types.Slug(toolset.Slug),
		WebmcpEnabled:    &enabled,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// Serve the install page
	req := httptest.NewRequest("GET", "/mcp/webmcp-enabled/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", "webmcp-enabled")
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = ti.service.ServeInstallPage(rr, req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()

	// The WebMCP JSON block should be present
	assert.Contains(t, body, `id="gram-webmcp-tools"`, "WebMCP tool JSON block should be present when enabled")
	assert.Contains(t, body, `type="application/json"`, "WebMCP JSON block should have correct type")

	// The WebMCP registration script should be present
	assert.Contains(t, body, "navigator.modelContext", "WebMCP registration script should reference navigator.modelContext")
	assert.Contains(t, body, "registerTool", "WebMCP script should call registerTool")
	assert.Contains(t, body, "tools/call", "WebMCP script should use tools/call JSON-RPC method")
	assert.Contains(t, body, "[Gram WebMCP]", "WebMCP script should have Gram WebMCP log prefix")
}

func TestServeInstallPage_WebMCPScript_Absent_WhenNoMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a public toolset with NO metadata set
	toolset, err := ti.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "WebMCP No Metadata Test",
		Slug:                   "webmcp-no-metadata",
		McpSlug:                conv.ToPGText("webmcp-no-metadata"),
		Description:            conv.ToPGText("A toolset with no metadata record"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	_, err = ti.conn.Exec(ctx,
		"UPDATE toolsets SET mcp_is_public = true WHERE id = $1", toolset.ID)
	require.NoError(t, err)

	// Serve install page without setting any metadata
	req := httptest.NewRequest("GET", "/mcp/webmcp-no-metadata/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", "webmcp-no-metadata")
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = ti.service.ServeInstallPage(rr, req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.NotContains(t, body, "gram-webmcp-tools", "WebMCP should not be present when no metadata exists")
	assert.NotContains(t, body, "navigator.modelContext", "WebMCP script should not be present when no metadata exists")
}
