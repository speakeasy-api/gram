package fixture_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed/fixture"
	"github.com/speakeasy-api/gram/server/internal/demoseed/fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func startFixture(t *testing.T, ttl time.Duration) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	base := "http://" + srv.Listener.Addr().String()
	srv.Config.Handler = fixture.New(fixture.Config{BaseURL: base, AccessTokenTTL: ttl, SuggestedEmails: []string{"alex@fixture.test"}}).Handler()
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, base
}

// fixtureHTTPClient is the client the fixture uses for outbound calls, built
// the way the Speakeasy server builds it, without the SSRF block list so it
// reaches test servers on loopback.
func fixtureHTTPClient(t *testing.T) *guardian.HTTPClient {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return policy.Client()
}

func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// decodeBody decodes a JSON response body. The caller closes it.
func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&v))
	return v
}

// getJSON fetches u and decodes its JSON body.
func getJSON[T any](t *testing.T, u string) T {
	t.Helper()
	resp := get(t, u)
	defer func() { _ = resp.Body.Close() }()
	return decodeBody[T](t, resp)
}

// postFormJSON posts form to u and decodes its JSON body.
func postFormJSON[T any](t *testing.T, u string, form url.Values) T {
	t.Helper()
	resp := postForm(t, u, form)
	defer func() { _ = resp.Body.Close() }()
	return decodeBody[T](t, resp)
}

