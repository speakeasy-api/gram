package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/signins"
)

// firstPartySignInFixture is a private toolset whose dashboard connects sign in
// through a mock identity provider.
type firstPartySignInFixture struct {
	ti   *testInstance
	mock *mockIdentityResolver
	slug string
}

func newFirstPartySignInFixture(t *testing.T) (context.Context, firstPartySignInFixture) {
	t.Helper()
	mock := &mockIdentityResolver{
		exchangeResult:     &identity.IDPUserInfo{Sub: "workos-user-first-party", Email: "first-party@example.com", Name: "First Party", WorkOSSessionID: "session_" + uuid.NewString(), AuthenticationMethod: signins.MethodSSO},
		upsertResult:       "user-" + uuid.NewString()[:8],
		hasAccessOK:        true,
		buildAuthURLResult: &url.URL{Scheme: "https", Host: "idp.example.com", Path: "/authorize"},
	}
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mock.exchangeResult.OrganizationID = workOSOrganizationID(t, ctx, ti, toolset.OrganizationID)
	return ctx, firstPartySignInFixture{ti: ti, mock: mock, slug: toolset.McpSlug.String}
}

func (f firstPartySignInFixture) connect(t *testing.T, ctx context.Context) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", f.slug)
	w := httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleFirstPartyConnect(w, httptest.NewRequestWithContext(context.WithValue(ctx, chi.RouteCtxKey, route), http.MethodGet, "/mcp/"+f.slug+"/connect/first-party", nil)))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	return w, cookies[0]
}

func (f firstPartySignInFixture) callback(ctx context.Context, query url.Values, cookie *http.Cookie) (*httptest.ResponseRecorder, error) {
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, f.ti.serverURL.String()+"/mcp/idp_callback?"+query.Encode(), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	if err := f.ti.service.HandleIDPCallback(w, req); err != nil {
		return w, fmt.Errorf("sign-in callback: %w", err)
	}
	return w, nil
}

// renderedBrowserError is the page a browser sees for err.
func renderedBrowserError(t *testing.T, f firstPartySignInFixture, err error) string {
	t.Helper()
	w := httptest.NewRecorder()
	oops.BrowserErrHandle(f.ti.logger, func(http.ResponseWriter, *http.Request) error { return err }).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	return w.Body.String()
}

// On one host, the IdP callback needs the browser's cookie before it spends
// the state: another browser delivering it is refused, and the owner then
// reaches the consent page.
func TestFirstPartySignIn_CallbackNeedsTheStartingBrowser(t *testing.T) {
	t.Parallel()
	ctx, f := newFirstPartySignInFixture(t)
	_, cookie := f.connect(t, ctx)
	query := url.Values{"state": {f.mock.buildAuthURLParams.State}, "code": {"idp-auth-code"}}

	_, err := f.callback(ctx, query, nil)
	require.ErrorContains(t, err, "sign-in browser binding is invalid")
	require.Contains(t, renderedBrowserError(t, f, err), "current browser or sign-in session")
	_, err = f.callback(ctx, query, &http.Cookie{Name: cookie.Name, Value: "another-browser"})
	require.ErrorContains(t, err, "sign-in browser binding is invalid")

	w, err := f.callback(ctx, query, cookie)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, w.Code)
	landing, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/mcp/"+f.slug+"/connect", landing.Path)
	consentGetPageWithContext(t, ctx, f.ti, f.slug, landing.Query().Get("state"), cookie)
}

// Time spent signing in past the binding's limit reads as expired, not as
// another browser.
func TestFirstPartySignIn_SlowSignInExpires(t *testing.T) {
	t.Parallel()
	ctx, f := newFirstPartySignInFixture(t)
	_, cookie := f.connect(t, ctx)
	stateID := f.mock.buildAuthURLParams.State
	state, err := f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.Browser.BoundAt = time.Now().Add(-state.TTL() - time.Minute)
	require.NoError(t, f.ti.authnChallengeCache.Store(ctx, state))

	_, err = f.callback(ctx, url.Values{"state": {stateID}, "code": {"idp-auth-code"}}, cookie)
	require.ErrorContains(t, err, "sign-in took too long")
	page := renderedBrowserError(t, f, err)
	require.Contains(t, page, "link expired or was already used")
	require.NotContains(t, page, "current browser or sign-in session")
}

