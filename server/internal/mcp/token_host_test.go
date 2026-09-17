package mcp_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp"
)

const testTokenHostURL = "https://auth.example.com"

// tokenHostHarness is the token host middleware in front of a next handler
// that records whether a request fell through to the main mux.
type tokenHostHarness struct {
	handler       http.Handler
	passedThrough *bool
}

func newTokenHostHarness(t *testing.T, ti *testInstance) tokenHostHarness {
	t.Helper()

	tokenHost, err := mcp.NewTokenHost(testTokenHostURL, ti.serverURL, "test")
	require.NoError(t, err)
	mcp.AttachTokenHost(tokenHost, ti.service)

	passed := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		passed = true
		w.WriteHeader(http.StatusTeapot)
	})
	return tokenHostHarness{handler: tokenHost.Middleware(next), passedThrough: &passed}
}

func (h tokenHostHarness) serve(t *testing.T, method, host, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, body)
	req.Host = host
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	return w
}

func tokenHostTokenURL(mcpSlug string) string {
	return testTokenHostURL + "/mcp/" + mcpSlug + "/token"
}

// fetchAdvertisedTokenEndpoint reads token_endpoint from the RFC 8414 document
// served on the MCP host.
func fetchAdvertisedTokenEndpoint(t *testing.T, ti *testInstance, mcpSlug string) string {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+mcpSlug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleGetAuthorizationServer(w, req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	tokenEndpoint, ok := meta["token_endpoint"].(string)
	require.True(t, ok, "metadata must carry a string token_endpoint: %s", w.Body.String())
	return tokenEndpoint
}

func accessTokenClaims(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	require.NoError(t, json.Unmarshal(body, &resp))
	accessToken, ok := resp["access_token"].(string)
	require.True(t, ok, "token response must carry an access_token: %s", string(body))
	parts := strings.Split(accessToken, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims
}

// A client assertion naming the token URL on the token host authenticates a
// token request sent there, and the session it mints carries the issuer and
// resource of the MCP host.
func TestTokenHost_TokenHostAudienceAccepted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)
	harness := newTokenHostHarness(t, ti)
	slug := toolset.McpSlug.String

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	form := withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, tokenHostTokenURL(slug)))
	form.Set("resource", advertisedIssuer)
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.False(t, *harness.passedThrough)

	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, advertisedIssuer, claims["iss"])

	// The issuer stays the other half of the accepted pair.
	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, client)
	w = harness.serve(t, http.MethodPost, "auth.example.com:443", "/mcp/"+slug+"/token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, advertisedIssuer)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Metadata keeps advertising the MCP host's token endpoint.
	require.Equal(t, advertisedIssuer+"/token", fetchAdvertisedTokenEndpoint(t, ti, slug))
}

// On the token host the accepted audiences are exactly the issuer and the
// token host's own token URL; nothing else on either host verifies.
func TestTokenHost_OtherAudiencesRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)
	harness := newTokenHostHarness(t, ti)
	slug := toolset.McpSlug.String

	for _, aud := range []string{
		advertisedIssuer + "/token",
		testTokenHostURL,
		testTokenHostURL + "/mcp/" + slug,
		testTokenHostURL + "/mcp/" + slug + "/revoke",
		testTokenHostURL + "/mcp/" + slug + "/token/",
		tokenHostTokenURL(slug) + "?x=1",
	} {
		code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
		w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, aud)))
		require.Equal(t, http.StatusUnauthorized, w.Code, "aud %q: %s", aud, w.Body.String())
		require.Contains(t, w.Body.String(), "invalid_client")
	}

	// The token host's URL is not an audience on the MCP host.
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	w := postForm(t, ti, slug, "token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, tokenHostTokenURL(slug))))
	requireInvalidClient(t, w)
}

// The canonical resource is the MCP host's, so a resource naming the token
// host matches nothing.
func TestTokenHost_ResourceStaysOnMCPHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	harness := newTokenHostHarness(t, ti)
	slug := toolset.McpSlug.String

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	form := withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, tokenHostTokenURL(slug)))
	form.Set("resource", testTokenHostURL+"/mcp/"+slug)
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_target")
}

// The token host admits no JWT bearer grant, and says so before asking for
// client credentials.
func TestTokenHost_JWTBearerGrantRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	harness := newTokenHostHarness(t, ti)
	slug := toolset.McpSlug.String

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", "header.payload.signature")
	form.Set("resource", "http://0.0.0.0/mcp/"+slug)
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "unsupported_grant_type")
}

// Nothing but the token endpoint answers on the token host.
func TestTokenHost_NonTokenRoutesNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	harness := newTokenHostHarness(t, ti)
	slug := toolset.McpSlug.String

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/mcp/" + slug},
		{http.MethodGet, "/mcp/" + slug},
		{http.MethodDelete, "/mcp/" + slug},
		{http.MethodGet, "/mcp/" + slug + "/token"},
		{http.MethodPost, "/mcp/" + slug + "/revoke"},
		{http.MethodPost, "/mcp/" + slug + "/register"},
		{http.MethodGet, "/mcp/" + slug + "/authorize"},
		{http.MethodGet, "/mcp/" + slug + "/connect"},
		{http.MethodGet, "/.well-known/oauth-authorization-server/mcp/" + slug},
		{http.MethodGet, "/.well-known/oauth-protected-resource/mcp/" + slug},
		{http.MethodPost, "/x/mcp/" + slug + "/token"},
		{http.MethodPost, "/rpc/auth.info"},
		{http.MethodGet, "/"},
	} {
		w := harness.serve(t, route.method, "auth.example.com", route.path, nil)
		require.Equal(t, http.StatusNotFound, w.Code, "%s %s", route.method, route.path)
	}
	require.False(t, *harness.passedThrough)
}

// Requests for any other host reach the main mux untouched.
func TestTokenHost_OtherHostsPassThrough(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	harness := newTokenHostHarness(t, ti)

	w := harness.serve(t, http.MethodPost, "0.0.0.0", "/mcp/some-slug/token", url.Values{})
	require.Equal(t, http.StatusTeapot, w.Code)
	require.True(t, *harness.passedThrough)
}

func TestNewTokenHost_Disabled(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	tokenHost, err := mcp.NewTokenHost("", serverURL, "prod")
	require.NoError(t, err)

	passed := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed = true })
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.Host = "auth.example.com"
	tokenHost.Middleware(next).ServeHTTP(httptest.NewRecorder(), req)
	require.True(t, passed)
}

func TestNewTokenHost_InvalidURLsRefused(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	for _, raw := range []string{
		"http://auth.example.com",
		"https://app.example.com",
		"https://APP.example.com:443",
		"https://auth.example.com/prefix",
		"https://auth.example.com?x=1",
		"https://user@auth.example.com",
		"https://auth.example.com#frag",
		"auth.example.com",
		"https://",
	} {
		_, err := mcp.NewTokenHost(raw, serverURL, "prod")
		require.Error(t, err, raw)
	}

	_, err = mcp.NewTokenHost("https://auth.example.com/", serverURL, "prod")
	require.NoError(t, err)

	localServerURL, err := url.Parse("http://localhost:8080")
	require.NoError(t, err)
	_, err = mcp.NewTokenHost("http://127.0.0.1:8080", localServerURL, "local")
	require.NoError(t, err)
}
