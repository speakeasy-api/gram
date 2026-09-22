package xmcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

const xmcpAuthenticationHost = "https://auth.example.com"

// xmcpAuthenticationHostHandler mounts the /mcp and /x/mcp authorization
// server routes on the authentication host, as the server does, in front of a
// next handler that fails the test if anything falls through to it.
func xmcpAuthenticationHostHandler(t *testing.T, ti *testInstance) http.Handler {
	t.Helper()

	host, err := mcp.NewAuthenticationHost(xmcpAuthenticationHost, ti.serverURL, "test")
	require.NoError(t, err)
	mcp.AttachAuthenticationHost(host, ti.mcpService)
	xmcp.AttachAuthenticationHost(host, ti.service)

	return host.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Errorf("a request for the authentication host reached the main mux")
	}))
}

func serveOnAuthenticationHost(t *testing.T, handler http.Handler, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(t.Context(), method, target, body)
	req.Host = "auth.example.com"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func optInToAuthenticationHost(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, issuerID uuid.UUID) {
	t.Helper()

	updated, err := testrepo.New(ti.conn).SetUserSessionIssuerUseAuthenticationHostFixture(ctx, testrepo.SetUserSessionIssuerUseAuthenticationHostFixtureParams{
		UseAuthenticationHost: true,
		IssuerID:              issuerID,
		OrganizationID:        organizationID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated, "the issuer must exist in this organization")
}

// An /x/mcp endpoint whose issuer opts in is served on the authentication
// host: its metadata names that host, and authorization there keeps consent
// on it. The MCP endpoint itself and its protected resource metadata are not.
func TestAuthenticationHost_XMCPOptedInEndpointServed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, issuerID := seedIssuerGatedToolsetMCPEndpoint(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, mcpservers.VisibilityPublic)
	optInToAuthenticationHost(t, ctx, ti, authCtx.ActiveOrganizationID, issuerID)
	client, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                "xmcp-auth-host-" + uuid.NewString()[:8],
		ClientName:              "test client",
		RedirectUris:            []string{"http://localhost:3000/callback"},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	handler := xmcpAuthenticationHostHandler(t, ti)
	authIssuer := xmcpAuthenticationHost + "/x/mcp/" + slug

	w := serveOnAuthenticationHost(t, handler, http.MethodGet, "/.well-known/oauth-authorization-server/x/mcp/"+slug, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	require.Equal(t, authIssuer, meta["issuer"])
	require.Equal(t, authIssuer+"/authorize", meta["authorization_endpoint"])
	require.Equal(t, authIssuer+"/token", meta["token_endpoint"])
	require.Equal(t, authIssuer+"/register", meta["registration_endpoint"])
	require.Equal(t, authIssuer+"/revoke", meta["revocation_endpoint"])

	authorize := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {client.RedirectUris[0]},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}
	w = serveOnAuthenticationHost(t, handler, http.MethodGet, "/x/mcp/"+slug+"/authorize?"+authorize.Encode(), nil)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), authIssuer+"/connect?"), w.Header().Get("Location"))

	consent, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	w = serveOnAuthenticationHost(t, handler, http.MethodGet, consent.RequestURI(), nil)
	require.Equal(t, http.StatusOK, w.Code, "the consent page the redirect names must be served on the authentication host")

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/x/mcp/" + slug},
		{http.MethodGet, "/x/mcp/" + slug},
		{http.MethodGet, "/.well-known/oauth-protected-resource/x/mcp/" + slug},
		{http.MethodGet, "/x/mcp/idp_callback"},
	} {
		w := serveOnAuthenticationHost(t, handler, route.method, route.path, nil)
		require.Equal(t, http.StatusNotFound, w.Code, "%s %s", route.method, route.path)
	}
}

// An /x/mcp endpoint whose issuer has not opted in is not served on the
// authentication host at all.
func TestAuthenticationHost_XMCPIssuerNotOptedInIsNotServed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedIssuerGatedToolsetMCPEndpoint(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, mcpservers.VisibilityPublic)
	handler := xmcpAuthenticationHostHandler(t, ti)

	w := serveOnAuthenticationHost(t, handler, http.MethodGet, "/.well-known/oauth-authorization-server/x/mcp/"+slug, nil)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	w = serveOnAuthenticationHost(t, handler, http.MethodPost, "/x/mcp/"+slug+"/token", url.Values{"grant_type": {"authorization_code"}})
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	w = serveOnAuthenticationHost(t, handler, http.MethodPost, "/x/mcp/"+slug+"/register", url.Values{})
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
