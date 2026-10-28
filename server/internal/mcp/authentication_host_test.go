package mcp_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
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
	mcp.AttachAuthenticationHost(authenticationHost, ti.service.Service)

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
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return h.serveRequest(host, req)
}

// serveRequest serves req through the harness, addressed to host.
func (h authenticationHostHarness) serveRequest(host string, req *http.Request) *httptest.ResponseRecorder {
	req.Host = host
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
		require.Equal(t, http.StatusBadRequest, w.Code, "aud %q: %s", aud, w.Body.String())
		require.Contains(t, w.Body.String(), "invalid_client")
	}

	// The authentication host's URL is not an audience on the MCP host.
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	w := postForm(t, ti, slug, "token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, authenticationHostTokenURL(slug))))
	requireInvalidClient(t, w, http.StatusBadRequest)
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

// authenticationHostIDJAGUpstreamIssuer is the upstream identity provider that
// signs ID-JAG assertions in the authentication host tests.
const authenticationHostIDJAGUpstreamIssuer = "https://identity.example.test"

// authenticationHostIDJAGFixture is an MCP server whose issuer has an ID-JAG
// trust link and opts in to the authentication host.
type authenticationHostIDJAGFixture struct {
	ti       *testInstance
	signer   idJAGTestAssertion
	harness  authenticationHostHarness
	slug     string
	clientID string
}

func newAuthenticationHostIDJAGFixture(t *testing.T) authenticationHostIDJAGFixture {
	t.Helper()

	ctx, ti, signer, jwksURL := newIDJAGTestService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, authenticationHostIDJAGUpstreamIssuer, jwksURL)
	toolset, err := toolsets_repo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)
	client := seedCIMDClient(t, ctx, ti, issuer.ID, codexCIMDClientID)
	seedIDJAGDirectoryUser(t, ctx, ti, authCtx.ActiveOrganizationID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), toolset.ID)
	useAuthenticationHost(t, ctx, ti, authCtx.ActiveOrganizationID, issuer.ID)

	return authenticationHostIDJAGFixture{
		ti:       ti,
		signer:   signer,
		harness:  newAuthenticationHostHarness(t, ti),
		slug:     toolset.McpSlug.String,
		clientID: client.ClientID,
	}
}

// exchange sends the token endpoint on the authentication host an ID-JAG
// addressed to audience.
func (f authenticationHostIDJAGFixture) exchange(t *testing.T, audience string) *httptest.ResponseRecorder {
	t.Helper()

	assertion := f.signer.sign(t, authenticationHostIDJAGUpstreamIssuer, audience, mcpHostRoot(f.ti, f.slug), f.clientID, mockidp.MockUserEmail, uuid.NewString())
	return f.harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+f.slug+"/token", url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamClientID:  {f.clientID},
		oauthwire.ParamAssertion: {assertion},
	})
}

// The ID-JAG exchange on the authentication host accepts an assertion
// addressed to the issuer the metadata announces there, and binds the session
// to the MCP host's resource.
func TestAuthenticationHost_IDJAGExchangeAddressedToIssuer(t *testing.T) {
	t.Parallel()

	f := newAuthenticationHostIDJAGFixture(t)
	authIssuer := testAuthenticationHostURL + "/mcp/" + f.slug

	w := f.exchange(t, authIssuer)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, authIssuer, claims["iss"])
	require.Equal(t, []any{mcpHostRoot(f.ti, f.slug)}, claims["aud"])
}

// An ID-JAG addressed to the resource rather than the issuer is refused once
// the issuer has moved to the authentication host.
func TestAuthenticationHost_IDJAGExchangeRefusesResourceAudience(t *testing.T) {
	t.Parallel()

	f := newAuthenticationHostIDJAGFixture(t)

	w := f.exchange(t, mcpHostRoot(f.ti, f.slug))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, oautherr.CodeInvalidGrant, decodeSharedTokenResponse(t, w).Error)
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
		{http.MethodGet, "/mcp/idp_callback/0190a6f8-5b2c-7d3e-8f41-2a6b9c0d1e2f"},
		{http.MethodGet, "/mcp/remote_login_callback"},
		{http.MethodGet, "/mcp/remote_login_bind"},
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

