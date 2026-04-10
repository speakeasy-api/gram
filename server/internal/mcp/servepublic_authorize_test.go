// Tests for authorization logic in ServePublic: whether a valid (or absent)
// credential has the right access to a public or private MCP resource.
package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServePublic_AllowsUnauthenticatedAccessToPublicMCP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a public toolset
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Public Unauthenticated MCP",
		Slug:                   "public-unauth-mcp",
		Description:            conv.ToPGText("A public MCP accessible without auth"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("public-unauth-mcp"),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make the toolset public
	toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	// Use a context WITHOUT any auth - simulates unauthenticated request
	unauthCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(unauthCtx)

	w := httptest.NewRecorder()

	// This should succeed - public MCPs should be accessible without authentication
	err = ti.service.ServePublic(w, req)
	require.NoError(t, err, "public MCP should be accessible without authentication")

	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "2.0", response["jsonrpc"])
}

func TestServePublic_AllowsCrossOrgAccessToPublicMCP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create toolset in the original org
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Public Cross-Org MCP",
		Slug:                   "public-cross-org-mcp",
		Description:            conv.ToPGText("A public MCP accessible from other orgs"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("public-cross-org-mcp"),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make the toolset public
	toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	// Create a different organization
	differentOrgID := uuid.New().String()

	// Create a context with a different ActiveOrganizationID to simulate cross-org access
	crossOrgAuthCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: differentOrgID,
		UserID:               authCtx.UserID,
		SessionID:            authCtx.SessionID,
	}

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	crossOrgCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
	crossOrgCtx = contextvalues.SetAuthContext(crossOrgCtx, crossOrgAuthCtx)
	req = req.WithContext(crossOrgCtx)

	w := httptest.NewRecorder()

	// This should succeed - public MCPs should be accessible from other orgs
	err = ti.service.ServePublic(w, req)
	require.NoError(t, err, "public MCP should be accessible from a different org")

	require.Equal(t, http.StatusOK, w.Code)
	require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))

	var response map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "response body: %s", w.Body.String())
	require.Equal(t, "2.0", response["jsonrpc"])
}

func TestServePublic_DeniesCrossOrgAccessToPrivateMCP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a PRIVATE toolset in the original org
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private Cross-Org MCP",
		Slug:                   "private-cross-org-mcp",
		Description:            conv.ToPGText("A private MCP not accessible from other orgs"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("private-cross-org-mcp"),
		McpEnabled:             true,
		// McpIsPublic defaults to false
	})
	require.NoError(t, err)

	// Create a different organization
	differentOrgID := uuid.New().String()

	// Create a context with a different ActiveOrganizationID to simulate cross-org access
	crossOrgAuthCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: differentOrgID,
		UserID:               authCtx.UserID,
		SessionID:            authCtx.SessionID,
	}

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	crossOrgCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
	crossOrgCtx = contextvalues.SetAuthContext(crossOrgCtx, crossOrgAuthCtx)
	req = req.WithContext(crossOrgCtx)

	w := httptest.NewRecorder()

	// This should fail - private MCPs require authentication and should NOT be accessible from other orgs
	err = ti.service.ServePublic(w, req)
	require.Error(t, err, "private MCP should NOT be accessible from a different org")
	// Private MCPs without a valid token return "expired or invalid access token"
	require.Contains(t, err.Error(), "expired or invalid access token")
}

func TestServePublic_SameOrgAuthenticatedUserGetsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a public toolset with a default environment
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Public Same-Org MCP",
		Slug:                   "public-same-org-mcp",
		Description:            conv.ToPGText("A public MCP for same-org test"),
		DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
		McpSlug:                conv.ToPGText("public-same-org-mcp"),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make the toolset public
	toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	// Use the original auth context - same org as the toolset
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	// This should succeed - same-org users should get authenticated access
	err = ti.service.ServePublic(w, req)
	require.NoError(t, err, "same-org user should have access to public MCP")

	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "2.0", response["jsonrpc"])
}
