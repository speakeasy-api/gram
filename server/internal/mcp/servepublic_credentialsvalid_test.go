// servepublic_credentialsvalid_test.go consolidates tests that verify
// credential validity for public MCP endpoints: external OAuth tokens passed
// through and API-key access to private toolsets without OAuth.
package mcp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServePublicOAuth_ExternalNoSecurityDefs_ValidToken_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-nosec-tok",
		IsPublic: true,
	})

	mcpSlug := result.Toolset.McpSlug.String
	// External OAuth flow passes the bearer token through without Gram-level
	// validation — it's collected as-is in tokenInputs.
	w, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "some-external-token", nil)
	require.NoError(t, err)
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "should not send WWW-Authenticate when token provided")
}

func TestServePublic_PrivateWithoutOAuth_ValidAPIKey_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create private toolset WITHOUT OAuth proxy server
	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private No-OAuth MCP",
		Slug:                   "private-no-oauth-mcp-" + uuid.New().String()[:8],
		Description:            conv.ToPGText("A private MCP server without OAuth"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("private-no-oauth-mcp-" + uuid.New().String()[:8]),
		McpEnabled:             true,
		// OauthProxyServerID NOT set - no OAuth
	})
	require.NoError(t, err)

	// Create API key
	apiKey := ti.createTestAPIKey(ctx, t)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	err = ti.service.ServePublic(w, req)
	require.NoError(t, err)

	// WWW-Authenticate should NOT be present
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "WWW-Authenticate header should not be present when OAuth is not configured")
}
