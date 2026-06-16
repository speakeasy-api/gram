// servepublic_clientdance_test.go — OAuth protocol endpoint tests (well-known
// metadata) and full client-dance integration tests that exercise the
// 401 → WWW-Authenticate → resource metadata → server metadata discovery chain.
package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ---------------------------------------------------------------------------
// Well-known OAuth server metadata tests
// ---------------------------------------------------------------------------

func TestService_HandleGetAuthorizationServer(t *testing.T) {
	t.Parallel()

	t.Run("returns_error_when_mcp_slug_is_missing", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/", nil)

		// Empty mcp slug - use chi.RouteCtxKey to set context
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", "")
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err := ti.service.HandleGetAuthorizationServer(w, req)

		// Should return an error for missing slug
		require.Error(t, err)
		require.Contains(t, err.Error(), "mcp slug must be provided")
	})

	t.Run("returns_404_when_toolset_not_found", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/nonexistent", nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", "nonexistent-mcp-slug")
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err := ti.service.HandleGetAuthorizationServer(w, req)

		// Should return a 404 error
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("returns_404_when_no_oauth_configuration_found", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		// Create a toolset without OAuth configuration
		slug := "no-oauth-mcp-" + uuid.New().String()[:8]
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "No OAuth MCP",
			Slug:                   slug,
			Description:            conv.ToPGText("A test MCP without OAuth"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                conv.ToPGText(slug),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+toolset.McpSlug.String, nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err = ti.service.HandleGetAuthorizationServer(w, req)

		// Should return 404 for no OAuth configuration
		require.Error(t, err)
		require.Contains(t, err.Error(), "OAuth")
	})
}

// ---------------------------------------------------------------------------
// Well-known OAuth protected resource metadata tests
// ---------------------------------------------------------------------------

func TestService_HandleGetProtectedResource(t *testing.T) {
	t.Parallel()

	t.Run("returns_error_when_mcp_slug_is_missing", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/", nil)

		// Empty mcp slug
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", "")
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err := ti.service.HandleGetProtectedResource(w, req)

		// Should return an error for missing slug
		require.Error(t, err)
		require.Contains(t, err.Error(), "mcp slug must be provided")
	})

	t.Run("returns_404_when_toolset_not_found", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/nonexistent", nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", "nonexistent-mcp-slug")
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err := ti.service.HandleGetProtectedResource(w, req)

		// Should return a 404 error
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})
}

// ---------------------------------------------------------------------------
// Full client-dance integration tests
// ---------------------------------------------------------------------------

// TestClientDance_ExternalOAuth_FullFlow verifies the end-to-end OAuth
// discovery chain for external OAuth: unauthenticated initialize returns 401
// with a WWW-Authenticate header pointing to the protected resource metadata,
// which in turn references the external authorization server's metadata.
func TestClientDance_ExternalOAuth_FullFlow(t *testing.T) {
	t.Parallel()

	// 1. Stand up a real upstream OAuth server via dev-idp.
	idp := devidptest.Launch(t, devidptest.LaunchOpts{})

	// 2. Create an external OAuth toolset wired to dev-idp's oauth2-1
	//    authorization-server metadata.
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-dance",
		IsPublic: true,
		Metadata: idp.OAuth21Metadata(t),
	})

	mcpSlug := result.Toolset.McpSlug.String

	// 3. Send an initialize request WITHOUT a bearer token — expect 401.
	w, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	// 4. The 401 response must include a WWW-Authenticate header with resource_metadata URL.
	wwwAuth := w.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth, "401 must include WWW-Authenticate header")
	require.Contains(t, wwwAuth, "Bearer resource_metadata=")

	// 5. Call HandleGetProtectedResource and verify it returns
	//    valid JSON containing the MCP resource URL.
	prReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+mcpSlug, nil)
	prRctx := chi.NewRouteContext()
	prRctx.URLParams.Add("mcpSlug", mcpSlug)
	prReq = prReq.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, prRctx))

	prW := httptest.NewRecorder()
	err = ti.service.HandleGetProtectedResource(prW, prReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, prW.Code)

	var prMeta map[string]any
	err = json.Unmarshal(prW.Body.Bytes(), &prMeta)
	require.NoError(t, err)

	// The resource metadata must contain a "resource" field with the MCP URL.
	resource, ok := prMeta["resource"].(string)
	require.True(t, ok, "resource field must be a string")
	require.Contains(t, resource, mcpSlug, "resource URL should reference the MCP slug")

	// 6. The authorization_servers array must exist and reference the auth server's
	//    endpoints (stored metadata from the AuthorizationServer).
	authServers, ok := prMeta["authorization_servers"].([]any)
	require.True(t, ok, "authorization_servers should be an array")
	require.NotEmpty(t, authServers, "authorization_servers should not be empty")
}
