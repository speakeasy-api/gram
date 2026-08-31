// wellknown_test.go covers the /.well-known/.../mcp/{slug} OAuth metadata
// handlers (HandleGetAuthorizationServer, HandleGetProtectedResource)
// end-to-end. Resolution tries mcp_endpoints → mcp_servers first and falls
// back to the legacy toolsets.mcp_slug lookup, so these tests exercise the
// full per-backend dispatch matrix on the resolved path (remote / toolset /
// issuer-gated) plus the legacy slug fallback — mirroring the /x/mcp
// coverage in xmcp/wellknown_test.go, but with metadata URLs rooted at
// /mcp/{slug}.
//
// The low-level response writers these handlers share
// (writeOAuth{Server,ProtectedResource}MetadataResponse) are unit-tested
// separately in wellknown_oauth_test.go, which is white-box (package mcp)
// because those helpers are unexported.
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

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// runMCPWellKnown invokes a /mcp well-known handler with the chi `mcpSlug`
// URL param set to slug. Passing slug="" exercises the missing-slug branch
// (chi.URLParam returns "" for both missing and empty params, matching
// production routing).
func runMCPWellKnown(
	t *testing.T,
	ctx context.Context,
	handler func(http.ResponseWriter, *http.Request) error,
	slug string,
) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/.well-known/oauth/mcp/"+slug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := handler(w, req)
	return w, err
}

// ---------------------------------------------------------------------------
// HandleGetAuthorizationServer
// ---------------------------------------------------------------------------

func TestHandleGetAuthorizationServer_MissingSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp slug must be provided")
	require.Empty(t, w.Body.String())
}

func TestWellKnownPrivateOnlyEndpointDoesNotFallBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolsetsRepo := toolsetsrepo.New(ti.conn)
	sharedSlug := "wellknown-private-only-" + uuid.NewString()[:8]
	endpointToolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "endpoint-"+uuid.NewString()[:8])
	legacy := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug: sharedSlug, IsPublic: true,
	})
	_, err := toolsetsRepo.UpdateToolset(ctx, toolsetsrepo.UpdateToolsetParams{
		Name: legacy.Toolset.Name, Description: legacy.Toolset.Description,
		DefaultEnvironmentSlug: legacy.Toolset.DefaultEnvironmentSlug,
		McpSlug:                pgtype.Text{String: sharedSlug, Valid: true},
		McpIsPublic:            true, McpEnabled: true,
		CustomDomainID: uuid.NullUUID{}, ToolSelectionMode: "",
		Slug: legacy.Toolset.Slug, ProjectID: legacy.Toolset.ProjectID,
	})
	require.NoError(t, err)
	server := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, endpointToolset.ID, sharedSlug, "public", uuid.NullUUID{}, uuid.Nil)
	rows, err := testrepo.New(ti.conn).SetMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMCPServerNetworkAccessModeFixtureParams{
		NetworkAccessMode: pgtype.Text{String: string(networkaccess.ModePrivateOnly), Valid: true},
		ID:                server.ID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	for _, handler := range []func(http.ResponseWriter, *http.Request) error{
		ti.service.HandleGetAuthorizationServer,
		ti.service.HandleGetProtectedResource,
	} {
		w, err := runMCPWellKnown(t, ctx, handler, sharedSlug)
		require.Error(t, err)
		require.Empty(t, w.Body.String())
	}
}

func TestHandleGetAuthorizationServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, "definitely-missing-"+uuid.NewString()[:8])
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp server not found")
	require.Empty(t, w.Body.String())
}

func TestHandleGetAuthorizationServer_DisabledServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "disabled-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "disabled", issuerID)

	// Disabled mcp_server resolves as not-found; with no legacy toolset for
	// the slug the fallback also misses → 404.
	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.Error(t, err)
	require.Empty(t, w.Body.String())
}

func TestHandleGetAuthorizationServer_ToolsetBackendWithoutOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "ts-noauth-"+uuid.NewString()[:8])
	slug := "ep-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth configuration found")
	require.Empty(t, w.Body.String())
}

func TestHandleGetAuthorizationServer_ToolsetBackendWithExternalOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	external := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "mcp-srv-external",
		IsPublic: true,
		Metadata: nil,
	})
	slug := external.Toolset.McpSlug.String
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, external.Toolset.ID, slug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")

	// External OAuth toolsets re-serve the upstream provider's captured
	// metadata, but the issuer is rewritten to the Gram resource URL so the
	// document satisfies RFC 8414 §3.3 (served issuer must equal the URL the
	// client fetched it under, i.e. the /mcp/{slug} surface). The upstream's
	// own authorization/token endpoints are preserved verbatim.
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "http://0.0.0.0/mcp/"+slug, metadata["issuer"])
	require.Equal(t, "https://test-oauth-server.example.com/authorize", metadata["authorization_endpoint"])
	require.Equal(t, "https://test-oauth-server.example.com/token", metadata["token_endpoint"])
}

