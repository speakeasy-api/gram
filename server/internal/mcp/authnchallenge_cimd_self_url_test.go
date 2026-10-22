package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

const clientIDNamesEndpointDescription = "client_id is this MCP server's URL, not an OAuth client ID. Check the OAuth Client ID setting in your MCP client."

// withPlatformOrigin stamps baseURL as the origin a request arrived on, so the
// endpoint's own URLs are built from it.
func withPlatformOrigin(ctx context.Context, baseURL string) context.Context {
	return requestorigin.WithContext(ctx, requestorigin.Origin{
		Surface:        requestorigin.SurfacePlatform,
		BaseURL:        baseURL,
		OrganizationID: "",
	})
}

func doCIMDAuthorizeAtOrigin(t *testing.T, ti *testInstance, baseURL, mcpSlug, clientID string) *httptest.ResponseRecorder {
	t.Helper()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {"http://127.0.0.1:33418/callback"},
		"code_challenge":        {pkceChallenge(pkceVerifier(t))},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(withPlatformOrigin(t.Context(), baseURL), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	return w
}

func requireClientIDNamesEndpoint(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "invalid_client", body["error"])
	require.Equal(t, clientIDNamesEndpointDescription, body["error_description"])
}

// TestOAuthCIMD_ClientIDNamingEndpointRejected serves the endpoint from the
// document server's own origin, so a client_id naming the endpoint is a URL
// the resolver could fetch. Each must be refused before any fetch with the
// actionable invalid_client rather than the 503 a failed fetch produces.
func TestOAuthCIMD_ClientIDNamingEndpointRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset := newTestCIMDService(t)
	mcpSlug := toolset.McpSlug.String
	origin := ds.srv.URL

	tests := []struct {
		name     string
		clientID string
	}{
		{name: "root", clientID: origin + "/mcp/" + mcpSlug},
		{name: "trailing slash", clientID: origin + "/mcp/" + mcpSlug + "/"},
		{name: "sub-path", clientID: origin + "/mcp/" + mcpSlug + "/install"},
		{name: "query", clientID: origin + "/mcp/" + mcpSlug + "?transport=http"},
		{name: "protected resource metadata", clientID: origin + "/.well-known/oauth-protected-resource/mcp/" + mcpSlug},
		{name: "authorization server metadata", clientID: origin + "/.well-known/oauth-authorization-server/mcp/" + mcpSlug},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requireClientIDNamesEndpoint(t, doCIMDAuthorizeAtOrigin(t, ti, origin, mcpSlug, tc.clientID))
		})
	}
}

// TestOAuthCIMD_ClientIDNamingEndpointHostCaseInsensitive: hosts compare
// case-insensitively, so a client_id spelling the endpoint's host in another
// case still names the endpoint.
func TestOAuthCIMD_ClientIDNamingEndpointHostCaseInsensitive(t *testing.T) {
	t.Parallel()

	_, ti, _, toolset := newTestCIMDService(t)
	mcpSlug := toolset.McpSlug.String

	w := doCIMDAuthorizeAtOrigin(t, ti, "https://MCP.Example.test", mcpSlug, "https://mcp.example.TEST/mcp/"+mcpSlug)
	requireClientIDNamesEndpoint(t, w)
}

// TestOAuthCIMD_ClientIDNamingEndpointRejectedWhenAdmissionDisabled pins the
// ordering: the check runs ahead of admission, so an issuer that refuses all
// documents still tells the person what is actually wrong.
func TestOAuthCIMD_ClientIDNamingEndpointRejectedWhenAdmissionDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeDisabled)
	mcpSlug := toolset.McpSlug.String

	w := doCIMDAuthorizeAtOrigin(t, ti, ds.srv.URL, mcpSlug, ds.srv.URL+"/mcp/"+mcpSlug)
	requireClientIDNamesEndpoint(t, w)
}

// TestOAuthCIMD_DocumentOnEndpointHostResolves: a document on the endpoint's
// own host, outside the endpoint's paths, is still a client metadata document.
func TestOAuthCIMD_DocumentOnEndpointHostResolves(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset := newTestCIMDService(t)

	w := doCIMDAuthorizeAtOrigin(t, ti, ds.srv.URL, toolset.McpSlug.String, ds.clientID)
	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t, int64(1), ds.requests.Load())
}

// TestOAuthCIMD_EndpointPathOnOtherHostFetched: the check is scoped to the
// origin the request arrived on, so the endpoint's path on any other host is
// treated as a document URL and fetched as before.
func TestOAuthCIMD_EndpointPathOnOtherHostFetched(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset := newTestCIMDService(t)
	mcpSlug := toolset.McpSlug.String

	w := doCIMDAuthorizeAtOrigin(t, ti, "https://mcp.example.test", mcpSlug, ds.srv.URL+"/mcp/"+mcpSlug)
	requireAuthorizeOAuthError(t, w, http.StatusServiceUnavailable, "temporarily_unavailable")
}

// TestOAuthCIMD_TokenJWTBearerClientIDNamingEndpointRejected: the JWT bearer
// grant resolves CIMD at /token, so it applies the same check before reading
// the assertion.
func TestOAuthCIMD_TokenJWTBearerClientIDNamingEndpointRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset := newTestCIMDService(t)
	mcpSlug := toolset.McpSlug.String

	ctx := withPlatformOrigin(t.Context(), ds.srv.URL)
	w := postIDJAGToken(t, ctx, ti, mcpSlug, ds.srv.URL+"/mcp/"+mcpSlug+"/", "unused-assertion")
	requireClientIDNamesEndpoint(t, w)
}