// seedAuthenticationHostEndpointIssuer is an issuer in endpoint mode, opted in
// to the authentication host, with a registered public client and public MCP
// servers.
func seedAuthenticationHostEndpointIssuer(t *testing.T, ctx context.Context, ti *testInstance) sharedIssuerFixture {
	t.Helper()

	f := seedSharedIssuer(t, ctx, ti)
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeEndpoint), "")
	useAuthenticationHost(t, ctx, ti, f.issuer.OrganizationID.String, f.issuer.ID)
	return f
}

// authorizeOnAuthenticationHost starts a flow for slug on the authentication
// host and returns the challenge its consent page serves there.
func authorizeOnAuthenticationHost(t *testing.T, ctx context.Context, ti *testInstance, harness authenticationHostHarness, f sharedIssuerFixture, slug string) mcp.AuthnChallengeState {
	t.Helper()

	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {f.client.ClientID},
		"redirect_uri":          {sharedTestRedirectURI},
		"state":                 {"client-state"},
		"code_challenge":        {pkceChallenge(pkceVerifier(t))},
		"code_challenge_method": {"S256"},
	}
	authorized := harness.serve(t, http.MethodGet, "auth.example.com", "/mcp/"+slug+"/authorize?"+query.Encode(), nil)
	require.Equal(t, http.StatusFound, authorized.Code, authorized.Body.String())
	consentURL, err := url.Parse(authorized.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, testAuthenticationHostURL+"/mcp/"+slug+"/connect", consentURL.Scheme+"://"+consentURL.Host+consentURL.Path)
	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentURL.Query().Get("state"))
	require.NoError(t, err)
	return stored
}

// A remote session connect from the consent page on the authentication host
// returns there, not to the MCP host the resource lives on.
func TestAuthenticationHost_RemoteConnectReturnsToAuthenticationHostConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	f := seedAuthenticationHostEndpointIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	remoteClientID := createConsentRemoteClient(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "authn-host-rc-"+uuid.NewString()[:8], "", []uuid.UUID{f.issuer.ID})
	stored := authorizeOnAuthenticationHost(t, ctx, ti, harness, f, f.slugA)

	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+f.slugA+"/connect/remote-session", url.Values{"state": {stored.ID}, "csrf_token": {stored.CSRFToken}, "action": {"connect"}, "client_id": {remoteClientID.String()}})
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	upstream, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)

	loginCache := cache.NewTypedObjectCache[remotesessions.RemoteLoginState](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	login, err := loginCache.Get(ctx, "remoteLogin:"+upstream.Query().Get("state"))
	require.NoError(t, err)
	require.Equal(t, testAuthenticationHostURL+"/mcp/"+f.slugA+"/connect?state="+stored.ID, login.ConsentURL)
}

// Turning use_authentication_host off mid-flow moves the issuer, so a flow
// started on the authentication host cannot finish on the MCP host under an
// issuer its client never recorded.
func TestAuthenticationHost_ToggleClosesInFlightFlows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedAuthenticationHostEndpointIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	stored := authorizeOnAuthenticationHost(t, ctx, ti, harness, f, f.slugA)

	updated, err := testrepo.New(ti.conn).SetUserSessionIssuerUseAuthenticationHostFixture(ctx, testrepo.SetUserSessionIssuerUseAuthenticationHostFixtureParams{
		UseAuthenticationHost: false,
		IssuerID:              f.issuer.ID,
		OrganizationID:        f.issuer.OrganizationID.String,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated)

	form := url.Values{"state": {stored.ID}, "csrf_token": {stored.CSRFToken}, "action": {"approve"}}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+f.slugA+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", f.slugA)
	err = ti.service.HandleConsent(httptest.NewRecorder(), req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx)))
	require.Contains(t, requireOopsCode(t, err, oops.CodeBadRequest), "authn challenge state does not match this MCP server")
}

