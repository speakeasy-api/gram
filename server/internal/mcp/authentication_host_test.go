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
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

const testAuthenticationHostURL = "https://auth.example.com"

// authenticationHostHarness is the authentication host middleware in front of
// a next handler that records whether a request fell through to the main mux.
type authenticationHostHarness struct {
	handler       http.Handler
	passedThrough *bool
}

func newAuthenticationHostHarness(t *testing.T, ti *testInstance) authenticationHostHarness {
	t.Helper()

	authenticationHost, err := mcp.NewAuthenticationHost(testAuthenticationHostURL, ti.serverURL, "test")
	require.NoError(t, err)
	mcp.AttachAuthenticationHost(authenticationHost, ti.service)

	passed := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		passed = true
		w.WriteHeader(http.StatusTeapot)
	})
	return authenticationHostHarness{handler: authenticationHost.Middleware(next), passedThrough: &passed}
}

func (h authenticationHostHarness) serve(t *testing.T, method, host, path string, form url.Values) *httptest.ResponseRecorder {
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

func authenticationHostTokenURL(mcpSlug string) string {
	return testAuthenticationHostURL + "/mcp/" + mcpSlug + "/token"
}

// useAuthenticationHost opts an issuer in to announcing the authentication
// host.
func useAuthenticationHost(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, issuerID uuid.UUID) {
	t.Helper()

	updated, err := testrepo.New(ti.conn).SetUserSessionIssuerUseAuthenticationHostFixture(ctx, testrepo.SetUserSessionIssuerUseAuthenticationHostFixtureParams{
		UseAuthenticationHost: true,
		IssuerID:              issuerID,
		OrganizationID:        organizationID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated, "the issuer must exist in this organization")
}

func wellKnownRequest(t *testing.T, prefix, mcpSlug string) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, prefix+mcpSlug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// fetchProtectedResourceMetadata reads the RFC 9728 document served on the MCP
// host.
func fetchProtectedResourceMetadata(t *testing.T, ti *testInstance, mcpSlug string) map[string]any {
	t.Helper()

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleGetProtectedResource(w, wellKnownRequest(t, "/.well-known/oauth-protected-resource/mcp/", mcpSlug)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	return meta
}

func requireNotFound(t *testing.T, err error) {
	t.Helper()

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeNotFound, shareable.Code)
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

// mcpHostRoot is the endpoint's canonical URL on the MCP host: its resource.
func mcpHostRoot(ti *testInstance, mcpSlug string) string {
	return strings.TrimSuffix(ti.serverURL.String(), "/") + "/mcp/" + mcpSlug
}

// For an issuer that opts in, a client assertion naming the token URL on the
// authentication host authenticates a token request sent there, and the
// session it mints carries the authentication host issuer and the MCP host's
// resource.
func TestAuthenticationHost_AuthenticationHostAudienceAccepted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String
	authIssuer := testAuthenticationHostURL + "/mcp/" + slug

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	form := withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, authenticationHostTokenURL(slug)))
	form.Set("resource", mcpHostRoot(ti, slug))
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.False(t, *harness.passedThrough)
	require.Equal(t, authIssuer, accessTokenClaims(t, w.Body.Bytes())["iss"])

	// The issuer stays the other half of the accepted pair.
	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, client)
	w = harness.serve(t, http.MethodPost, "auth.example.com:443", "/mcp/"+slug+"/token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, authIssuer)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Metadata, served on the authentication host, advertises its token URL.
	w = harness.serve(t, http.MethodGet, "auth.example.com", "/.well-known/oauth-authorization-server/mcp/"+slug, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	require.Equal(t, authenticationHostTokenURL(slug), meta["token_endpoint"])
}

// The authentication host serves no route for an issuer that has not opted
// in: to that issuer it is a host that does not exist.
func TestAuthenticationHost_IssuerNotOptedInIsNotServed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	assertionClient := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, assertionClient)
	form := withAssertion(codeGrantForm(assertionClient, code, verifier), signer.assertion(t, assertionClient.ClientID, authenticationHostTokenURL(slug)))
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	w = harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/revoke", url.Values{"token": {"x"}, "client_id": {client.ClientID}})
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	// The clientless workload grant is refused by the host guard too: an
	// issuer that has not opted in is not served here at all, so the grant
	// never runs and cannot answer invalid_grant.
	w = harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", url.Values{
		"grant_type": {workloadGrantJWTBearer},
		"assertion":  {"not.a.token"},
		"resource":   {strings.TrimSuffix(ti.serverURL.String(), "/") + "/mcp/" + slug},
	})
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	authorize := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {client.RedirectUris[0]},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}
	w = harness.serve(t, http.MethodGet, "auth.example.com", "/mcp/"+slug+"/authorize?"+authorize.Encode(), nil)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	require.False(t, *harness.passedThrough)
}

