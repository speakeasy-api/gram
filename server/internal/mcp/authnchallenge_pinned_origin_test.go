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
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// pinnedOutbound is an outbound callback origin on a host other than the
// server URL, as after the server URL moves to a new brand.
var pinnedOutbound = &url.URL{Scheme: "https", Host: "app.example.test"}

const pinnedIDPCallback = "https://app.example.test/mcp/idp_callback"

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
	require.Equal(t, pinnedIDPCallback, loc.Query().Get("redirect_uri"), "the IdP callback stays on the pinned origin, not the server URL")
}

// A federated login whose IdP callback is pinned to a host other than the
// server URL still completes: the browser binds a cookie on the callback host,
// proves the original cookie on the server URL host, then logs in upstream.
func TestFederatedLoginPinnedOutboundCallbackOrigin(t *testing.T) {
	t.Parallel()

	ctx, f := newFederationLoginFixture(t, true)
	ti, provider := f.ti, f.provider
	require.NotEqual(t, pinnedOutbound.Host, ti.serverURL.Host)
	ti.service.SetCallbackOrigins(remotesessions.CallbackOrigins{Outbound: pinnedOutbound, Registration: nil})

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
	require.Equal(t, pinnedIDPCallback, bootstrap.Scheme+"://"+bootstrap.Host+bootstrap.Path)
	require.Equal(t, "1", bootstrap.Query().Get("federated_start"))
	originCookies := start.Result().Cookies()
	require.Len(t, originCookies, 1)

	// The pinned callback host holds no cookie yet, so it binds one and sends
	// the browser back to the server URL host.
	handoff := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(handoff, httptest.NewRequest(http.MethodGet, bootstrap.String(), nil).WithContext(ctx)))
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
	require.Equal(t, pinnedIDPCallback, readyURL.Scheme+"://"+readyURL.Host+readyURL.Path)

	// Back on the callback host, the callback cookie starts the upstream login.
	begin := httptest.NewRecorder()
	beginRequest := httptest.NewRequest(http.MethodGet, readyURL.String(), nil).WithContext(ctx)
	beginRequest.AddCookie(callbackCookies[0])
	require.NoError(t, ti.service.HandleIDPCallback(begin, beginRequest))
	upstream, err := url.Parse(begin.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, provider.URL+"/authorize", upstream.Scheme+"://"+upstream.Host+upstream.Path)
	require.Equal(t, pinnedIDPCallback, upstream.Query().Get("redirect_uri"), "the customer IdP sees the pinned callback")

	id, nonce := upstream.Query().Get("state"), upstream.Query().Get("nonce")
	provider.issueCode(t, "pinned-one-use-code", federationToken{challenge: upstream.Query().Get("code_challenge"), nonce: nonce, email: mockidp.MockUserEmail, issuer: provider.URL, verified: true, secret: "selected-secret"})
	callback := httptest.NewRequest(http.MethodGet, pinnedIDPCallback+"?"+url.Values{"state": {id}, "code": {"pinned-one-use-code"}, "iss": {provider.URL}}.Encode(), nil).WithContext(ctx)
	callback.AddCookie(callbackCookies[0])
	result := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(result, callback))
	require.Equal(t, http.StatusFound, result.Code)
	connect, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, ti.serverURL.Host, connect.Host)
	require.True(t, strings.HasSuffix(connect.Path, "/connect"))
	resolved, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+connect.Query().Get("state"))
	require.NoError(t, err)
	require.Equal(t, mockidp.MockUserID, resolved.AuthorizerUserID)
	require.Nil(t, resolved.Federation)
	require.Equal(t, 1, provider.exchangeCount())

	// The consent page still recognises the initiating browser.
	page := httptest.NewRequest(http.MethodGet, connect.String(), nil).WithContext(routed)
	page.AddCookie(originCookies[0])
	rendered := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(rendered, page))
	require.Equal(t, http.StatusOK, rendered.Code)
}