// completeAuthenticationHostFederatedLogin finishes a federated login whose
// first response, start, came from the authentication host, with the IdP
// callback on the platform host. The callback binds its own cookie and hands
// the browser to the consent page at consentURL, which must recognise the
// initiating browser before and after the login.
func completeAuthenticationHostFederatedLogin(t *testing.T, ctx context.Context, f *federationLoginFixture, harness authenticationHostHarness, start *httptest.ResponseRecorder, consentURL string) {
	t.Helper()

	ti, provider := f.ti, f.provider
	callbackURL := ti.serverURL.String() + f.callbackPath()
	require.Equal(t, http.StatusFound, start.Code, start.Body.String())
	bootstrap, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, callbackURL, bootstrap.Scheme+"://"+bootstrap.Host+bootstrap.Path)
	originCookies := slices.DeleteFunc(start.Result().Cookies(), func(cookie *http.Cookie) bool {
		return !strings.HasPrefix(cookie.Name, "__Host-gram-federation-")
	})
	require.Len(t, originCookies, 1)

	// The callback host holds no cookie yet, so it binds one and sends the
	// browser to the consent page.
	handoff := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(handoff, routeIDPCallback(httptest.NewRequestWithContext(ctx, http.MethodGet, bootstrap.String(), nil))))
	require.Equal(t, http.StatusFound, handoff.Code, handoff.Body.String())
	consent, err := url.Parse(handoff.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, consentURL, consent.Scheme+"://"+consent.Host+consent.Path)
	callbackCookies := handoff.Result().Cookies()
	require.Len(t, callbackCookies, 1)

	confirm := httptest.NewRequestWithContext(ctx, http.MethodGet, consent.RequestURI(), nil)
	confirm.AddCookie(originCookies[0])
	ready := harness.serveRequest("auth.example.com", confirm)
	require.Equal(t, http.StatusFound, ready.Code, ready.Body.String())
	readyURL, err := url.Parse(ready.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, callbackURL, readyURL.Scheme+"://"+readyURL.Host+readyURL.Path)

	begin := httptest.NewRecorder()
	beginRequest := httptest.NewRequestWithContext(ctx, http.MethodGet, readyURL.String(), nil)
	beginRequest.AddCookie(callbackCookies[0])
	require.NoError(t, ti.service.HandleIDPCallback(begin, routeIDPCallback(beginRequest)))
	upstream, err := url.Parse(begin.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, provider.URL+"/authorize", upstream.Scheme+"://"+upstream.Host+upstream.Path)

	id := upstream.Query().Get("state")
	provider.issueCode(t, "authentication-host-code", federationToken{challenge: upstream.Query().Get("code_challenge"), nonce: upstream.Query().Get("nonce"), email: mockidp.MockUserEmail, issuer: provider.URL, verified: true, secret: "selected-secret"})
	callback := httptest.NewRequestWithContext(ctx, http.MethodGet, callbackURL+"?"+url.Values{"state": {id}, "code": {"authentication-host-code"}, "iss": {provider.URL}}.Encode(), nil)
	callback.AddCookie(callbackCookies[0])
	result := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(result, routeIDPCallback(callback)))
	require.Equal(t, http.StatusFound, result.Code, result.Body.String())
	connect, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, consentURL, connect.Scheme+"://"+connect.Host+connect.Path)

	page := httptest.NewRequestWithContext(ctx, http.MethodGet, connect.RequestURI(), nil)
	page.AddCookie(originCookies[0])
	rendered := harness.serveRequest("auth.example.com", page)
	require.Equal(t, http.StatusOK, rendered.Code, rendered.Body.String())
}

// federatedAuthorizeQuery is the downstream client's authorization request in
// the federated login tests.
func federatedAuthorizeQuery(f *federationLoginFixture) url.Values {
	return url.Values{"response_type": {"code"}, "client_id": {f.downstreamClientID}, "redirect_uri": {"http://127.0.0.1/callback"}, "state": {"downstream-state"}, "code_challenge": {"downstream-pkce"}, "code_challenge_method": {"S256"}}
}

