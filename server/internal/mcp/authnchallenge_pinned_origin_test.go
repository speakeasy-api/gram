package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// pinnedOutbound is an outbound callback origin on a host other than the
// server URL, as after the server URL moves to a new brand.
var pinnedOutbound = &url.URL{Scheme: "https", Host: "app.example.test"}

func TestAuthorize_PinnedOutboundOriginIDPCallback(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)
	require.NotEqual(t, pinnedOutbound.Host, ti.serverURL.Host)
	ti.service.SetCallbackOrigins(remotesessions.CallbackOrigins{Outbound: pinnedOutbound, Registration: nil})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "pinned-callback-" + uuid.New().String()[:8]
	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	insertUserSessionClient(t, ctx, ti.conn, issuer.ID, "pinned-callback-client")

	q := url.Values{"response_type": {"code"}, "client_id": {"pinned-callback-client"}, "redirect_uri": {"http://example.com/cb"}, "state": {"state-123"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/authorize?"+q.Encode(), nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "https://app.example.test/mcp/idp_callback", loc.Query().Get("redirect_uri"), "the IdP callback stays on the pinned origin, not the server URL")
}

// A federated login with no recorded client callback origin uses the pinned
// outbound origin for its IdP callback.
func TestFederatedLoginPinnedOutboundCallbackOrigin(t *testing.T) {
	t.Parallel()
	runPinnedFederatedLogin(t, "", "https://app.example.test", false)
}

// A trusted client with a recorded callback_base_url gets its IdP callback on
// that host, next to its remote_login_callback, not on the outbound origin.
func TestFederatedLoginTrustedClientCallbackOrigin(t *testing.T) {
	t.Parallel()
	runPinnedFederatedLogin(t, "https://reg.example.test", "https://reg.example.test", false)
}

// The per-client callback is checked on the trusted client's own origin, not the outbound origin, before discovery.
func TestFederatedLoginChecksClientCallbackOrigin(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, outbound, recorded string
		wantOK                   bool
	}{
		{name: "https client on http outbound", outbound: "http://app.example.test", recorded: "https://reg.example.test", wantOK: true},
		{name: "http client on https outbound", outbound: "https://app.example.test", recorded: "http://reg.example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newFederationLoginFixture(t, true)
			outbound, err := url.Parse(test.outbound)
			require.NoError(t, err)
			f.ti.service.SetCallbackOrigins(remotesessions.CallbackOrigins{Outbound: outbound, Registration: nil})
			err = remotesessionsrepo.New(f.ti.conn).SetOrganizationRemoteSessionClientCallbackBaseURLFixture(ctx, remotesessionsrepo.SetOrganizationRemoteSessionClientCallbackBaseURLFixtureParams{CallbackBaseUrl: conv.ToPGText(test.recorded), ID: f.clientID, OrganizationID: conv.ToPGText(f.organizationID)})
			require.NoError(t, err)
			query := url.Values{"response_type": {"code"}, "client_id": {f.downstreamClientID}, "redirect_uri": {"http://127.0.0.1/callback"}, "state": {"downstream-state"}, "code_challenge": {"downstream-pkce"}, "code_challenge_method": {"S256"}}
			route := chi.NewRouteContext()
			route.URLParams.Add("mcpSlug", f.toolsetSlug)
			start := httptest.NewRecorder()
			err = f.ti.service.HandleAuthorize(start, httptest.NewRequest(http.MethodGet, f.ti.serverURL.String()+"/mcp/"+f.toolsetSlug+"/authorize?"+query.Encode(), nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route)))
			if !test.wantOK {
				require.NoError(t, err)
				failure := assertFederationErrorRedirect(t, start)
				require.Equal(t, "server_error", failure.Query().Get("error"), "an unusable client callback must not start login")
				require.Zero(t, f.provider.discoveryCount(), "rejected before discovery")
				return
			}
			require.NoError(t, err)
			require.Equal(t, http.StatusFound, start.Code)
			bootstrap, err := url.Parse(start.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, test.recorded+f.callbackPath(), bootstrap.Scheme+"://"+bootstrap.Host+bootstrap.Path)
		})
	}
}

func TestFederatedLoginPlatformCallbackWithPort(t *testing.T) {
	t.Parallel()
	origin := "https://" + platformHostChecklistExtraHost + ":8443"
	runPinnedFederatedLogin(t, origin, origin, true)
}

