// wellknown_test.go covers the experimental /.well-known/.../x/mcp/{slug}
// routes. The xmcp handlers walk slug → mcp_endpoint → mcp_server and
// dispatch per-backend (toolset vs remote), so these tests exercise:
//
//   - Path validation (missing slug, unknown slug, disabled server).
//   - Remote-backed dispatch returning 404 — gated on a separate upcoming
//     OAuth migration (independent of AGE-1902).
//   - Toolset-backed dispatch reusing the existing wellknown resolvers, with
//     proxy and external-OAuth happy paths covered.
//
// The production model assumes mcp_endpoints.slug == toolsets.mcp_slug for
// toolset-backed servers until the upcoming OAuth migration moves the OAuth
// machinery onto mcp_servers. seedToolsetMCPEndpoint mirrors that assumption.
package xmcp_test

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

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// runWellKnown invokes the supplied xmcp well-known handler with the chi
// `mcpSlug` URL param set to slug. The empty slug case is supported by
// passing slug="" — chi.URLParam returns "" both when the param is missing
// and when it is explicitly empty, which matches production routing.
func runWellKnown(
	t *testing.T,
	ctx context.Context,
	handler func(http.ResponseWriter, *http.Request) error,
	path, slug string,
) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := handler(w, req)
	return w, err
}

// seedBareToolset creates a toolset with the given mcp_slug and no OAuth
// configuration. Used for the "no OAuth configured" branches.
func seedBareToolset(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, organizationID, mcpSlug string) toolsetsrepo.Toolset {
	t.Helper()

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Name:                   "xmcp-bare-" + mcpSlug,
		Slug:                   mcpSlug,
		Description:            conv.ToPGText("xmcp wellknown_test bare toolset"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(mcpSlug),
		McpEnabled:             true,
	})
	require.NoError(t, err)
	return toolset
}

// ---------------------------------------------------------------------------
// HandleWellKnownOAuthServerMetadata
// ---------------------------------------------------------------------------

func TestHandleWellKnownOAuthServerMetadata_MissingSlug(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)

	w, err := runWellKnown(t, t.Context(), ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp slug must be provided")
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthServerMetadata_EndpointNotFound(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)

	w, err := runWellKnown(t, t.Context(), ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/none", "definitely-missing-"+uuid.NewString()[:8])
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp endpoint not found")
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthServerMetadata_DisabledServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "disabled")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.Error(t, err)
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthServerMetadata_RemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Even a remote-backed mcp_server with an oauth_proxy attached returns
	// 404 today. Sourcing OAuth metadata from mcp_servers / oauth_proxy_servers
	// directly is gated on a separate upcoming OAuth migration (independent
	// of AGE-1902); until that lands, the upstream remote MCP server
	// publishes its own .well-known and Gram doesn't act as the AS.
	slug := seedRemoteMCPEndpointWithOAuthProxy(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth configuration found")
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthServerMetadata_ToolsetBackendWithoutOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := seedBareToolset(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "ts-noauth-"+uuid.NewString()[:8])
	slug, _ := seedToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, toolset, "public")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth configuration found")
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthServerMetadata_ToolsetBackendWithProxy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	proxy := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "xmcp-srv-proxy",
		IsPublic:     true,
		ProviderType: "",
	})
	slug, _ := seedToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, proxy.Toolset, "public")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedIssuer := "http://0.0.0.0/oauth/" + slug
	require.Equal(t, expectedIssuer, metadata["issuer"])
	require.Equal(t, expectedIssuer+"/authorize", metadata["authorization_endpoint"])
	require.Equal(t, expectedIssuer+"/token", metadata["token_endpoint"])
	require.Equal(t, expectedIssuer+"/register", metadata["registration_endpoint"])

	// The Gram-issued offline_access scope is always advertised so RFC 8414
	// scopes_supported never disappears.
	scopes, ok := metadata["scopes_supported"].([]any)
	require.True(t, ok)
	require.Contains(t, scopes, "offline_access")

	grants, ok := metadata["grant_types_supported"].([]any)
	require.True(t, ok)
	require.Contains(t, grants, "authorization_code")
	require.Contains(t, grants, "refresh_token")
}

