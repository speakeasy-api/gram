package authn

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"

	"github.com/speakeasy-api/gram/server/internal/channels/authn/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

const testOrigin = "https://channels.example.com"
const testResource = testOrigin + "/channels/test-org/default/mcp"
const testRedirect = "https://client.example.com/callback"
const testVerifier = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"

type testRoutes struct{ mux *http.ServeMux }

func (r testRoutes) Handle(method, path string, handler http.HandlerFunc) {
	r.mux.HandleFunc(method+" "+path, handler)
}

type fakeMetadataResolver struct{ document *cimd.Document }

func (f fakeMetadataResolver) Resolve(context.Context, string, cimd.CacheState) (*cimd.CacheResult, error) {
	return &cimd.CacheResult{Document: f.document, Outcome: cimd.CacheOutcomeRefreshed}, nil
}

type oauthFixture struct {
	db      *pgxpool.Pool
	service *Service
	mux     *http.ServeMux
	signer  *sessiontokens.Signer
	browser func() context.Context
	denied  bool
}

func newOAuthFixture(t *testing.T, db *pgxpool.Pool, configure ...func(*Config)) *oauthFixture {
	t.Helper()
	err := fixturerepo.New(db).SeedOrganizations(t.Context())
	require.NoError(t, err)
	err = fixturerepo.New(db).SeedDefaultProjects(t.Context())
	require.NoError(t, err)
	base, err := url.Parse(testOrigin)
	require.NoError(t, err)
	f := &oauthFixture{db: db, mux: http.NewServeMux(), signer: sessiontokens.NewSigner("channels-oauth-test-signing-key"), browser: func() context.Context {
		return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{UserID: "user_test", ActiveOrganizationID: "org_test"}, false)
	}}
	config := Config{BaseURL: base, DB: db, Signer: f.signer,
		AuthenticateBrowser: func(*http.Request) (context.Context, error) { return f.browser(), nil },
		Admit: func(ctx context.Context, identity Identity) (context.Context, error) {
			if f.denied {
				return nil, errors.New("permission removed")
			}
			return ctx, nil
		},
	}
	for _, apply := range configure {
		apply(&config)
	}
	f.service, err = New(config)
	require.NoError(t, err)
	f.service.Attach(testRoutes{f.mux})
	return f
}

func (f *oauthFixture) request(method, path, body, origin string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, testOrigin+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if strings.HasPrefix(body, "{") {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	f.mux.ServeHTTP(res, req)
	return res
}

func (f *oauthFixture) register(t *testing.T) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"client_name": "Test client", "redirect_uris": []string{testRedirect}, "token_endpoint_auth_method": "none"})
	res := f.request(http.MethodPost, "/channels/oauth/register", string(body), "")
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	var result map[string]any
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &result))
	client, ok := result["client_id"].(string)
	require.True(t, ok)
	return client
}

func authorizationQuery(client string) url.Values {
	digest := sha256.Sum256([]byte(testVerifier))
	return url.Values{"response_type": {"code"}, "client_id": {client}, "redirect_uri": {testRedirect}, "resource": {testResource}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "state": {"opaque-client-state"}, "scope": {"channels:connect"}}
}

func consentFields(t *testing.T, body string) url.Values {
	t.Helper()
	fields := url.Values{}
	tokenizer := html.NewTokenizer(strings.NewReader(body))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		if token.Data != "input" {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		if attrs["type"] == "hidden" {
			fields.Set(attrs["name"], attrs["value"])
		}
	}
	require.NotEmpty(t, fields.Get("csrf"), body)
	require.NotEmpty(t, fields.Get("challenge"), body)
	return fields
}

func (f *oauthFixture) challenge(t *testing.T, client string, resources ...string) (url.Values, *http.Cookie) {
	t.Helper()
	query := authorizationQuery(client)
	if len(resources) == 1 {
		query.Set("resource", resources[0])
	}
	res := f.request(http.MethodGet, "/channels/oauth/authorize?"+query.Encode(), "", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var proof *http.Cookie
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == "__Host-channels-oauth-proof" {
			proof = cookie
		}
	}
	require.NotNil(t, proof)
	require.True(t, proof.Secure)
	require.True(t, proof.HttpOnly)
	require.Equal(t, "/", proof.Path)
	require.Empty(t, proof.Domain)
	fields := consentFields(t, res.Body.String())
	fields.Set("decision", "allow")
	return fields, proof
}

func (f *oauthFixture) authorize(t *testing.T, client string, resources ...string) string {
	t.Helper()
	fields, proof := f.challenge(t, client, resources...)
	res := f.request(http.MethodPost, "/channels/oauth/authorize", fields.Encode(), testOrigin, proof)
	require.Contains(t, []int{http.StatusFound, http.StatusSeeOther}, res.Code, res.Body.String())
	location, err := url.Parse(res.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "opaque-client-state", location.Query().Get("state"))
	require.NotEmpty(t, location.Query().Get("code"))
	return location.Query().Get("code")
}

func (f *oauthFixture) token(fields url.Values) *httptest.ResponseRecorder {
	return f.request(http.MethodPost, "/channels/oauth/token", fields.Encode(), "")
}
func decodeTokens(t *testing.T, res *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &decoded))
	access, ok := decoded["access_token"].(string)
	require.True(t, ok)
	refresh, ok := decoded["refresh_token"].(string)
	require.True(t, ok)
	require.NotEmpty(t, access)
	require.NotEmpty(t, refresh)
	return map[string]string{"access_token": access, "refresh_token": refresh}
}