// TestHandleGetAuthorizationServer_IssuerGatedRemoteBackend is the primary
// regression for AGE-2624: an issuer-gated remote-backed mcp_server
// addressed at /mcp/{slug} now serves Gram-hosted RFC 8414 metadata
// (previously 404). The advertised issuer + endpoint URLs are rooted at
// /mcp/{slug}, matching the resource_metadata URL ServePublic advertises.
func TestHandleGetAuthorizationServer_IssuerGatedRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "issuer-remote-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "public, max-age=60", w.Header().Get("Cache-Control"))
	require.NotEmpty(t, w.Header().Get("ETag"))

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedIssuer := "http://0.0.0.0/mcp/" + slug
	require.Equal(t, expectedIssuer, metadata["issuer"])
	require.Equal(t, expectedIssuer+"/authorize", metadata["authorization_endpoint"])
	require.Equal(t, expectedIssuer+"/token", metadata["token_endpoint"])
	require.Equal(t, expectedIssuer+"/register", metadata["registration_endpoint"])
	require.Equal(t, expectedIssuer+"/revoke", metadata["revocation_endpoint"])
}

// TestHandleGetAuthorizationServer_IssuerGatedToolsetBackend is the
// toolset companion of the remote-backed test. The dispatch branches on
// backend after the issuer check, so both backends need coverage.
func TestHandleGetAuthorizationServer_IssuerGatedToolsetBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "ts-issuer-"+uuid.NewString()[:8])
	slug := "issuer-toolset-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuerID)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedIssuer := "http://0.0.0.0/mcp/" + slug
	require.Equal(t, expectedIssuer, metadata["issuer"])
	require.Equal(t, expectedIssuer+"/authorize", metadata["authorization_endpoint"])

	// RFC 9207 §3. Asserted per surface because the route base is baked into
	// the issuer that the `iss` authorization-response parameter has to match,
	// so a regression can land on one surface while the other stays correct.
	require.Equal(t, true, metadata["authorization_response_iss_parameter_supported"])
}

// TestHandleGetAuthorizationServer_IssuerGatedRemoteBackend_DanglingIssuerFK
// covers the race where the user_session_issuer FK target is deleted
// between mcp_server resolution and metadata emission.
func TestHandleGetAuthorizationServer_IssuerGatedRemoteBackend_DanglingIssuerFK(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "dangling-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)

	// Sanity check: resolves cleanly before deletion.
	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	err = testrepo.New(ti.conn).ForceSoftDeleteUserSessionIssuer(ctx, testrepo.ForceSoftDeleteUserSessionIssuerParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	w, err = runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.Error(t, err, "dangling issuer FK must surface as a request-level error")
	require.Contains(t, err.Error(), "user_session_issuer not found")
	require.Empty(t, w.Body.String())
}

// TestHandleGetAuthorizationServer_LegacySlugFallbackProxy confirms a
// toolset with no mcp_endpoint row (pre the toolsets→mcp_servers
// migration) still resolves via the legacy toolsets.mcp_slug fallback.
// ---------------------------------------------------------------------------
// HandleGetProtectedResource
// ---------------------------------------------------------------------------

func TestHandleGetProtectedResource_MissingSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp slug must be provided")
	require.Empty(t, w.Body.String())
}

func TestHandleGetProtectedResource_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, "definitely-missing-"+uuid.NewString()[:8])
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp server not found")
	require.Empty(t, w.Body.String())
}

func TestHandleGetProtectedResource_ToolsetBackendWithoutOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "ts-prnoauth-"+uuid.NewString()[:8])
	slug := "ep-pr-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth configuration found")
	require.Empty(t, w.Body.String())
}

// TestHandleGetProtectedResource_IssuerGatedRemoteBackend is the
// protected-resource companion of the AGE-2624 regression: an issuer-gated
// remote-backed mcp_server at /mcp/{slug} serves RFC 9728 metadata whose
// resource + authorization_servers point back at /mcp/{slug}.
func TestHandleGetProtectedResource_IssuerGatedRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "issuer-remote-pr-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedResource := "http://0.0.0.0/mcp/" + slug
	require.Equal(t, expectedResource, metadata["resource"])
	authServers, ok := metadata["authorization_servers"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{expectedResource}, authServers)
}

func TestHandleGetProtectedResource_IssuerGatedToolsetBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "ts-issuer-pr-"+uuid.NewString()[:8])
	slug := "issuer-toolset-pr-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuerID)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "http://0.0.0.0/mcp/"+slug, metadata["resource"])
}

// TestHandleGetProtectedResource_IssuerGatedToolsetBackend_OnCustomDomain
// asserts an issuer-gated endpoint registered against a custom domain
// emits https://<domain>/mcp/<slug> as both resource and
// authorization_servers — clients reject discovery responses whose host
// doesn't match the resource they were directed to.
func TestHandleGetProtectedResource_IssuerGatedToolsetBackend_OnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	domainName := "mcp-issuer-cd-" + uuid.NewString()[:8] + ".example.com"
	toolset, domain := createPublicMCPToolsetWithCustomDomain(t, ctx, ti, authCtx, "ts-cd-"+uuid.NewString()[:8], domainName)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "issuer-cd-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{UUID: domain.ID, Valid: true}, issuerID)

	domainCtx := customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})
	domainCtx = requestorigin.WithContext(domainCtx, requestorigin.Origin{
		Surface: requestorigin.SurfaceCustomDomain, BaseURL: "https://" + domain.Domain,
		OrganizationID: authCtx.ActiveOrganizationID,
	})

	w, err := runMCPWellKnown(t, domainCtx, ti.service.HandleGetProtectedResource, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedResource := "https://" + domain.Domain + "/mcp/" + slug
	require.Equal(t, expectedResource, metadata["resource"])
	authServers, ok := metadata["authorization_servers"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{expectedResource}, authServers)
}

// TestHandleGetProtectedResource_LegacySlugFallbackProxy is the
// protected-resource companion of the legacy fallback test.