// register registers a client with the authorization server at issuer and
// returns its client id.
func register(t *testing.T, issuer, redirect string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, issuer+"/oauth/register",
		strings.NewReader(`{"redirect_uris":["`+redirect+`"],"client_name":"Test client","token_endpoint_auth_method":"none"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	clientID, ok := decodeBody[map[string]any](t, resp)["client_id"].(string)
	require.True(t, ok, "client_id is a string")
	return clientID
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
}

// signIn runs registration, the authorization page and the code exchange
// the way an MCP client does, and returns the token response.
func signIn(t *testing.T, base, email string) tokenResponse {
	t.Helper()
	ctx := t.Context()
	redirect := "https://client.example.test/callback"

	// Discovery: the protected resource names the authorization server.
	prm := getJSON[map[string]any](t, base+"/.well-known/oauth-protected-resource/mcp/oauth")
	require.Equal(t, []any{base}, prm["authorization_servers"])
	asm := getJSON[map[string]any](t, base+"/.well-known/oauth-authorization-server")
	require.Equal(t, base, asm["issuer"])
	require.Equal(t, true, asm["client_id_metadata_document_supported"])

	// Dynamic client registration.
	clientID := register(t, base, redirect)

	verifier := "a-long-enough-pkce-code-verifier-for-the-fixture-test-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {"xyz"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "resource": {fixtureauth.OAuthResource(base)},
	}

	// The sign-in page renders the client and the suggested email.
	page := get(t, base+"/oauth/authorize?"+q.Encode())
	require.Equal(t, http.StatusOK, page.StatusCode)
	_ = page.Body.Close()

	// Picking an email redirects back with a code.
	form := url.Values{}
	maps.Copy(form, q)
	form.Set("email", email)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/oauth/authorize", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := noRedirect().Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "xyz", loc.Query().Get("state"))
	require.Equal(t, base, loc.Query().Get("iss"))
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	// A wrong verifier is rejected; the right one gets tokens, once.
	bad := postFormJSON[tokenResponse](t, base+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {"wrong"}})
	require.Equal(t, "invalid_grant", bad.Error)
	// The failed attempt spent the code, so sign in again for a fresh one.
	return exchangeFresh(t, base, clientID, redirect, email, verifier, q)
}

func exchangeFresh(t *testing.T, base, clientID, redirect, email, verifier string, q url.Values) tokenResponse {
	t.Helper()
	form := url.Values{}
	maps.Copy(form, q)
	form.Set("email", email)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/oauth/authorize", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := noRedirect().Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")

	tok := postFormJSON[tokenResponse](t, base+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {verifier}})
	require.Empty(t, tok.Error)
	require.NotEmpty(t, tok.AccessToken)
	require.NotEmpty(t, tok.RefreshToken)

	again := postFormJSON[tokenResponse](t, base+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {verifier}})
	require.Equal(t, "invalid_grant", again.Error, "a code works once")
	return tok
}

func get(t *testing.T, u string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
	require.NoError(t, err)
	resp, err := noRedirect().Do(req)
	require.NoError(t, err)
	return resp
}

func postForm(t *testing.T, u string, form url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, u, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}
	return resp, nil
}

func callTool(t *testing.T, endpoint, token, tool string, args map[string]any) map[string]any {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Transport: bearerTransport{token: token}}}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool %s failed: %v", tool, res.Content)
	out, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "structured content: %T", res.StructuredContent)
	return out
}

func TestOAuthSignInAndRefresh(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)

	tok := signIn(t, base, "alex@fixture.test")

	who := callTool(t, base+fixtureauth.PathOAuthMCP, tok.AccessToken, "whoami", nil)
	require.Equal(t, "alex@fixture.test", who["identity"])
	require.Equal(t, "oauth", who["auth"])

	notes := callTool(t, base+fixtureauth.PathOAuthMCP, tok.AccessToken, "add_note", map[string]any{"text": "hello"})
	require.Equal(t, []any{"hello"}, notes["notes"])

	// Refresh rotates: the new refresh token works, the old one is spent.
	fresh := postFormJSON[tokenResponse](t, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}})
	require.Empty(t, fresh.Error)
	require.NotEqual(t, tok.RefreshToken, fresh.RefreshToken)
	reused := postFormJSON[tokenResponse](t, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}})
	require.Equal(t, "invalid_grant", reused.Error)

	who = callTool(t, base+fixtureauth.PathOAuthMCP, fresh.AccessToken, "whoami", nil)
	require.Equal(t, "alex@fixture.test", who["identity"])

	// Revoking the access token locks it out.
	resp := postForm(t, base+"/oauth/revoke", url.Values{"token": {fresh.AccessToken}})
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+fixtureauth.PathOAuthMCP, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+fresh.AccessToken)
	denied, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = denied.Body.Close()
	require.Equal(t, http.StatusUnauthorized, denied.StatusCode)
}

func TestOAuthEndpointChallengesWithoutToken(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+fixtureauth.PathOAuthMCP, strings.NewReader(`{}`))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Contains(t, resp.Header.Get("WWW-Authenticate"), `resource_metadata="`+base+`/.well-known/oauth-protected-resource/mcp/oauth"`)
}

func TestExpiredAccessTokenIsRejected(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)
	pair, err := fixtureauth.IssueTokens(base, fixtureauth.OAuthResource(base), "seeded-client", "sam@fixture.test", nil, time.Minute, time.Now().Add(-2*time.Hour))
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+fixtureauth.PathOAuthMCP, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Its refresh token still works: that is how an expired connection
	// recovers without signing in again.
	fresh := postFormJSON[tokenResponse](t, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {pair.RefreshToken}})
	require.Empty(t, fresh.Error)
}

func TestSeededTokensWork(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)
	pair, err := fixtureauth.IssueTokens(base, fixtureauth.OAuthResource(base), "seeded-client", "sam@fixture.test", nil, time.Hour, time.Now())
	require.NoError(t, err)
	who := callTool(t, base+fixtureauth.PathOAuthMCP, pair.AccessToken, "whoami", nil)
	require.Equal(t, "sam@fixture.test", who["identity"])
}

func TestSharedEndpoint(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)

	who := callTool(t, base+fixtureauth.PathSharedMCP, fixtureauth.SharedToken, "whoami", nil)
	require.Equal(t, fixtureauth.SharedIdentity, who["identity"])
	require.Equal(t, "shared-token", who["auth"])

	// A caller's own fixture token also works there.
	pair, err := fixtureauth.IssueTokens(base, fixtureauth.OAuthResource(base), "seeded-client", "sam@fixture.test", nil, time.Hour, time.Now())
	require.NoError(t, err)
	who = callTool(t, base+fixtureauth.PathSharedMCP, pair.AccessToken, "whoami", nil)
	require.Equal(t, "sam@fixture.test", who["identity"])
	require.Equal(t, "own-token", who["auth"])

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+fixtureauth.PathSharedMCP, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAPIStub(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)
	body := getJSON[map[string]any](t, base+fixtureauth.PathAPI+"/customers/get?id=cus_42")
	require.Equal(t, "cus_42", body["id"])
	health := getJSON[map[string]any](t, base+fixtureauth.PathAPI+"/health/check")
	require.Equal(t, "ok", health["status"])
}

func TestTokensAreBoundToTheirResource(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)
	dcr := fixtureauth.DCRIssuer(base)
	autoURL := fixtureauth.AutoResource(base)

	auto, err := fixtureauth.IssueTokens(dcr, autoURL, "client", "sam@fixture.test", nil, time.Hour, time.Now())
	require.NoError(t, err)
	who := callTool(t, autoURL, auto.AccessToken, "whoami", nil)
	require.Equal(t, "sam@fixture.test", who["identity"])

	unauthorized := func(endpoint, token string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, strings.NewReader(`{}`))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, endpoint)
	}

	// The same token is refused at the other OAuth endpoint, and the main
	// authorization server's token is refused at the DCR server's endpoint.
	unauthorized(base+fixtureauth.PathOAuthMCP, auto.AccessToken)
	mainIssued, err := fixtureauth.IssueTokens(base, autoURL, "client", "sam@fixture.test", nil, time.Hour, time.Now())
	require.NoError(t, err)
	unauthorized(autoURL, mainIssued.AccessToken)

	// A token for another server's resource (External OAuth) works nowhere here.
	foreign, err := fixtureauth.IssueTokens(base, "https://speakeasy.test/mcp/external", "client", "sam@fixture.test", nil, time.Hour, time.Now())
	require.NoError(t, err)
	for _, endpoint := range []string{base + fixtureauth.PathOAuthMCP, autoURL, base + fixtureauth.PathSharedMCP} {
		unauthorized(endpoint, foreign.AccessToken)
	}
}

// The second authorization server takes only dynamically registered clients,
// so a sign-in set up against it goes through DCR.
func TestDCRServerTakesOnlyRegisteredClients(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, base := startFixture(t, time.Hour)
	dcr := fixtureauth.DCRIssuer(base)
	autoURL := fixtureauth.AutoResource(base)
	redirect := "https://client.example.test/callback"

	// Discovery, from the well-known locations at the host root and from the
	// 401's resource_metadata.
	prm := getJSON[map[string]any](t, base+"/.well-known/oauth-protected-resource"+fixtureauth.PathDCR+fixtureauth.PathAutoMCP)
	require.Equal(t, autoURL, prm["resource"])
	require.Equal(t, []any{dcr}, prm["authorization_servers"])
	prm = getJSON[map[string]any](t, dcr+"/.well-known/oauth-protected-resource"+fixtureauth.PathAutoMCP)
	require.Equal(t, autoURL, prm["resource"])
	for _, metadataURL := range []string{base + "/.well-known/oauth-authorization-server" + fixtureauth.PathDCR, dcr + "/.well-known/oauth-authorization-server"} {
		asm := getJSON[map[string]any](t, metadataURL)
		require.Equal(t, dcr, asm["issuer"], metadataURL)
		require.Equal(t, false, asm["client_id_metadata_document_supported"], metadataURL)
		require.Equal(t, dcr+"/oauth/register", asm["registration_endpoint"], metadataURL)
		docsURL, ok := asm["service_documentation"].(string)
		require.True(t, ok, "service_documentation is a string")
		docs := get(t, docsURL)
		_ = docs.Body.Close()
		require.Equal(t, http.StatusOK, docs.StatusCode, "service_documentation must resolve")
	}

	verifier := "a-long-enough-pkce-code-verifier-for-the-fixture-test-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	authorize := func(clientID string) url.Values {
		return url.Values{
			"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {"xyz"},
			"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "resource": {autoURL},
		}
	}

	// A metadata document client is refused.
	refused := get(t, dcr+"/oauth/authorize?"+authorize("https://client.example.test/client.json").Encode())
	_ = refused.Body.Close()
	require.NotEqual(t, http.StatusOK, refused.StatusCode)

	// A registered client signs in and calls a tool.
	clientID := register(t, dcr, redirect)
	q := authorize(clientID)
	page := get(t, dcr+"/oauth/authorize?"+q.Encode())
	_ = page.Body.Close()
	require.Equal(t, http.StatusOK, page.StatusCode)

	form := url.Values{}
	maps.Copy(form, q)
	form.Set("email", "sam@fixture.test")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dcr+"/oauth/authorize", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := noRedirect().Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, dcr, loc.Query().Get("iss"))

	tok := postFormJSON[tokenResponse](t, dcr+"/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "client_id": {clientID},
		"redirect_uri": {redirect}, "code_verifier": {verifier}, "resource": {autoURL},
	})
	require.Empty(t, tok.Error)
	who := callTool(t, autoURL, tok.AccessToken, "whoami", nil)
	require.Equal(t, "sam@fixture.test", who["identity"])
}

func TestOpenEndpointNeedsNoCredentials(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)
	who := callTool(t, base+fixtureauth.PathOpenMCP, "", "whoami", nil)
	require.Equal(t, "anonymous", who["identity"])
	require.Equal(t, "none", who["auth"])
}

func TestPersonalTokenPage(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)
	resp := postForm(t, base+"/tokens", url.Values{"email": {"pat@fixture.test"}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body strings.Builder
	_, err := io.Copy(&body, resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	_, after, found := strings.Cut(body.String(), fixtureauth.OwnTokenHeader+": ")
	require.True(t, found, "the page shows the header to send")
	tok, _, _ := strings.Cut(after, "<")
	who := callTool(t, base+fixtureauth.PathSharedMCP, tok, "whoami", nil)
	require.Equal(t, "pat@fixture.test", who["identity"])
	require.Equal(t, "own-token", who["auth"])
}

// fakeResourceAS stands in for the dev-idp's resource authorization server
// introspection endpoint: active holds the tokens it reports as active.
func fakeResourceAS(t *testing.T, active map[string]map[string]any) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /resource-as/"+fixtureauth.EMAResourceSlug+"/introspect", func(w http.ResponseWriter, r *http.Request) {
		if !assert.NoError(t, r.ParseForm()) {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		body, ok := active[r.Form.Get("token")]
		if !ok {
			body = map[string]any{"active": false}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
	idp := httptest.NewServer(mux)
	t.Cleanup(idp.Close)
	return idp.URL
}

func startEMAFixture(t *testing.T, devIDPURL string) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	base := "http://" + srv.Listener.Addr().String()
	srv.Config.Handler = fixture.New(fixture.Config{BaseURL: base, AccessTokenTTL: time.Hour, DevIDPURL: devIDPURL, HTTPClient: fixtureHTTPClient(t)}).Handler()
	srv.Start()
	t.Cleanup(srv.Close)
	return base
}

func TestEMAEndpoint(t *testing.T) {
	t.Parallel()
	exp := time.Now().Add(time.Hour).Unix()
	tokens := map[string]map[string]any{}
	devIDP := fakeResourceAS(t, tokens)
	base := startEMAFixture(t, devIDP)
	resource := fixtureauth.Resource(base, fixtureauth.PathEMAMCP)
	authServer := fixtureauth.EMAAuthorizationServer(devIDP)
	tokens["good"] = map[string]any{"active": true, "scope": fixtureauth.EMAScope, "username": "dev@fixture.test", "exp": exp, "aud": resource, "iss": authServer}
	tokens["other-resource"] = map[string]any{"active": true, "scope": fixtureauth.EMAScope, "username": "dev@fixture.test", "exp": exp, "aud": "https://elsewhere.test/mcp", "iss": authServer}
	tokens["other-issuer"] = map[string]any{"active": true, "scope": fixtureauth.EMAScope, "username": "dev@fixture.test", "exp": exp, "aud": resource, "iss": "https://elsewhere.test/as"}

	// The protected resource names the dev-idp's resource authorization server.
	meta := getJSON[map[string]any](t, base+"/.well-known/oauth-protected-resource"+fixtureauth.PathEMAMCP)
	require.Equal(t, resource, meta["resource"])
	require.Equal(t, []any{authServer}, meta["authorization_servers"])

	who := callTool(t, base+fixtureauth.PathEMAMCP, "good", "whoami", nil)
	require.Equal(t, "dev@fixture.test", who["identity"])
	require.Equal(t, "ema", who["auth"])
	require.Equal(t, []any{fixtureauth.EMAScope}, who["scopes"])

	for _, tok := range []string{"", "unknown", "other-resource", "other-issuer"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+fixtureauth.PathEMAMCP, strings.NewReader(`{}`))
		require.NoError(t, err)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, tok)
		require.Contains(t, resp.Header.Get("WWW-Authenticate"), `resource_metadata="`+base+`/.well-known/oauth-protected-resource/mcp/ema"`, tok)
	}

	// The fixture's own OAuth tokens are not accepted there.
	pair, err := fixtureauth.IssueTokens(base, resource, "client", "sam@fixture.test", nil, time.Hour, time.Now())
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+fixtureauth.PathEMAMCP, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestEMAEndpointNeedsTheDevIDP(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)
	resp := get(t, base+"/.well-known/oauth-protected-resource"+fixtureauth.PathEMAMCP)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

type headerTransport struct{ name, value string }

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if h.value != "" {
		r.Header.Set(h.name, h.value)
	}
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}
	return resp, nil
}

func TestOwnTokenEndpoint(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)
	connect := func(token string) *mcp.ClientSession {
		client := mcp.NewClient(&mcp.Implementation{Name: "fixture-test", Version: "1.0.0"}, nil)
		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint:   base + fixtureauth.PathOwnTokenMCP,
			HTTPClient: &http.Client{Transport: headerTransport{name: fixtureauth.OwnTokenHeader, value: token}},
		}, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = session.Close() })
		return session
	}

	// Anyone can connect and list tools, so a setup probe sees a working
	// server, but a tool call without a token is refused.
	anonymous := connect("")
	tools, err := anonymous.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, tools.Tools)
	res, err := anonymous.CallTool(t.Context(), &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, res.IsError, "a call without the caller's own token must fail")

	pair, err := fixtureauth.IssueTokens(base, fixtureauth.Resource(base, fixtureauth.PathSharedMCP), "personal-token", "pat@fixture.test", nil, time.Hour, time.Now())
	require.NoError(t, err)
	res, err = connect(pair.AccessToken).CallTool(t.Context(), &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	out, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "pat@fixture.test", out["identity"])
	require.Equal(t, "own-token", out["auth"])

	// A typed pass-through client forwards the caller's token as
	// Authorization instead, raw or as a bearer token.
	for _, value := range []string{pair.AccessToken, "Bearer " + pair.AccessToken} {
		client := mcp.NewClient(&mcp.Implementation{Name: "fixture-test", Version: "1.0.0"}, nil)
		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint:   base + fixtureauth.PathOwnTokenMCP,
			HTTPClient: &http.Client{Transport: headerTransport{name: "Authorization", value: value}},
		}, nil)
		require.NoError(t, err)
		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
		require.NoError(t, err)
		require.False(t, res.IsError, "%v", res.Content)
		got, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "pat@fixture.test", got["identity"])
		_ = session.Close()
	}
}

// The DCR server sits on another host than a localhost fixture, so the
// dashboard does not take it for the main authorization server.
