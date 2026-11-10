package mcpfixture_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	mcpfixture "github.com/speakeasy-api/gram/mcp-fixture"
	"github.com/speakeasy-api/gram/mcp-fixture/fixtureauth"
)

func startFixture(t *testing.T, ttl time.Duration) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	base := "http://" + srv.Listener.Addr().String()
	srv.Config.Handler = mcpfixture.New(mcpfixture.Config{BaseURL: base, AccessTokenTTL: ttl, SuggestedEmails: []string{"alex@fixture.test"}}).Handler()
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, base
}

func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var v T
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&v))
	return v
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
	prm := decode[map[string]any](t, get(t, base+"/.well-known/oauth-protected-resource/mcp/oauth"))
	require.Equal(t, []any{base}, prm["authorization_servers"])
	asm := decode[map[string]any](t, get(t, base+"/.well-known/oauth-authorization-server"))
	require.Equal(t, base, asm["issuer"])
	require.Equal(t, true, asm["client_id_metadata_document_supported"])

	// Dynamic client registration.
	reg, err := http.Post(base+"/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["`+redirect+`"],"client_name":"Test client","token_endpoint_auth_method":"none"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, reg.StatusCode)
	clientID := decode[map[string]any](t, reg)["client_id"].(string)

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
	bad := postForm(t, base+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {"wrong"}})
	require.Equal(t, "invalid_grant", decode[tokenResponse](t, bad).Error)
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

	tok := decode[tokenResponse](t, postForm(t, base+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {verifier}}))
	require.Empty(t, tok.Error)
	require.NotEmpty(t, tok.AccessToken)
	require.NotEmpty(t, tok.RefreshToken)

	again := decode[tokenResponse](t, postForm(t, base+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {verifier}}))
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
	return http.DefaultTransport.RoundTrip(r)
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
	fresh := decode[tokenResponse](t, postForm(t, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}}))
	require.Empty(t, fresh.Error)
	require.NotEqual(t, tok.RefreshToken, fresh.RefreshToken)
	reused := decode[tokenResponse](t, postForm(t, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}}))
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
	fresh := decode[tokenResponse](t, postForm(t, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {pair.RefreshToken}}))
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
	body := decode[map[string]any](t, get(t, base+fixtureauth.PathAPI+"/customers/get?id=cus_42"))
	require.Equal(t, "cus_42", body["id"])
	health := decode[map[string]any](t, get(t, base+fixtureauth.PathAPI+"/health/check"))
	require.Equal(t, "ok", health["status"])
}

func TestTokensAreBoundToTheirResource(t *testing.T) {
	t.Parallel()
	_, base := startFixture(t, time.Hour)

	auto, err := fixtureauth.IssueTokens(base, fixtureauth.Resource(base, fixtureauth.PathAutoMCP), "client", "sam@fixture.test", nil, time.Hour, time.Now())
	require.NoError(t, err)
	who := callTool(t, base+fixtureauth.PathAutoMCP, auto.AccessToken, "whoami", nil)
	require.Equal(t, "sam@fixture.test", who["identity"])

	// The same token is refused at the other OAuth endpoint.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+fixtureauth.PathOAuthMCP, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+auto.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// A token for another server's resource (External OAuth) works nowhere here.
	foreign, err := fixtureauth.IssueTokens(base, "https://speakeasy.test/mcp/external", "client", "sam@fixture.test", nil, time.Hour, time.Now())
	require.NoError(t, err)
	for _, path := range []string{fixtureauth.PathOAuthMCP, fixtureauth.PathAutoMCP, fixtureauth.PathSharedMCP} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+path, strings.NewReader(`{}`))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+foreign.AccessToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, path)
	}

	meta := decode[map[string]any](t, get(t, base+"/.well-known/oauth-protected-resource"+fixtureauth.PathAutoMCP))
	require.Equal(t, fixtureauth.Resource(base, fixtureauth.PathAutoMCP), meta["resource"])
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
		require.NoError(t, r.ParseForm())
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
	srv.Config.Handler = mcpfixture.New(mcpfixture.Config{BaseURL: base, AccessTokenTTL: time.Hour, DevIDPURL: devIDPURL}).Handler()
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
	meta := decode[map[string]any](t, get(t, base+"/.well-known/oauth-protected-resource"+fixtureauth.PathEMAMCP))
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
	return http.DefaultTransport.RoundTrip(r)
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
}