func TestOAuthSecurity(t *testing.T) {
	t.Parallel()
	container, clone, err := testenv.NewTestPostgres(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	fixture := func(t *testing.T, configure ...func(*Config)) *oauthFixture {
		t.Helper()
		db, err := clone(t, "channels_oauth")
		require.NoError(t, err)
		return newOAuthFixture(t, db, configure...)
	}

	t.Run("consent identifies the signed-in account without exposing its principal ID", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		email := "member@example.test"
		f.browser = func() context.Context {
			return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{
				UserID: "user_test", ActiveOrganizationID: "org_test", Email: &email,
			}, false)
		}
		response := f.request(http.MethodGet, "/channels/oauth/authorize?"+authorizationQuery(client).Encode(), "", "")
		require.Equal(t, http.StatusOK, response.Code)
		require.Contains(t, response.Body.String(), email)
		require.NotContains(t, response.Body.String(), "user_test")
		require.Contains(t, response.Body.String(), testRedirect)

		f.browser = func() context.Context {
			return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{
				UserID: "user_test", ActiveOrganizationID: "org_test",
			}, false)
		}
		response = f.request(http.MethodGet, "/channels/oauth/authorize?"+authorizationQuery(client).Encode(), "", "")
		require.Equal(t, http.StatusOK, response.Code)
		require.NotContains(t, response.Body.String(), "Signed in as")
		require.NotContains(t, response.Body.String(), "user_test")
	})

	t.Run("consent policy permits registered OAuth callback redirects", func(t *testing.T) {
		t.Parallel()
		for _, callback := range []string{"https://client.example.com/callback", "http://127.0.0.1:43210/callback", "com.example.client:/oauth/callback"} {
			t.Run(callback, func(t *testing.T) {
				t.Parallel()
				f := fixture(t)
				body, err := json.Marshal(map[string]any{"client_name": "Test client", "redirect_uris": []string{callback}, "token_endpoint_auth_method": "none"})
				require.NoError(t, err)
				registered := f.request(http.MethodPost, "/channels/oauth/register", string(body), "")
				require.Equal(t, http.StatusCreated, registered.Code)
				var client struct {
					ID string `json:"client_id"`
				}
				require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &client))
				query := authorizationQuery(client.ID)
				query.Set("redirect_uri", callback)
				page := f.request(http.MethodGet, "/channels/oauth/authorize?"+query.Encode(), "", "")
				require.Equal(t, http.StatusOK, page.Code)
				policy := page.Header().Get("Content-Security-Policy")
				require.NotContains(t, policy, "form-action")
				require.Contains(t, policy, "frame-ancestors 'none'")
				require.Contains(t, policy, "default-src 'none'")
				require.Equal(t, "same-origin", page.Header().Get("Referrer-Policy"))
				form := consentFields(t, page.Body.String())
				form.Set("decision", "allow")
				approved := f.request(http.MethodPost, "/channels/oauth/authorize", form.Encode(), testOrigin, page.Result().Cookies()...)
				require.Equal(t, http.StatusSeeOther, approved.Code)
				redirect, err := url.Parse(approved.Header().Get("Location"))
				require.NoError(t, err)
				require.NotEmpty(t, redirect.Query().Get("code"))
				redirect.RawQuery = ""
				require.Equal(t, callback, redirect.String())
			})
		}
	})

	t.Run("same user client grants remain independent across projects", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		issue := func(resource string) map[string]string {
			return decodeTokens(t, f.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {f.authorize(t, client, resource)}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {resource}}))
		}
		first := issue(testResource)
		secondResource := testOrigin + "/channels/test-org/second/mcp"
		second := issue(secondResource)
		secondTarget, err := f.service.target(t.Context(), secondResource)
		require.NoError(t, err)
		_, err = f.service.Authenticate(f.targetContext(t), first["access_token"])
		require.NoError(t, err)
		_, err = f.service.Authenticate(WithTarget(t.Context(), secondTarget), second["access_token"])
		require.NoError(t, err)
		_, err = f.service.Authenticate(WithTarget(t.Context(), secondTarget), first["access_token"])
		require.Error(t, err)
		issue(secondResource)
		_, err = f.service.Authenticate(f.targetContext(t), first["access_token"])
		require.NoError(t, err)
		_, err = f.service.Authenticate(WithTarget(t.Context(), secondTarget), second["access_token"])
		require.Error(t, err)
	})

	t.Run("authorize rejects empty query and fragment resource delimiters", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		for _, suffix := range []string{"?", "#"} {
			query := authorizationQuery(client)
			query.Set("resource", testResource+suffix)
			response := f.request(http.MethodGet, "/channels/oauth/authorize?"+query.Encode(), "", "")
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.JSONEq(t, `{"error":"invalid_target"}`, response.Body.String())
		}
	})

	t.Run("deleted and recreated slug cannot inherit OAuth credentials", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		tokens := decodeTokens(t, f.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {f.authorize(t, client)}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}))
		codeClient := f.register(t)
		code := f.authorize(t, codeClient)
		form, proof := f.challenge(t, f.register(t))
		originalTarget, err := f.service.target(t.Context(), testResource)
		require.NoError(t, err)
		queries := fixturerepo.New(f.db)
		require.NoError(t, queries.SoftDeleteProject(t.Context(), fixturerepo.SoftDeleteProjectParams{OrganizationID: originalTarget.OrganizationID, ProjectID: originalTarget.ProjectID}))
		replacementID, err := queries.RecreateDefaultProject(t.Context(), originalTarget.OrganizationID)
		require.NoError(t, err)
		require.NotEqual(t, originalTarget.ProjectID, replacementID)
		target, err := f.service.target(t.Context(), testResource)
		require.NoError(t, err)
		require.Equal(t, replacementID, target.ProjectID)
		_, err = f.service.Authenticate(WithTarget(t.Context(), target), tokens["access_token"])
		require.Error(t, err)
		response := f.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {tokens["refresh_token"]}, "resource": {testResource}})
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		response = f.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {codeClient}, "code": {code}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}})
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		response = f.request(http.MethodPost, "/channels/oauth/authorize", form.Encode(), testOrigin, proof)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	})

	t.Run("project resources bind consent codes refresh and tokens", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		otherResource := testOrigin + "/channels/other-org/default/mcp"
		otherTarget, err := f.service.target(t.Context(), otherResource)
		require.NoError(t, err)
		// A consent form cannot select a different target after authorization.
		form, proof := f.challenge(t, client)
		form.Set("resource", otherResource)
		response := f.request(http.MethodPost, "/channels/oauth/authorize", form.Encode(), testOrigin, proof)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		form.Set("resource", testResource)
		response = f.request(http.MethodPost, "/channels/oauth/authorize", form.Encode(), testOrigin, proof)
		require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
		callback, err := url.Parse(response.Header().Get("Location"))
		require.NoError(t, err)
		fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {callback.Query().Get("code")}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {otherResource}}
		require.Equal(t, http.StatusBadRequest, f.token(fields).Code)
		fields.Set("resource", testResource)
		tokens := decodeTokens(t, f.token(fields))
		identity, err := f.service.Authenticate(f.targetContext(t), tokens["access_token"])
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, identity.ProjectID)
		_, err = f.service.Authenticate(WithTarget(t.Context(), otherTarget), tokens["access_token"])
		require.Error(t, err)
		refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {tokens["refresh_token"]}, "resource": {otherResource}}
		require.Equal(t, http.StatusBadRequest, f.token(refresh).Code)
		refresh.Set("resource", testResource)
		decodeTokens(t, f.token(refresh))
	})

	t.Run("consent targets another accessible organization not the browser selection", func(t *testing.T) {
		t.Parallel()
		var admitted Identity
		f := fixture(t, func(c *Config) {
			c.Admit = func(ctx context.Context, id Identity) (context.Context, error) { admitted = id; return ctx, nil }
		})
		client := f.register(t)
		resource := testOrigin + "/channels/other-org/default/mcp"
		query := authorizationQuery(client)
		query.Set("resource", resource)
		response := f.request(http.MethodGet, "/channels/oauth/authorize?"+query.Encode(), "", "")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Regexp(t, `<dt[^>]*>Organization</dt>\s*<dd[^>]*>other-org</dd>`, response.Body.String())
		require.Regexp(t, `<dt[^>]*>Project</dt>\s*<dd[^>]*>default</dd>`, response.Body.String())
		require.Equal(t, "org_other", admitted.OrganizationID)
		form := consentFields(t, response.Body.String())
		form.Set("decision", "allow")
		var proof *http.Cookie
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == proofCookie {
				proof = cookie
			}
		}
		response = f.request(http.MethodPost, "/channels/oauth/authorize", form.Encode(), testOrigin, proof)
		require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
		callback, err := url.Parse(response.Header().Get("Location"))
		require.NoError(t, err)
		tokens := decodeTokens(t, f.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {callback.Query().Get("code")}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {resource}}))
		target, err := f.service.target(t.Context(), resource)
		require.NoError(t, err)
		identity, err := f.service.Authenticate(WithTarget(t.Context(), target), tokens["access_token"])
		require.NoError(t, err)
		require.Equal(t, "org_other", identity.OrganizationID)
		require.Equal(t, target.ProjectID, identity.ProjectID)
	})

	t.Run("consent is bound to the browser session", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		browserSession := "session_first"
		f.browser = func() context.Context {
			return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{UserID: "user_test", ActiveOrganizationID: "org_test", SessionID: &browserSession}, false)
		}
		form, proof := f.challenge(t, f.register(t))
		browserSession = "session_switched"
		response := f.request(http.MethodPost, "/channels/oauth/authorize", form.Encode(), testOrigin, proof)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	})

	t.Run("consent is bound to the browser user", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		form, proof := f.challenge(t, f.register(t))
		f.browser = func() context.Context {
			return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{UserID: "user_switched", ActiveOrganizationID: "org_test"}, false)
		}
		response := f.request(http.MethodPost, "/channels/oauth/authorize", form.Encode(), testOrigin, proof)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	})

	t.Run("legacy and malformed resource paths have no metadata aliases", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		for _, path := range []string{"/channels/mcp", "/.well-known/oauth-protected-resource/channels/mcp", "/.well-known/oauth-protected-resource/channels/test-org/default/mcp/", "/.well-known/oauth-protected-resource/channels/test-org/default/mcp/extra", "/.well-known/oauth-protected-resource/channels/test-org/%64efault/mcp"} {
			response := f.request(http.MethodGet, path, "", "")
			require.Equal(t, http.StatusNotFound, response.Code, path)
		}
	})

	t.Run("unauthenticated login returns through the configured dashboard proxy", func(t *testing.T) {
		t.Parallel()
		dashboard, err := url.Parse("https://channels.example.com:5173")
		require.NoError(t, err)
		f := fixture(t, func(config *Config) { config.DashboardURL = dashboard })
		client := f.register(t)
		authenticatedBrowser := f.browser
		f.browser = func() context.Context { return t.Context() }
		originalPath := "/channels/oauth/authorize?" + authorizationQuery(client).Encode()
		unauthenticated := f.request(http.MethodGet, originalPath, "", "")
		require.Equal(t, http.StatusSeeOther, unauthenticated.Code)
		login, err := url.Parse(unauthenticated.Header().Get("Location"))
		require.NoError(t, err)
		require.Equal(t, "/rpc/auth.login", login.Path)
		require.Empty(t, login.Host)
		returnTarget, err := url.Parse(login.Query().Get("redirect"))
		require.NoError(t, err)
		require.False(t, returnTarget.IsAbs())
		require.Empty(t, returnTarget.Host)
		require.Equal(t, originalPath, returnTarget.String())
		// Existing local auth callbacks use the dashboard origin and return a
		// relative Location. Vite proxies this browser URL back to the API.
		callback := dashboard.ResolveReference(&url.URL{Path: "/rpc/auth.callback"})
		returned := callback.ResolveReference(returnTarget)
		require.Equal(t, dashboard.Host, returned.Host)
		f.browser = authenticatedBrowser
		browserRequest := func(method, target, body, origin string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
			request := httptest.NewRequest(method, target, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Origin", origin)
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			f.mux.ServeHTTP(response, request)
			return response
		}
		consent := browserRequest(http.MethodGet, returned.String(), "", "")
		require.Equal(t, http.StatusOK, consent.Code, consent.Body.String())
		require.Contains(t, consent.Body.String(), `action="/channels/oauth/authorize"`)
		fields := consentFields(t, consent.Body.String())
		fields.Set("decision", "allow")
		proof := consent.Result().Cookies()
		action := dashboard.ResolveReference(&url.URL{Path: "/channels/oauth/authorize"}).String()
		for _, origin := range []string{"", "https://unrelated.example.test"} {
			denied := browserRequest(http.MethodPost, action, fields.Encode(), origin, proof...)
			require.Equal(t, http.StatusForbidden, denied.Code)
			require.Empty(t, denied.Header().Get("Location"))
		}
		originalCSRF := fields.Get("csrf")
		fields.Set("csrf", "incorrect")
		denied := browserRequest(http.MethodPost, action, fields.Encode(), dashboard.String(), proof...)
		require.Equal(t, http.StatusForbidden, denied.Code)
		fields.Set("csrf", originalCSRF)
		approved := browserRequest(http.MethodPost, action, fields.Encode(), dashboard.String(), proof...)
		require.Equal(t, http.StatusSeeOther, approved.Code, approved.Body.String())
		location, err := url.Parse(approved.Header().Get("Location"))
		require.NoError(t, err)
		require.Equal(t, "opaque-client-state", location.Query().Get("state"))
		require.NotEmpty(t, location.Query().Get("code"))
		tokens := decodeTokens(t, f.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {location.Query().Get("code")}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}))
		claims, err := f.signer.ValidateExactAudience(tokens["access_token"], testResource)
		require.NoError(t, err)
		require.Equal(t, testOrigin+"/channels/oauth", claims.Issuer)
		identity, err := f.service.Authenticate(f.targetContext(t), tokens["access_token"])
		require.NoError(t, err)
		require.Equal(t, "org_test", identity.OrganizationID)
		// Configuring a browser proxy must not remove canonical API consent.
		require.NotEmpty(t, f.authorize(t, client))
	})

	t.Run("dashboard origin configuration is explicit and validated", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		fields, proof := f.challenge(t, client)
		denied := f.request(http.MethodPost, "/channels/oauth/authorize", fields.Encode(), "https://channels.example.com:5173", proof)
		require.Equal(t, http.StatusForbidden, denied.Code, "an unconfigured dashboard origin is not trusted")
		for _, raw := range []string{"https://app.example.test/path", "https://app.example.test?query=1", "https://app.example.test#fragment", "https://user@app.example.test", "http://app.example.test", "//app.example.test"} {
			dashboard, err := url.Parse(raw)
			require.NoError(t, err)
			config := f.service.config
			config.DashboardURL = dashboard
			_, err = New(config)
			require.Error(t, err, raw)
		}
	})

	t.Run("lifecycle rotation reuse and admission", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)

		client := f.register(t)
		// This intentionally shares the signer, user, and client with Channels,
		// but belongs to a different resource and issuer.
		otherToken, _, err := f.signer.Mint(sessiontokens.MintParams{Subject: urn.NewUserSubject("user_test"), Issuer: testOrigin + "/other/oauth", Audience: testOrigin + "/mcp", ClientID: client, Lifetime: time.Hour})
		require.NoError(t, err)
		checkOtherToken := func() {
			claims, err := f.signer.ValidateExactAudience(otherToken, testOrigin+"/mcp")
			require.NoError(t, err)
			require.Equal(t, client, claims.ClientID)
			_, err = f.service.Authenticate(f.targetContext(t), otherToken)
			require.Error(t, err)
		}
		checkOtherToken()
		code := f.authorize(t, client)
		fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {code}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}
		tokens := decodeTokens(t, f.token(fields))
		identity, err := f.service.Authenticate(f.targetContext(t), tokens["access_token"])
		require.NoError(t, err)
		require.Equal(t, "org_test", identity.OrganizationID)
		require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeUser, "user_test"), identity.Principal)

		for _, mutation := range []string{"issuer", "audience", "apikey", "agent"} {
			claims := &sessiontokens.SessionClaims{}
			_, _, err := jwt.NewParser().ParseUnverified(tokens["access_token"], claims)
			require.NoError(t, err)
			switch mutation {
			case "issuer":
				claims.Issuer = "https://other.example.com"
			case "audience":
				claims.Audience = jwt.ClaimStrings{"https://other.example.com/mcp"}
			case "apikey":
				claims.Subject = urn.NewAPIKeySubject(uuid.New()).String()
			case "agent":
				claims.Subject = urn.NewAgentSubject(uuid.New()).String()
			}
			forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("channels-oauth-test-signing-key"))
			require.NoError(t, err)
			_, err = f.service.Authenticate(f.targetContext(t), forged)
			require.Error(t, err, mutation)
		}
		f.denied = true
		_, err = f.service.Authenticate(f.targetContext(t), tokens["access_token"])
		require.Error(t, err)
		f.denied = false
		refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {tokens["refresh_token"]}, "resource": {testResource}}
		rotated := decodeTokens(t, f.token(refresh))
		require.NotEqual(t, tokens["refresh_token"], rotated["refresh_token"])
		require.Equal(t, http.StatusBadRequest, f.token(refresh).Code)
		_, err = f.service.Authenticate(f.targetContext(t), rotated["access_token"])
		require.Error(t, err, "refresh reuse must revoke the token family")
		refresh.Set("refresh_token", rotated["refresh_token"])
		require.Equal(t, http.StatusBadRequest, f.token(refresh).Code)
		checkOtherToken()
	})

	t.Run("consent requires origin browser proof and csrf", func(t *testing.T) {
		t.Parallel()
		for _, scenario := range []string{"missing origin", "foreign origin", "missing proof", "wrong proof", "wrong csrf", "wrong challenge"} {
			t.Run(scenario, func(t *testing.T) {
				t.Parallel()
				f := fixture(t)
				client := f.register(t)
				fields, proof := f.challenge(t, client)
				origin := testOrigin
				switch scenario {
				case "missing origin":
					origin = ""
				case "foreign origin":
					origin = "https://attacker.example.com"
				case "missing proof":
					proof = nil
				case "wrong proof":
					proof.Value = "attacker"
				case "wrong csrf":
					fields.Set("csrf", "attacker")
				case "wrong challenge":
					fields.Set("challenge", "attacker")
				}
				var cookies []*http.Cookie
				if proof != nil {
					cookies = append(cookies, proof)
				}
				res := f.request(http.MethodPost, "/channels/oauth/authorize", fields.Encode(), origin, cookies...)
				require.GreaterOrEqual(t, res.Code, 400, res.Body.String())
				require.Empty(t, res.Header().Get("Location"))
			})
		}
	})

	t.Run("authorization rejects unsafe parameters", func(t *testing.T) {
		t.Parallel()
		for key, value := range map[string]string{"resource": testOrigin + "/mcp", "redirect_uri": "https://attacker.example.com/callback", "code_challenge_method": "plain", "scope": "admin", "response_type": "token"} {
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				f := fixture(t)
				client := f.register(t)
				query := authorizationQuery(client)
				query.Set(key, value)
				res := f.request(http.MethodGet, "/channels/oauth/authorize?"+query.Encode(), "", "")
				require.GreaterOrEqual(t, res.Code, 400, res.Body.String())
			})
		}
	})

	t.Run("code binding and one time redemption", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		for _, scenario := range []string{"wrong verifier", "missing resource", "wrong resource", "wrong redirect", "wrong client"} {
			t.Run(scenario, func(t *testing.T) {
				t.Parallel()
				f := fixture(t)
				client := f.register(t)
				code := f.authorize(t, client)
				fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {code}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}
				switch scenario {
				case "wrong verifier":
					fields.Set("code_verifier", strings.Repeat("x", 43))
				case "missing resource":
					fields.Del("resource")
				case "wrong resource":
					fields.Set("resource", testOrigin+"/mcp")
				case "wrong redirect":
					fields.Set("redirect_uri", testRedirect+"/other")
				case "wrong client":
					fields.Set("client_id", f.register(t))
				}
				require.GreaterOrEqual(t, f.token(fields).Code, 400)
			})
		}
		fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {f.authorize(t, client)}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}
		decodeTokens(t, f.token(fields))
		require.Equal(t, http.StatusBadRequest, f.token(fields).Code)
	})

	t.Run("duplicate resource indicators require exact matches", func(t *testing.T) {
		t.Parallel()
		for _, scenario := range []string{"matching resources", "mismatched resources", "duplicate client id"} {
			t.Run(scenario, func(t *testing.T) {
				t.Parallel()
				f := fixture(t)
				client := f.register(t)
				query := authorizationQuery(client)
				switch scenario {
				case "matching resources":
					query.Add("resource", testResource)
				case "mismatched resources":
					query.Add("resource", testOrigin+"/mcp")
				case "duplicate client id":
					query.Add("client_id", client)
				}
				res := f.request(http.MethodGet, "/channels/oauth/authorize?"+query.Encode(), "", "")
				var code string
				if scenario == "matching resources" {
					require.Equal(t, http.StatusOK, res.Code, res.Body.String())
					fields := consentFields(t, res.Body.String())
					fields.Set("decision", "allow")
					consent := f.request(http.MethodPost, "/channels/oauth/authorize", fields.Encode(), testOrigin, res.Result().Cookies()...)
					require.Contains(t, []int{http.StatusFound, http.StatusSeeOther}, consent.Code, consent.Body.String())
					location, err := url.Parse(consent.Header().Get("Location"))
					require.NoError(t, err)
					code = location.Query().Get("code")
					require.NotEmpty(t, code)
				} else {
					require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
					code = f.authorize(t, client)
				}
				fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {code}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}
				switch scenario {
				case "matching resources":
					fields.Add("resource", testResource)
				case "mismatched resources":
					fields.Add("resource", testOrigin+"/mcp")
				case "duplicate client id":
					fields.Add("client_id", client)
				}
				res = f.token(fields)
				if scenario == "matching resources" {
					tokens := decodeTokens(t, res)
					_, err := f.service.Authenticate(f.targetContext(t), tokens["access_token"])
					require.NoError(t, err)
				} else {
					require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
				}
			})
		}
	})

	t.Run("revocation invalidates access and refresh", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {f.authorize(t, client)}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}
		tokens := decodeTokens(t, f.token(fields))
		revoke := url.Values{"resource": {testResource}, "client_id": {client}, "token": {tokens["refresh_token"]}, "token_type_hint": {"refresh_token"}}
		res := f.request(http.MethodPost, "/channels/oauth/revoke", revoke.Encode(), "")
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		_, err := f.service.Authenticate(f.targetContext(t), tokens["access_token"])
		require.Error(t, err)
		refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {tokens["refresh_token"]}, "resource": {testResource}}
		require.Equal(t, http.StatusBadRequest, f.token(refresh).Code)
	})

	t.Run("confidential clients require basic authentication", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		body, _ := json.Marshal(map[string]any{"client_name": "Confidential test client", "redirect_uris": []string{testRedirect}, "token_endpoint_auth_method": "client_secret_basic"})
		res := f.request(http.MethodPost, "/channels/oauth/register", string(body), "")
		require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
		var registration map[string]any
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &registration))
		client, ok := registration["client_id"].(string)
		require.True(t, ok)
		secret, ok := registration["client_secret"].(string)
		require.True(t, ok)
		require.NotEmpty(t, secret)
		fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {f.authorize(t, client)}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}
		require.GreaterOrEqual(t, f.token(fields).Code, 400)
		sendBasic := func(secret string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodPost, testOrigin+"/channels/oauth/token", strings.NewReader(fields.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetBasicAuth(client, secret)
			res := httptest.NewRecorder()
			f.mux.ServeHTTP(res, req)
			return res
		}
		require.GreaterOrEqual(t, sendBasic("wrong-secret").Code, 400)
		fields.Set("code", f.authorize(t, client))
		tokens := decodeTokens(t, sendBasic(secret))
		_, err := f.service.Authenticate(f.targetContext(t), tokens["access_token"])
		require.NoError(t, err)
	})

	t.Run("browser credentials require validated user session", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		for _, ctx := range []context.Context{t.Context(), contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{UserID: "user_test", ActiveOrganizationID: "org_test"}), contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{UserID: "user_test"}, false), contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{UserID: "user_test", ActiveOrganizationID: "org_test"}, true)} {
			f.browser = func() context.Context { return ctx }
			res := f.request(http.MethodGet, "/channels/oauth/authorize?"+authorizationQuery(client).Encode(), "", "")
			if res.Code == http.StatusOK {
				fields := consentFields(t, res.Body.String())
				res = f.request(http.MethodPost, "/channels/oauth/authorize", fields.Encode(), testOrigin, res.Result().Cookies()...)
			}
			require.Contains(t, []int{http.StatusFound, http.StatusSeeOther}, res.Code, res.Body.String())
			login, err := url.Parse(res.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, "/rpc/auth.login", login.Path)
		}
	})

	t.Run("consent is bound to user and organization", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		original := f.browser
		for _, identity := range []struct{ user, org string }{{"user_other", "org_test"}, {"user_test", "org_other"}} {
			f.browser = original
			fields, proof := f.challenge(t, client)
			f.browser = func() context.Context {
				return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{UserID: identity.user, ActiveOrganizationID: identity.org}, false)
			}
			res := f.request(http.MethodPost, "/channels/oauth/authorize", fields.Encode(), testOrigin, proof)
			require.GreaterOrEqual(t, res.Code, 400, res.Body.String())
			require.Empty(t, res.Header().Get("Location"))
		}
	})

	t.Run("reauthorization revokes previous family", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		exchange := func() map[string]string {
			return decodeTokens(t, f.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {f.authorize(t, client)}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}))
		}
		previous := exchange()
		current := exchange()
		_, err := f.service.Authenticate(f.targetContext(t), previous["access_token"])
		require.Error(t, err)
		_, err = f.service.Authenticate(f.targetContext(t), current["access_token"])
		require.NoError(t, err)
		refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {previous["refresh_token"]}, "resource": {testResource}}
		require.Equal(t, http.StatusBadRequest, f.token(refresh).Code)
		_, err = f.service.Authenticate(f.targetContext(t), current["access_token"])
		require.NoError(t, err, "old-family use must not revoke the new family")
	})

	t.Run("cleanup preserves reuse evidence until family expiry", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		tokens := decodeTokens(t, f.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {f.authorize(t, client)}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}))
		refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {tokens["refresh_token"]}, "resource": {testResource}}
		rotated := decodeTokens(t, f.token(refresh))
		f.challenge(t, client)
		err := fixturerepo.New(f.db).ExpireCodes(t.Context())
		require.NoError(t, err)
		err = fixturerepo.New(f.db).ExpireChallenges(t.Context())
		require.NoError(t, err)
		err = fixturerepo.New(f.db).ExpireAccessTokens(t.Context())
		require.NoError(t, err)
		err = fixturerepo.New(f.db).AgeClients(t.Context())
		require.NoError(t, err)
		cleanup := func() {
			require.NoError(t, f.service.Cleanup(t.Context()))
		}
		count := func(table string) int {
			var n int
			result20, queryErr20 := fixturerepo.New(f.db).CountRows(t.Context(), table)
			require.NoError(t, queryErr20)
			n = int(result20)
			return n
		}
		cleanup()
		require.Equal(t, 0, count("channels_oauth_challenges"))
		require.Equal(t, 0, count("channels_oauth_access_tokens"))
		require.Equal(t, 1, count("channels_oauth_clients"))
		require.Equal(t, 1, count("channels_oauth_grants"))
		var expiredCodes int
		result21, queryErr21 := fixturerepo.New(f.db).CountCodes(t.Context())
		require.NoError(t, queryErr21)
		expiredCodes = int(result21)
		require.Zero(t, expiredCodes)
		var used bool
		result22, queryErr22 := fixturerepo.New(f.db).CredentialUsed(t.Context(), digest(tokens["refresh_token"]))
		require.NoError(t, queryErr22)
		used = result22
		require.True(t, used)
		require.Equal(t, http.StatusBadRequest, f.token(refresh).Code)
		refresh.Set("refresh_token", rotated["refresh_token"])
		require.Equal(t, http.StatusBadRequest, f.token(refresh).Code)
		err = fixturerepo.New(f.db).ExpireGrants(t.Context())
		require.NoError(t, err)
		err = fixturerepo.New(f.db).ExpireCredentials(t.Context())
		require.NoError(t, err)
		err = fixturerepo.New(f.db).ExpireAccessTokens(t.Context())
		require.NoError(t, err)
		cleanup()
		require.Equal(t, 0, count("channels_oauth_grants"))
		require.Equal(t, 0, count("channels_oauth_credentials"))
		require.Equal(t, 0, count("channels_oauth_clients"))
	})

	t.Run("cleanup bounds credential and access fanout before deleting grants", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		f.authorize(t, client)
		err := fixturerepo.New(f.db).SeedExpiredRefreshBatch(t.Context())
		require.NoError(t, err)
		err = fixturerepo.New(f.db).SeedExpiredAccessBatch(t.Context())
		require.NoError(t, err)
		err = fixturerepo.New(f.db).ExpireGrants(t.Context())
		require.NoError(t, err)
		err = fixturerepo.New(f.db).ExpireCredentials(t.Context())
		require.NoError(t, err)
		count := func(table string) int {
			var n int
			result23, queryErr23 := fixturerepo.New(f.db).CountRows(t.Context(), table)
			require.NoError(t, queryErr23)
			n = int(result23)
			return n
		}
		require.NoError(t, f.service.Cleanup(t.Context()))
		require.Equal(t, 6, count("channels_oauth_credentials"))
		require.Equal(t, 5, count("channels_oauth_access_tokens"))
		require.Equal(t, 1, count("channels_oauth_grants"), "grant must not cascade deletion past the batch limit")
		require.NoError(t, f.service.Cleanup(t.Context()))
		require.Zero(t, count("channels_oauth_credentials"))
		require.Zero(t, count("channels_oauth_access_tokens"))
		require.Zero(t, count("channels_oauth_grants"))
	})

	t.Run("cleanup challenge sweep is bounded", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		err := fixturerepo.New(f.db).SeedExpiredChallengeBatch(t.Context(), fixturerepo.SeedExpiredChallengeBatchParams{ClientID: pgtype.Text{String: client, Valid: true}, RedirectUri: testRedirect})
		require.NoError(t, err)
		require.NoError(t, f.service.Cleanup(t.Context()))
		var remaining int
		result24, queryErr24 := fixturerepo.New(f.db).CountChallenges(t.Context())
		require.NoError(t, queryErr24)
		remaining = int(result24)
		require.Equal(t, 5, remaining)
		require.NoError(t, f.service.Cleanup(t.Context()))
		result25, queryErr25 := fixturerepo.New(f.db).CountChallenges(t.Context())
		require.NoError(t, queryErr25)
		remaining = int(result25)
		require.Zero(t, remaining)
	})

	t.Run("cleanup deletes unused nonrevoked DCR and metadata clients", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		unused := f.register(t)
		revoked := f.register(t)
		active := f.register(t)
		f.challenge(t, active)
		err := fixturerepo.New(f.db).RevokeClient(t.Context(), revoked)
		require.NoError(t, err)
		err = fixturerepo.New(f.db).SeedMetadataClient(t.Context(), []string{testRedirect})
		require.NoError(t, err)
		err = fixturerepo.New(f.db).AgeClients(t.Context())
		require.NoError(t, err)
		require.NoError(t, f.service.Cleanup(t.Context()))
		var exists bool
		for _, client := range []string{unused, "https://client.example.com/preserved.json"} {
			exists, err = fixturerepo.New(f.db).ClientExists(t.Context(), client)
			require.NoError(t, err)
			require.False(t, exists, client)
		}
		for _, client := range []string{revoked, active} {
			result27, queryErr27 := fixturerepo.New(f.db).ClientExists(t.Context(), client)
			require.NoError(t, queryErr27)
			exists = result27
			require.True(t, exists, client)
		}
	})

	t.Run("metadata public clients own registry and revocation tombstones", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := "https://client.example.com/oauth-metadata.json"
		document := &cimd.Document{ClientID: client, ClientName: "Metadata client", RedirectURIs: []string{testRedirect}, TokenEndpointAuthMethod: "none"}
		f.service.clients = fakeMetadataResolver{document: document}
		code := f.authorize(t, client)
		var stored string
		result28, queryErr28 := fixturerepo.New(f.db).ClientAuthMethod(t.Context(), client)
		require.NoError(t, queryErr28)
		stored = result28
		require.Equal(t, "none", stored)
		tokens := decodeTokens(t, f.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {code}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}))
		_, err := f.service.Authenticate(f.targetContext(t), tokens["access_token"])
		require.NoError(t, err)
		err = fixturerepo.New(f.db).DeleteClientGrants(t.Context(), client)
		require.NoError(t, err)
		err = fixturerepo.New(f.db).AgeRevokedClient(t.Context(), client)
		require.NoError(t, err)
		require.NoError(t, f.service.Cleanup(t.Context()))
		var revoked bool
		result29, queryErr29 := fixturerepo.New(f.db).ClientRevoked(t.Context(), client)
		require.NoError(t, queryErr29)
		revoked = result29
		require.True(t, revoked)
		res := f.request(http.MethodGet, "/channels/oauth/authorize?"+authorizationQuery(client).Encode(), "", "")
		require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
	})

	t.Run("metadata confidential authentication cannot downgrade", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := "https://client.example.com/confidential.json"
		f.service.clients = fakeMetadataResolver{document: &cimd.Document{ClientID: client, ClientName: "Confidential metadata client", RedirectURIs: []string{testRedirect}, TokenEndpointAuthMethod: "private_key_jwt"}}
		res := f.request(http.MethodGet, "/channels/oauth/authorize?"+authorizationQuery(client).Encode(), "", "")
		require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
		var count int
		result30, queryErr30 := fixturerepo.New(f.db).CountClient(t.Context(), client)
		require.NoError(t, queryErr30)
		count = int(result30)
		require.Zero(t, count)
	})

	t.Run("public endpoints accept foreign origins", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		origin := "https://foreign.example.com"
		for _, path := range []string{"/.well-known/oauth-protected-resource/channels/test-org/default/mcp", "/.well-known/oauth-authorization-server/channels/oauth"} {
			res := f.request(http.MethodGet, path, "", origin)
			require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		}
		body, _ := json.Marshal(map[string]any{"client_name": "Foreign origin client", "redirect_uris": []string{testRedirect}, "token_endpoint_auth_method": "none"})
		res := f.request(http.MethodPost, "/channels/oauth/register", string(body), origin)
		require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
		var registration map[string]any
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &registration))
		client, ok := registration["client_id"].(string)
		require.True(t, ok)
		fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {f.authorize(t, client)}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}
		decodeTokens(t, f.request(http.MethodPost, "/channels/oauth/token", fields.Encode(), origin))
	})

	t.Run("consent requires explicit allow decision", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		fields, proof := f.challenge(t, client)
		fields.Del("decision")
		res := f.request(http.MethodPost, "/channels/oauth/authorize", fields.Encode(), testOrigin, proof)
		require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
		require.Empty(t, res.Header().Get("Location"))
	})

	t.Run("spent code wrong verifier preserves family correct replay revokes", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {f.authorize(t, client)}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}, "resource": {testResource}}
		tokens := decodeTokens(t, f.token(fields))
		fields.Set("code_verifier", strings.Repeat("x", 43))
		require.Equal(t, http.StatusBadRequest, f.token(fields).Code)
		_, err := f.service.Authenticate(f.targetContext(t), tokens["access_token"])
		require.NoError(t, err)
		fields.Set("code_verifier", testVerifier)
		require.Equal(t, http.StatusBadRequest, f.token(fields).Code)
		_, err = f.service.Authenticate(f.targetContext(t), tokens["access_token"])
		require.Error(t, err)
	})

	t.Run("valid browser denied admission does not login redirect", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := f.register(t)
		f.denied = true
		res := f.request(http.MethodGet, "/channels/oauth/authorize?"+authorizationQuery(client).Encode(), "", "")
		require.Equal(t, http.StatusForbidden, res.Code, res.Body.String())
		require.Contains(t, res.Body.String(), "access_denied")
		require.Empty(t, res.Header().Get("Location"))
	})

	t.Run("signed tokens require channels issuer audience user and stored grant", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		for _, params := range []sessiontokens.MintParams{
			{Subject: urn.NewUserSubject("user_test"), Issuer: "https://other.example.com", Audience: testResource},
			{Subject: urn.NewUserSubject("user_test"), Issuer: testOrigin + "/channels/oauth", Audience: "https://other.example.com/mcp"},
			{Subject: urn.NewAPIKeySubject(uuid.New()), Issuer: testOrigin + "/channels/oauth", Audience: testResource},
			{Subject: urn.NewAgentSubject(uuid.New()), Issuer: testOrigin + "/channels/oauth", Audience: testResource},
			{Subject: urn.NewUserSubject("user_test"), Issuer: testOrigin + "/channels/oauth", Audience: testResource},
		} {
			params.Lifetime = time.Hour
			token, _, err := f.signer.Mint(params)
			require.NoError(t, err)
			_, err = f.service.Authenticate(f.targetContext(t), token)
			require.Error(t, err)
		}
	})
}

func (f *oauthFixture) targetContext(t *testing.T) context.Context {
	t.Helper()
	target, err := f.service.target(t.Context(), testResource)
	require.NoError(t, err)
	return WithTarget(t.Context(), target)
}