// On the authentication host the accepted audiences are exactly the issuer and
// the authentication host's own token URL; nothing else on either host
// verifies.
func TestAuthenticationHost_OtherAudiencesRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String

	for _, aud := range []string{
		mcpHostRoot(ti, slug),
		mcpHostRoot(ti, slug) + "/token",
		testAuthenticationHostURL,
		testAuthenticationHostURL + "/mcp/" + slug + "/revoke",
		testAuthenticationHostURL + "/mcp/" + slug + "/token/",
		authenticationHostTokenURL(slug) + "?x=1",
	} {
		code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
		w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, aud)))
		require.Equal(t, http.StatusUnauthorized, w.Code, "aud %q: %s", aud, w.Body.String())
		require.Contains(t, w.Body.String(), "invalid_client")
	}

	// The authentication host's URL is not an audience on the MCP host.
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	w := postForm(t, ti, slug, "token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, authenticationHostTokenURL(slug))))
	requireInvalidClient(t, w)
}

// The canonical resource is the MCP host's, so a resource naming the
// authentication host matches nothing.
func TestAuthenticationHost_ResourceStaysOnMCPHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	form := withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, authenticationHostTokenURL(slug)))
	form.Set("resource", testAuthenticationHostURL+"/mcp/"+slug)
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_target")
}

// An authenticated ID-JAG exchange is not admitted on the authentication host.
func TestAuthenticationHost_IDJAGExchangeRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", "header.payload.signature")
	form.Set("client_id", "id-jag-client")
	form.Set("resource", "http://0.0.0.0/mcp/"+slug)
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "unsupported_grant_type")
}

// A JWT bearer request presenting no client reaches the workload grant on the
// authentication host and is refused on the assertion, not turned away as
// unsupported_grant_type by the host.
func TestAuthenticationHost_ClientlessAssertionGrantReachesClientlessBranch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", "header.payload.signature")
	form.Set("resource", "http://0.0.0.0/mcp/"+slug)
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_grant")
	require.NotContains(t, w.Body.String(), "unsupported_grant_type")
}

// MCP traffic, protected-resource metadata and anything outside the
// authorization server never answer on the authentication host.
func TestAuthenticationHost_NonAuthorizationServerRoutesNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/mcp/" + slug},
		{http.MethodGet, "/mcp/" + slug},
		{http.MethodDelete, "/mcp/" + slug},
		{http.MethodGet, "/mcp/" + slug + "/token"},
		{http.MethodGet, "/mcp/" + slug + "/install"},
		{http.MethodGet, "/.well-known/oauth-protected-resource/mcp/" + slug},
		{http.MethodGet, "/mcp/idp_callback"},
		{http.MethodGet, "/mcp/remote_login_callback"},
		{http.MethodPost, "/rpc/auth.info"},
		{http.MethodGet, "/"},
	} {
		w := harness.serve(t, route.method, "auth.example.com", route.path, nil)
		require.Equal(t, http.StatusNotFound, w.Code, "%s %s", route.method, route.path)
	}
	require.False(t, *harness.passedThrough)
}

// An issuer that has not opted in keeps its authorization server metadata on
// the MCP host. Serving it on the authentication host would hand clients a
// document whose issuer does not match the URL it came from.
func TestAuthenticationHost_IssuerNotOptedInKeepsMetadataOnMCPHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String

	w := harness.serve(t, http.MethodGet, "auth.example.com", "/.well-known/oauth-authorization-server/mcp/"+slug, nil)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, slug)
	require.Equal(t, strings.TrimSuffix(ti.serverURL.String(), "/")+"/mcp/"+slug, advertisedIssuer)
	require.Equal(t, []any{advertisedIssuer}, fetchProtectedResourceMetadata(t, ti, slug)["authorization_servers"])
}

// An issuer that opts in announces the authentication host everywhere a
// client learns its issuer, while the resource stays on the MCP host.
func TestAuthenticationHost_OptedInIssuerAnnouncesAuthenticationHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String
	authIssuer := testAuthenticationHostURL + "/mcp/" + slug

	w := harness.serve(t, http.MethodGet, "auth.example.com", "/.well-known/oauth-authorization-server/mcp/"+slug, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	require.Equal(t, authIssuer, meta["issuer"])
	require.Equal(t, authIssuer+"/authorize", meta["authorization_endpoint"])
	require.Equal(t, authIssuer+"/token", meta["token_endpoint"])
	require.Equal(t, authIssuer+"/register", meta["registration_endpoint"])
	require.Equal(t, authIssuer+"/revoke", meta["revocation_endpoint"])

	// The MCP host serves no document naming a different issuer.
	req := wellKnownRequest(t, "/.well-known/oauth-authorization-server/mcp/", slug)
	requireNotFound(t, ti.service.HandleGetAuthorizationServer(httptest.NewRecorder(), req))

	prm := fetchProtectedResourceMetadata(t, ti, slug)
	require.Equal(t, strings.TrimSuffix(ti.serverURL.String(), "/")+"/mcp/"+slug, prm["resource"])
	require.Equal(t, []any{authIssuer}, prm["authorization_servers"])
}