func TestHandleWellKnownOAuthServerMetadata_ToolsetBackendOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	domain := seedCustomDomain(t, ctx, ti, authCtx.ActiveOrganizationID, "xmcp-srv-cd-"+uuid.NewString()[:8]+".example.com")
	proxy := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "xmcp-srv-cd",
		IsPublic:     true,
		ProviderType: "",
	})
	slug, _ := seedToolsetMCPEndpointOnDomain(t, ctx, ti, *authCtx.ProjectID, proxy.Toolset, "public", uuid.NullUUID{UUID: domain.ID, Valid: true})

	domainCtx := customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})

	w, err := runWellKnown(t, domainCtx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	// Custom-domain requests must produce metadata URLs rooted at the
	// custom domain (https), not the platform serverURL — clients will
	// reject discovery responses whose host doesn't match the resource
	// they were directed to.
	expectedIssuer := "https://" + domain.Domain + "/oauth/" + slug
	require.Equal(t, expectedIssuer, metadata["issuer"])
	require.Equal(t, expectedIssuer+"/authorize", metadata["authorization_endpoint"])
	require.Equal(t, expectedIssuer+"/token", metadata["token_endpoint"])
	require.Equal(t, expectedIssuer+"/register", metadata["registration_endpoint"])
}

func TestHandleWellKnownOAuthServerMetadata_ToolsetBackendWithExternalOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	external := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:       "xmcp-srv-external",
		IsPublic:   true,
		Metadata:   nil,
		AuthServer: nil,
	})
	slug, _ := seedToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, external.Toolset, "public")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")

	// External OAuth toolsets surface the upstream provider's metadata
	// verbatim — confirm we passed the stored JSON through unmodified.
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "https://test-oauth-server.example.com", metadata["issuer"])
}

// ---------------------------------------------------------------------------
// HandleWellKnownOAuthProtectedResourceMetadata
// ---------------------------------------------------------------------------

func TestHandleWellKnownOAuthProtectedResourceMetadata_MissingSlug(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)

	w, err := runWellKnown(t, t.Context(), ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp slug must be provided")
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthProtectedResourceMetadata_EndpointNotFound(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)

	w, err := runWellKnown(t, t.Context(), ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/none", "definitely-missing-"+uuid.NewString()[:8])
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp endpoint not found")
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthProtectedResourceMetadata_DisabledServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "disabled")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.Error(t, err)
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthProtectedResourceMetadata_RemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := seedRemoteMCPEndpointWithExternalOAuth(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthProtectedResourceMetadata_ToolsetBackendWithoutOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := seedBareToolset(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "ts-prnoauth-"+uuid.NewString()[:8])
	slug, _ := seedToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, toolset, "public")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
	require.Empty(t, w.Body.String())
}

func TestHandleWellKnownOAuthProtectedResourceMetadata_ToolsetBackendWithProxy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	proxy := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "xmcp-pr-proxy",
		IsPublic:     true,
		ProviderType: "",
	})
	slug, _ := seedToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, proxy.Toolset, "public")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedResource := "http://0.0.0.0/x/mcp/" + slug
	require.Equal(t, expectedResource, metadata["resource"])

	// authorization_servers points at the same resource so .well-known
	// discovery loops back to /.well-known/oauth-authorization-server/x/mcp/{slug}.
	authServers, ok := metadata["authorization_servers"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{expectedResource}, authServers)
}

func TestHandleWellKnownOAuthProtectedResourceMetadata_ToolsetBackendOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	domain := seedCustomDomain(t, ctx, ti, authCtx.ActiveOrganizationID, "xmcp-pr-cd-"+uuid.NewString()[:8]+".example.com")
	proxy := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "xmcp-pr-cd",
		IsPublic:     true,
		ProviderType: "",
	})
	slug, _ := seedToolsetMCPEndpointOnDomain(t, ctx, ti, *authCtx.ProjectID, proxy.Toolset, "public", uuid.NullUUID{UUID: domain.ID, Valid: true})

	domainCtx := customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})

	w, err := runWellKnown(t, domainCtx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedResource := "https://" + domain.Domain + "/x/mcp/" + slug
	require.Equal(t, expectedResource, metadata["resource"])

	authServers, ok := metadata["authorization_servers"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{expectedResource}, authServers)
}

func TestHandleWellKnownOAuthProtectedResourceMetadata_ToolsetBackendWithExternalOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	external := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:       "xmcp-pr-external",
		IsPublic:   true,
		Metadata:   nil,
		AuthServer: nil,
	})
	slug, _ := seedToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, external.Toolset, "public")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "http://0.0.0.0/x/mcp/"+slug, metadata["resource"])
}