// A revoked sign-in restarts with the person's browser present: the binding
// starts over with a fresh cookie, and the restarted sign-in completes.
func TestFirstPartySignIn_RevokedRestartRenewsTheBinding(t *testing.T) {
	t.Parallel()
	ctx, f := newFirstPartySignInFixture(t)
	_, cookie := f.connect(t, ctx)
	stateID := f.mock.buildAuthURLParams.State
	state, err := f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.Browser.BoundAt = time.Now().Add(-9 * time.Minute)
	require.NoError(t, f.ti.authnChallengeCache.Store(ctx, state))
	orgID := mustEndpointOrganization(t, ctx, f)
	signins.EnableForTests(f.ti.features, orgID)
	_, err = signins.RevokeSession(ctx, f.ti.conn, []string{orgID}, f.mock.upsertResult, f.mock.exchangeResult.WorkOSSessionID)
	require.NoError(t, err)

	restarted, err := f.callback(ctx, url.Values{"state": {stateID}, "code": {"idp-auth-code"}}, cookie)
	require.NoError(t, err)
	require.Equal(t, "https://idp.example.com/authorize", restarted.Header().Get("Location"))
	require.Equal(t, "login", f.mock.buildAuthURLParams.Prompt)
	renewed := restarted.Result().Cookies()
	require.Len(t, renewed, 1)
	require.Equal(t, int((10 * time.Minute).Seconds()), renewed[0].MaxAge, "the restart does not inherit the time already spent")
	retried, err := f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+f.mock.buildAuthURLParams.State)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), retried.Browser.BoundAt, time.Minute)

	f.mock.exchangeResult.WorkOSSessionID = "session_" + uuid.NewString()
	_, err = f.callback(ctx, url.Values{"state": {retried.ID}, "code": {"idp-auth-code-2"}}, cookie)
	require.ErrorContains(t, err, "sign-in browser binding is invalid", "the old cookie no longer counts")
	w, err := f.callback(ctx, url.Values{"state": {retried.ID}, "code": {"idp-auth-code-2"}}, renewed[0])
	require.NoError(t, err)
	landing, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/mcp/"+f.slug+"/connect", landing.Path)
	consentGetPageWithContext(t, ctx, f.ti, f.slug, landing.Query().Get("state"), renewed[0])
}

func mustEndpointOrganization(t *testing.T, ctx context.Context, f firstPartySignInFixture) string {
	t.Helper()
	endpoint, err := f.ti.service.LoadResolvedMcpEndpointBySlug(ctx, f.ti.logger, f.slug, "mcp")
	require.NoError(t, err)
	return endpoint.OrganizationID
}

// With the consent page on the authentication host, the browser proves both
// hosts' cookies before it signs in. Another browser cannot deliver the IdP
// callback, so no other browser ends up holding the callback host's cookie.
func TestFirstPartySignIn_TwoHostsHandOffTheBrowser(t *testing.T) {
	t.Parallel()
	ctx, f := newFirstPartySignInFixture(t)
	endpoint, err := f.ti.service.LoadResolvedMcpEndpointBySlug(ctx, f.ti.logger, f.slug, "mcp")
	require.NoError(t, err)
	useAuthenticationHost(t, ctx, f.ti, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	harness := newAuthenticationHostHarness(t, f.ti)

	start := harness.serve(t, http.MethodGet, "auth.example.com", "/mcp/"+f.slug+"/connect/first-party", nil)
	require.Equal(t, http.StatusFound, start.Code, start.Body.String())
	originCookies := start.Result().Cookies()
	require.Len(t, originCookies, 1)
	bootstrap, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, f.ti.serverURL.String()+"/mcp/idp_callback", bootstrap.Scheme+"://"+bootstrap.Host+bootstrap.Path, "sign-in waits for the handoff")

	handoff, err := f.callback(ctx, bootstrap.Query(), nil)
	require.NoError(t, err)
	callbackCookies := handoff.Result().Cookies()
	require.Len(t, callbackCookies, 1)
	toConsent, err := url.Parse(handoff.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, testAuthenticationHostURL+"/mcp/"+f.slug+"/connect", toConsent.Scheme+"://"+toConsent.Host+toConsent.Path)

	stray := httptest.NewRequestWithContext(ctx, http.MethodGet, toConsent.RequestURI(), nil)
	stray.AddCookie(callbackCookies[0])
	require.NotEqual(t, http.StatusFound, harness.serveRequest("auth.example.com", stray).Code, "the callback host's cookie does not prove the consent host's")

	confirm := httptest.NewRequestWithContext(ctx, http.MethodGet, toConsent.RequestURI(), nil)
	confirm.AddCookie(originCookies[0])
	ready := harness.serveRequest("auth.example.com", confirm)
	require.Equal(t, http.StatusFound, ready.Code, ready.Body.String())
	freshOrigin := ready.Result().Cookies()
	require.Len(t, freshOrigin, 1)
	readyURL, err := url.Parse(ready.Header().Get("Location"))
	require.NoError(t, err)

	_, err = f.callback(ctx, readyURL.Query(), nil)
	require.Error(t, err, "the ready stop needs the callback host's cookie")
	login, err := f.callback(ctx, readyURL.Query(), callbackCookies[0])
	require.NoError(t, err)
	require.Equal(t, "https://idp.example.com/authorize", login.Header().Get("Location"))

	query := url.Values{"state": {f.mock.buildAuthURLParams.State}, "code": {"idp-auth-code"}}
	_, err = f.callback(ctx, query, nil)
	require.ErrorContains(t, err, "sign-in browser binding is invalid", "another browser cannot deliver the IdP callback")
	_, err = f.callback(ctx, query, &http.Cookie{Name: callbackCookies[0].Name, Value: "another-browser"})
	require.ErrorContains(t, err, "sign-in browser binding is invalid")
	signedIn, err := f.callback(ctx, query, callbackCookies[0])
	require.NoError(t, err)
	landing, err := url.Parse(signedIn.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, testAuthenticationHostURL+"/mcp/"+f.slug+"/connect", landing.Scheme+"://"+landing.Host+landing.Path)
	bound, err := f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+landing.Query().Get("state"))
	require.NoError(t, err)
	require.Equal(t, "login", bound.Browser.Handoff)

	page := httptest.NewRequestWithContext(ctx, http.MethodGet, landing.RequestURI(), nil)
	page.AddCookie(freshOrigin[0])
	rendered := harness.serveRequest("auth.example.com", page)
	require.Equal(t, http.StatusOK, rendered.Code, rendered.Body.String())
}