// A token minted for an opted-in issuer names the authentication host as its
// issuer, and the resource a client names is still the MCP host's.
func TestAuthenticationHost_OptedInTokensCarryAuthenticationHostIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String
	authIssuer := testAuthenticationHostURL + "/mcp/" + slug

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	form := withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, authIssuer))
	form.Set("resource", strings.TrimSuffix(ti.serverURL.String(), "/")+"/mcp/"+slug)
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, authIssuer, accessTokenClaims(t, w.Body.Bytes())["iss"])

	// The MCP host's token endpoint mints the same issuer.
	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, client)
	w = postForm(t, ti, slug, "token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, authIssuer)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, authIssuer, accessTokenClaims(t, w.Body.Bytes())["iss"])
}

// Authorization on the authentication host keeps the user there: consent is
// served where the issuer lives, and the RFC 8707 resource is the MCP host's.
func TestAuthenticationHost_OptedInAuthorizeRedirectsToConsentOnAuthenticationHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	toolset, err := toolsets_repo.New(ti.conn).UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
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
	harness := newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String
	authIssuer := testAuthenticationHostURL + "/mcp/" + slug

	authorizePath := func(resource string) string {
		q := url.Values{
			"response_type":         {"code"},
			"client_id":             {client.ClientID},
			"redirect_uri":          {client.RedirectUris[0]},
			"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
			"code_challenge_method": {"S256"},
			"resource":              {resource},
		}
		return "/mcp/" + slug + "/authorize?" + q.Encode()
	}

	w := harness.serve(t, http.MethodGet, "auth.example.com", authorizePath(strings.TrimSuffix(ti.serverURL.String(), "/")+"/mcp/"+slug), nil)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), authIssuer+"/connect?"), w.Header().Get("Location"))

	// A resource naming the authentication host is some other server; the
	// error goes back to the client carrying the authentication host issuer.
	w = harness.serve(t, http.MethodGet, "auth.example.com", authorizePath(authIssuer), nil)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "invalid_target", loc.Query().Get("error"))
	require.Equal(t, authIssuer, loc.Query().Get("iss"))
}

// Consent completing a flow minted on the platform host stamps the
// authentication host issuer for an opted-in issuer.
func TestAuthenticationHost_OptedInConsentEmitsAuthenticationHostIss(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	_ = newAuthenticationHostHarness(t, ti)
	slug := toolset.McpSlug.String

	loc := postConsent(t, ctx, ctx, ti, consentPostOpts{
		mcpSlug:        slug,
		issuerID:       issuer.ID,
		clientID:       client.ClientID,
		redirectURI:    client.RedirectUris[0],
		baseURL:        ti.serverURL.String(),
		customDomainID: uuid.NullUUID{},
		action:         "approve",
	})
	require.NotEmpty(t, loc.Query().Get("code"))
	require.Equal(t, testAuthenticationHostURL+"/mcp/"+slug, loc.Query().Get("iss"))
}

// Requests for any other host reach the main mux untouched.
func TestAuthenticationHost_OtherHostsPassThrough(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	harness := newAuthenticationHostHarness(t, ti)

	w := harness.serve(t, http.MethodPost, "0.0.0.0", "/mcp/some-slug/token", url.Values{})
	require.Equal(t, http.StatusTeapot, w.Code)
	require.True(t, *harness.passedThrough)
}

func TestNewAuthenticationHost_Disabled(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	authenticationHost, err := mcp.NewAuthenticationHost("", serverURL, "prod")
	require.NoError(t, err)

	passed := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed = true })
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.Host = "auth.example.com"
	authenticationHost.Middleware(next).ServeHTTP(httptest.NewRecorder(), req)
	require.True(t, passed)
}

func TestNewAuthenticationHost_InvalidURLsRefused(t *testing.T) {
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
		_, err := mcp.NewAuthenticationHost(raw, serverURL, "prod")
		require.Error(t, err, raw)
	}

	_, err = mcp.NewAuthenticationHost("https://auth.example.com/", serverURL, "prod")
	require.NoError(t, err)

	localServerURL, err := url.Parse("http://localhost:8080")
	require.NoError(t, err)
	_, err = mcp.NewAuthenticationHost("http://127.0.0.1:8080", localServerURL, "local")
	require.NoError(t, err)
}