// runPinnedFederatedLogin runs a federated login whose IdP callback is on a
// host other than the server URL: the browser binds a cookie on the callback
// host, proves the original cookie on the server URL host, then logs in
// upstream. recorded is the trusted client's callback_base_url ("" for NULL).
func runPinnedFederatedLogin(t *testing.T, recorded, wantOrigin string, throughRouter bool) {
	t.Helper()
	ctx, f := newFederationLoginFixture(t, true)
	wantCallback := wantOrigin + f.callbackPath()
	ti, provider := f.ti, f.provider
	callbackHandler := ti.service.HandleIDPCallback
	var router http.Handler
	if throughRouter {
		router, _, _ = newPlatformHostMux(t, ti)
		callbackHandler = func(w http.ResponseWriter, r *http.Request) error {
			router.ServeHTTP(w, r)
			return nil
		}
	}
	require.NotEqual(t, pinnedOutbound.Host, ti.serverURL.Host)
	ti.service.SetCallbackOrigins(remotesessions.CallbackOrigins{Outbound: pinnedOutbound, Registration: nil})
	if recorded != "" {
		err := remotesessionsrepo.New(ti.conn).SetOrganizationRemoteSessionClientCallbackBaseURLFixture(ctx, remotesessionsrepo.SetOrganizationRemoteSessionClientCallbackBaseURLFixtureParams{CallbackBaseUrl: conv.ToPGText(recorded), ID: f.clientID, OrganizationID: conv.ToPGText(f.organizationID)})
		require.NoError(t, err)
	}

	query := url.Values{"response_type": {"code"}, "client_id": {f.downstreamClientID}, "redirect_uri": {"http://127.0.0.1/callback"}, "state": {"downstream-state"}, "code_challenge": {"downstream-pkce"}, "code_challenge_method": {"S256"}}
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", f.toolsetSlug)
	routed := context.WithValue(ctx, chi.RouteCtxKey, route)

	// /authorize on the server URL host binds the initiating browser there.
	start := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(start, httptest.NewRequest(http.MethodGet, ti.serverURL.String()+"/mcp/"+f.toolsetSlug+"/authorize?"+query.Encode(), nil).WithContext(routed)))
	require.Equal(t, http.StatusFound, start.Code)
	bootstrap, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, wantCallback, bootstrap.Scheme+"://"+bootstrap.Host+bootstrap.Path)
	require.Equal(t, "1", bootstrap.Query().Get("federated_start"))
	originCookies := start.Result().Cookies()
	require.Len(t, originCookies, 1)

	// The pinned callback host holds no cookie yet, so it binds one and sends
	// the browser back to the server URL host.
	handoff := httptest.NewRecorder()
	require.NoError(t, callbackHandler(handoff, routeIDPCallback(httptest.NewRequest(http.MethodGet, bootstrap.String(), nil).WithContext(ctx))))
	require.Equal(t, http.StatusFound, handoff.Code)
	consent, err := url.Parse(handoff.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, ti.serverURL.Host, consent.Host)
	callbackCookies := handoff.Result().Cookies()
	require.Len(t, callbackCookies, 1)

	// Only the initiating browser can complete the handoff.
	confirm := httptest.NewRequest(http.MethodGet, consent.String(), nil).WithContext(routed)
	confirm.AddCookie(originCookies[0])
	ready := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(ready, confirm))
	require.Equal(t, http.StatusFound, ready.Code)
	readyURL, err := url.Parse(ready.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, wantCallback, readyURL.Scheme+"://"+readyURL.Host+readyURL.Path)

	// Back on the callback host, the callback cookie starts the upstream login.
	begin := httptest.NewRecorder()
	beginRequest := httptest.NewRequest(http.MethodGet, readyURL.String(), nil).WithContext(ctx)
	beginRequest.AddCookie(callbackCookies[0])
	require.NoError(t, callbackHandler(begin, routeIDPCallback(beginRequest)))
	upstream, err := url.Parse(begin.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, provider.URL+"/authorize", upstream.Scheme+"://"+upstream.Host+upstream.Path)
	require.Equal(t, wantCallback, upstream.Query().Get("redirect_uri"), "the customer IdP sees the pinned callback")

	id, nonce := upstream.Query().Get("state"), upstream.Query().Get("nonce")
	provider.issueCode(t, "pinned-one-use-code", federationToken{challenge: upstream.Query().Get("code_challenge"), nonce: nonce, email: mockidp.MockUserEmail, issuer: provider.URL, verified: true, secret: "selected-secret"})
	callback := httptest.NewRequest(http.MethodGet, wantCallback+"?"+url.Values{"state": {id}, "code": {"pinned-one-use-code"}, "iss": {provider.URL}}.Encode(), nil).WithContext(ctx)
	callback.AddCookie(callbackCookies[0])
	if throughRouter {
		for _, host := range []string{ti.serverURL.Host, platformHostChecklistExtraHost, platformHostChecklistExtraHost + ":9443"} {
			headers := http.Header{"X-Forwarded-Host": {callback.Host}, "X-Forwarded-Proto": {"https"}, "Cookie": {callback.Header.Get("Cookie")}}
			rejected := serveOnHost(t, router, http.MethodGet, host, callback.URL.RequestURI(), nil, headers)
			require.Equal(t, http.StatusUnauthorized, rejected.Code, host)
			_, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+id)
			require.NoError(t, err, "misrouted callback must preserve state on %s", host)
			require.Zero(t, provider.exchangeCount())
		}
	}
	result := httptest.NewRecorder()
	require.NoError(t, callbackHandler(result, routeIDPCallback(callback)))
	require.Equal(t, http.StatusFound, result.Code)
	connect, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, ti.serverURL.Host, connect.Host)
	require.True(t, strings.HasSuffix(connect.Path, "/connect"))
	resolved, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+connect.Query().Get("state"))
	require.NoError(t, err)
	require.Equal(t, mockidp.MockUserID, resolved.AuthorizerUserID)
	require.Equal(t, wantOrigin, resolved.Browser.CallbackOrigin, "the remote login hop finds the callback cookie host here")
	require.Nil(t, resolved.Federation)
	require.Equal(t, 1, provider.exchangeCount())

	// The consent page still recognises the initiating browser.
	page := httptest.NewRequest(http.MethodGet, connect.String(), nil).WithContext(routed)
	page.AddCookie(originCookies[0])
	rendered := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(rendered, page))
	require.Equal(t, http.StatusOK, rendered.Code)
}