// Federated login started on the authentication host binds the browser there,
// for an MCP server whose issuer opts in to it.
func TestAuthenticationHost_FederatedLoginBindsBrowserOnAuthenticationHost(t *testing.T) {
	t.Parallel()

	ctx, f := newFederationLoginFixture(t, true)
	useAuthenticationHost(t, ctx, f.ti, f.organizationID, f.issuerID)
	harness := newAuthenticationHostHarness(t, f.ti)

	start := harness.serve(t, http.MethodGet, "auth.example.com", "/mcp/"+f.toolsetSlug+"/authorize?"+federatedAuthorizeQuery(f).Encode(), nil)
	completeAuthenticationHostFederatedLogin(t, ctx, f, harness, start, testAuthenticationHostURL+"/mcp/"+f.toolsetSlug+"/connect")
}

// The dashboard starts first-party connects on the platform host. For an
// issuer that opts in to the authentication host, the flow moves there before
// binding the browser, so the consent page on the authentication host finds
// the browser's cookie.
func TestAuthenticationHost_FederatedFirstPartyConnectMovesToAuthenticationHost(t *testing.T) {
	t.Parallel()

	ctx, f := newFederationLoginFixture(t, true)
	useAuthenticationHost(t, ctx, f.ti, f.organizationID, f.issuerID)
	harness := newAuthenticationHostHarness(t, f.ti)
	path := "/mcp/" + f.toolsetSlug + "/connect/first-party"

	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", f.toolsetSlug)
	moved := httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleFirstPartyConnect(moved, httptest.NewRequestWithContext(context.WithValue(ctx, chi.RouteCtxKey, route), http.MethodGet, path, nil)))
	require.Equal(t, http.StatusFound, moved.Code, moved.Body.String())
	require.Equal(t, testAuthenticationHostURL+path, moved.Header().Get("Location"))
	require.Empty(t, moved.Result().Cookies(), "no browser binding is set on the host the flow left")

	start := harness.serve(t, http.MethodGet, "auth.example.com", path, nil)
	completeAuthenticationHostFederatedLogin(t, ctx, f, harness, start, testAuthenticationHostURL+"/mcp/"+f.toolsetSlug+"/connect")
}

// Federated login through a shared authorization server on the authentication
// host binds the browser there.
func TestAuthenticationHost_SharedFederatedLoginBindsBrowserOnAuthenticationHost(t *testing.T) {
	t.Parallel()

	ctx, f := newFederationLoginFixture(t, true)
	harness := newAuthenticationHostHarness(t, f.ti)
	issuerURL := authenticationHostSharedIssuerURL(f.issuerID.String())
	setIssuerMode(t, ctx, f.ti, f.issuerID, string(authserver.ModeShared), issuerURL)

	query := federatedAuthorizeQuery(f)
	query.Set("resource", mcpHostRoot(f.ti, f.toolsetSlug))
	start := harness.serve(t, http.MethodGet, "auth.example.com", "/oauth/usi/"+f.issuerID.String()+"/authorize?"+query.Encode(), nil)
	completeAuthenticationHostFederatedLogin(t, ctx, f, harness, start, issuerURL+"/connect")
}

// An authorization request for an issuer that opts in to the authentication
// host, sent to the MCP host by a client holding older metadata, moves to the
// authentication host unchanged.
func TestAuthenticationHost_OptedInAuthorizeOnMCPHostMoves(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedAuthenticationHostEndpointIssuer(t, ctx, ti)
	newAuthenticationHostHarness(t, ti)
	path := "/mcp/" + f.slugA + "/authorize?" + url.Values{"response_type": {"code"}, "client_id": {f.client.ClientID}, "redirect_uri": {sharedTestRedirectURI}, "state": {"client-state"}}.Encode()

	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", f.slugA)
	moved := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(moved, httptest.NewRequestWithContext(context.WithValue(ctx, chi.RouteCtxKey, route), http.MethodGet, path, nil)))
	require.Equal(t, http.StatusFound, moved.Code, moved.Body.String())
	require.Equal(t, testAuthenticationHostURL+path, moved.Header().Get("Location"))
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
