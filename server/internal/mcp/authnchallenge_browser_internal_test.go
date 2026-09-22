package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func browserTestService(t *testing.T) *Service {
	t.Helper()
	store := testenv.NewMemoryCache()
	logger := testenv.NewLogger(t)
	serverURL, err := url.Parse("https://gram.example")
	require.NoError(t, err)
	return &Service{logger: logger, serverURL: serverURL,
		authnChallengeCache: cache.NewTypedObjectCache[AuthnChallengeState](logger, store, cache.SuffixNone),
		remoteLoginCache:    cache.NewTypedObjectCache[remotesessions.RemoteLoginState](logger, store, cache.SuffixNone)}
}

func browserTestState() AuthnChallengeState {
	subject := urn.NewUserSubject("test-user")
	return AuthnChallengeState{ID: uuid.NewString(), FlowID: uuid.NewString(), CreatedAt: time.Now(), Subject: &subject,
		Browser: &ChallengeBrowserBinding{CookieID: uuid.NewString(), OriginHash: sha256Hex("origin-browser"), CallbackHash: sha256Hex("callback-browser")}}
}

func TestConsentRejectsTransferredBrowserState(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"get", "approve", "deny", "connect", "disconnect", "validate", "set_auto_refresh", "retry_delegation"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			s := browserTestService(t)
			state := browserTestState()
			state.CSRFToken = "known-form-token"
			require.NoError(t, s.authnChallengeCache.Store(t.Context(), state))
			form := url.Values{"state": {state.ID}, "csrf_token": {state.CSRFToken}, "action": {action}}
			req := httptest.NewRequest(http.MethodPost, "https://gram.example/mcp/test/connect", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if action == "get" {
				req = httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/test/connect?state="+state.ID, nil)
			}
			var err error
			if action == "get" || action == "approve" || action == "deny" {
				err = s.ServeConsent(httptest.NewRecorder(), req, &ResolvedMcpEndpoint{})
			} else {
				err = s.ServeConsentAction(httptest.NewRecorder(), req, &ResolvedMcpEndpoint{})
			}
			require.ErrorContains(t, err, "invalid consent browser binding")
			_, err = s.authnChallengeCache.Get(t.Context(), "authnChallenge:"+state.ID)
			require.NoError(t, err, "copied state must not consume the owner's challenge")
			req.AddCookie(federatedBrowserCookie(state.Browser.CookieID, "origin-browser", 600))
			require.NoError(t, validateChallengeBrowser(req, state, false))
		})
	}
}

func TestFederatedCrossOriginBrowserHandoff(t *testing.T) {
	t.Parallel()
	s := browserTestService(t)
	state := browserTestState()
	state.Subject = nil
	state.Browser.CallbackHash = ""
	state.Endpoint.BaseURL = "https://custom.example"
	state.Federation = &FederatedChallenge{CallbackURL: "https://gram.example/mcp/idp_callback", StartPhase: "bootstrap"}
	endpoint := &ResolvedMcpEndpoint{RouteBase: "mcp", Slug: "test"}
	bootstrap := httptest.NewRecorder()
	require.NoError(t, s.prepareFederatedBrowserHandoff(bootstrap, httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/idp_callback", nil), endpoint, &state))
	require.Equal(t, "origin", state.Federation.StartPhase)
	require.Contains(t, bootstrap.Header().Get("Location"), "https://custom.example/")
	cookies := bootstrap.Result().Cookies()
	require.Len(t, cookies, 1)
	require.NotEqual(t, state.Browser.OriginHash, state.Browser.CallbackHash)
	transferred := httptest.NewRequest(http.MethodGet, bootstrap.Header().Get("Location"), nil)
	require.Error(t, validateChallengeBrowser(transferred, state, false), "a copied bootstrap URL does not prove original-browser ownership")
	transferred.AddCookie(cookies[0])
	require.Error(t, validateChallengeBrowser(transferred, state, false), "even the callback cookie cannot substitute for the origin cookie")
	owner := httptest.NewRequest(http.MethodGet, bootstrap.Header().Get("Location"), nil)
	owner.AddCookie(federatedBrowserCookie(state.Browser.CookieID, "origin-browser", 600))
	require.NoError(t, validateChallengeBrowser(owner, state, false))
	handoff := httptest.NewRecorder()
	require.NoError(t, s.completeFederatedBrowserHandoff(handoff, owner, state))
	target, err := url.Parse(handoff.Header().Get("Location"))
	require.NoError(t, err)
	bound, err := s.authnChallengeCache.Get(t.Context(), "authnChallenge:"+target.Query().Get("state"))
	require.NoError(t, err)
	require.NotEqual(t, state.ID, bound.ID)
	require.Equal(t, "ready", bound.Federation.StartPhase)
	callback := httptest.NewRequest(http.MethodGet, target.String(), nil)
	require.Error(t, validateChallengeBrowser(callback, bound, true))
	callback.AddCookie(cookies[0])
	require.NoError(t, validateChallengeBrowser(callback, bound, true))
	require.Error(t, s.completeFederatedBrowserHandoff(httptest.NewRecorder(), owner, state), "handoff is single-use")
	// The final identity callback rotates only the state ID and removes provider
	// secrets. Both origin and callback ownership remain independently verifiable.
	bound.ID = uuid.NewString()
	bound.Federation = nil
	require.NoError(t, validateChallengeBrowser(owner, bound, false))
	require.NoError(t, validateChallengeBrowser(callback, bound, true))
}

func TestRemoteCallbackRejectsTransferredBrowserState(t *testing.T) {
	t.Parallel()
	s := browserTestService(t)
	parent := browserTestState()
	require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
	remote := remotesessions.RemoteLoginState{ID: uuid.NewString(), ParentChallengeID: parent.ID, Subject: parent.Subject, UserSessionIssuerID: parent.UserSessionIssuerID}
	require.NoError(t, s.remoteLoginCache.Store(t.Context(), remote))
	callback := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/remote_login_callback?state="+remote.ID+"&code=code", nil)
	// The manager is deliberately nil: rejection must precede token exchange and
	// credential storage, even when a victim completes the upstream authorization.
	require.ErrorContains(t, s.HandleRemoteLoginCallback(httptest.NewRecorder(), callback), "invalid remote login browser binding")
	callback.AddCookie(federatedBrowserCookie(parent.Browser.CookieID, "callback-browser", 600))
	require.NoError(t, s.validateRemoteLoginBrowser(callback))
	_, err := s.remoteLoginCache.Get(t.Context(), "remoteLogin:"+remote.ID)
	require.NoError(t, err)
}
