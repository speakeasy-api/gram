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
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
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
		remoteLoginCache:    cache.NewTypedObjectCache[remotesessions.RemoteLoginState](logger, store, cache.SuffixNone),
		remoteLoginHopCache: cache.NewTypedObjectCache[remoteLoginHop](logger, store, cache.SuffixNone)}
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
	endpoint := &ResolvedMcpEndpoint{RouteBase: mcproute.MCP, Slug: "test"}
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

func TestCodeReconnectBrowserProofPrecedesMemberLookup(t *testing.T) {
	t.Parallel()
	for _, hop := range []bool{false, true} {
		s := browserTestService(t) // No DB: member resolution before proof would panic.
		parent := browserTestState()
		parent.FirstParty = true
		parent.FocusServer = "tracker"
		parent.AuthorizerUserID = parent.Subject.ID
		parent.AuthorizerImpersonated = new(false)
		parent.CodeReconnect = &CodeReconnectBinding{UserID: parent.Subject.ID, Server: "tracker", ServerID: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}
		require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
		remote := remotesessions.RemoteLoginState{ID: uuid.NewString(), ParentChallengeID: parent.ID, Subject: parent.Subject, UserSessionIssuerID: parent.UserSessionIssuerID}
		if hop {
			remote.BrowserHash = sha256Hex("hop-browser")
			remote.BrowserCookieID = uuid.NewString()
		}
		require.NoError(t, s.remoteLoginCache.Store(t.Context(), remote))
		req := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/remote_login_callback?state="+remote.ID, nil)
		require.ErrorContains(t, s.validateRemoteLoginBrowser(req), "invalid remote login browser binding")
		if !hop {
			req.AddCookie(federatedBrowserCookie(parent.Browser.CookieID, "callback-browser", 600))
			parent.CodeReconnect.ExpiresAt = time.Now().Add(-time.Minute)
			require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
			w := httptest.NewRecorder()
			oops.BrowserErrHandle(s.logger, func(_ http.ResponseWriter, r *http.Request) error { return s.validateRemoteLoginBrowser(r) }).ServeHTTP(w, req)
			require.Equal(t, http.StatusUnauthorized, w.Code)
			require.Contains(t, w.Body.String(), "current browser or sign-in session")
		}
	}
}

func TestWorkloadReconnectBrowserFailureIsSessionChanged(t *testing.T) {
	t.Parallel()
	s := browserTestService(t)
	parent := browserTestState()
	parent.WorkloadReconnect = &WorkloadReconnectBinding{OwnerUserID: parent.Subject.ID, ExpiresAt: time.Now().Add(-time.Minute)}
	require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
	remote := remotesessions.RemoteLoginState{ID: uuid.NewString(), ParentChallengeID: parent.ID, Subject: parent.Subject, UserSessionIssuerID: parent.UserSessionIssuerID}
	require.NoError(t, s.remoteLoginCache.Store(t.Context(), remote))
	req := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/remote_login_callback?state="+remote.ID, nil)
	req.AddCookie(federatedBrowserCookie(parent.Browser.CookieID, "callback-browser", 600))
	response := httptest.NewRecorder()
	oops.BrowserErrHandle(s.logger, func(_ http.ResponseWriter, r *http.Request) error { return s.validateRemoteLoginBrowser(r) }).ServeHTTP(response, req)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), "current browser or sign-in session")
}

// firstPartyReviewState is a signed-in first-party catalogue review whose
// browser binding the returned origin and callback cookies prove.
func firstPartyReviewState(t *testing.T, s *Service, consentOrigin string) (AuthnChallengeState, *http.Cookie, *http.Cookie) {
	t.Helper()
	state := browserTestState()
	state.Browser = nil
	state.FirstParty = true
	state.CSRFToken = "known-form-token"
	state.FocusServer = "tracker"
	state.AuthorizerUserID = state.Subject.ID
	state.AuthorizerImpersonated = new(false)
	state.Endpoint.BaseURL = consentOrigin
	state.CodeReconnect = &CodeReconnectBinding{UserID: state.Subject.ID, Server: "tracker", ServerID: uuid.New(), ExpiresAt: time.Now().Add(time.Minute), CatalogID: uuid.New(), ReviewReady: true}
	origin, err := s.bindFirstPartyBrowser(&ResolvedMcpEndpoint{RouteBase: mcproute.MCP, Slug: "test"}, &state)
	require.NoError(t, err)
	callback := origin
	if state.Browser.CallbackHash == "" {
		response := httptest.NewRecorder()
		require.NoError(t, bindFirstPartyCallbackBrowser(response, &state, time.Now()))
		cookies := response.Result().Cookies()
		require.Len(t, cookies, 1)
		callback = cookies[0]
	}
	return state, origin, callback
}

func TestFirstPartyBrowserBinding(t *testing.T) {
	t.Parallel()
	s := browserTestService(t)
	state, origin, callback := firstPartyReviewState(t, s, "https://gram.example")
	require.Same(t, origin, callback, "one host shares one cookie")
	require.Equal(t, federationCookieName(state.ID), origin.Name)
	require.Equal(t, "/", origin.Path)
	require.Empty(t, origin.Domain)
	require.True(t, origin.HttpOnly)
	require.True(t, origin.Secure)
	require.Equal(t, http.SameSiteLaxMode, origin.SameSite)
	require.Equal(t, int(state.TTL().Seconds()), origin.MaxAge)
	require.Equal(t, sha256Hex(origin.Value), state.Browser.OriginHash)
	require.Equal(t, state.Browser.OriginHash, state.Browser.CallbackHash)
	require.Equal(t, "https://gram.example", state.Browser.CallbackOrigin)

	custom, customOrigin, customCallback := firstPartyReviewState(t, s, "https://custom.example")
	require.NotEqual(t, customOrigin.Value, customCallback.Value, "each host holds its own cookie")
	require.Equal(t, customOrigin.Name, customCallback.Name)
	require.Equal(t, sha256Hex(customCallback.Value), custom.Browser.CallbackHash)
	require.Equal(t, "https://gram.example", custom.Browser.CallbackOrigin, "the IdP callback host holds the callback cookie")
	second := httptest.NewRecorder()
	require.NoError(t, bindFirstPartyCallbackBrowser(second, &custom, time.Now()))
	require.Empty(t, second.Result().Cookies(), "a bound callback host is never rebound")
}

func TestFirstPartyConsentRejectsAnotherBrowser(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"get", "approve", "deny", "connect", "disconnect", "validate", "set_auto_refresh"} {
		for _, cookie := range []string{"none", "wrong"} {
			t.Run(action+"/"+cookie, func(t *testing.T) {
				t.Parallel()
				s := browserTestService(t)
				state, owner, _ := firstPartyReviewState(t, s, "https://gram.example")
				require.NoError(t, s.authnChallengeCache.Store(t.Context(), state))
				form := url.Values{"state": {state.ID}, "csrf_token": {state.CSRFToken}, "action": {action}}
				req := httptest.NewRequest(http.MethodPost, "https://gram.example/mcp/test/connect", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if action == "get" {
					req = httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/test/connect?state="+state.ID, nil)
				}
				if cookie == "wrong" {
					req.AddCookie(&http.Cookie{Name: owner.Name, Value: "another-browser"})
				}
				var err error
				if action == "get" || action == "approve" || action == "deny" {
					err = s.ServeConsent(httptest.NewRecorder(), req, &ResolvedMcpEndpoint{})
				} else {
					err = s.ServeConsentAction(httptest.NewRecorder(), req, &ResolvedMcpEndpoint{})
				}
				require.ErrorContains(t, err, "invalid consent browser binding")
				_, err = s.authnChallengeCache.Get(t.Context(), "authnChallenge:"+state.ID)
				require.NoError(t, err, "a refused browser must not consume the owner's challenge")

				owned := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/test/connect?state="+state.ID, nil)
				owned.AddCookie(owner)
				require.NoError(t, validateChallengeBrowser(owned, state, false))
			})
		}
	}
}

func TestFirstPartyConsentBrowserFailureShowsStartAgain(t *testing.T) {
	t.Parallel()
	s := browserTestService(t)
	state, _, _ := firstPartyReviewState(t, s, "https://gram.example")
	require.NoError(t, s.authnChallengeCache.Store(t.Context(), state))
	req := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/test/connect?state="+state.ID, nil)
	response := httptest.NewRecorder()
	oops.BrowserErrHandle(s.logger, func(w http.ResponseWriter, r *http.Request) error {
		return s.ServeConsent(w, r, &ResolvedMcpEndpoint{})
	}).ServeHTTP(response, req)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "current browser or sign-in session")
}

// A first-party challenge an earlier release stored has no binding and stays
// usable until it expires. Only the stored state can mark it so.
func TestFirstPartyChallengeWithoutRecordedBinding(t *testing.T) {
	t.Parallel()
	s := browserTestService(t)
	state, _, _ := firstPartyReviewState(t, s, "https://gram.example")
	state.Browser = nil
	req := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/test/connect?state="+state.ID, nil)
	require.NoError(t, validateChallengeBrowser(req, state, false))
	require.NoError(t, validateChallengeBrowser(req, state, true))

	bound, _, _ := firstPartyReviewState(t, s, "https://gram.example")
	require.Error(t, validateChallengeBrowser(req, bound, false), "a bound challenge never falls back to the unbound check")
	require.Error(t, validateChallengeBrowser(req, bound, true))
}

// An upstream login started from a first-party consent page completes only in
// the browser that started the challenge, on either host layout.
func TestFirstPartyRemoteCallbackRejectsAnotherBrowser(t *testing.T) {
	t.Parallel()
	for _, consentOrigin := range []string{"https://gram.example", "https://custom.example"} {
		t.Run(consentOrigin, func(t *testing.T) {
			t.Parallel()
			s := browserTestService(t)
			parent, _, callbackCookie := firstPartyReviewState(t, s, consentOrigin)
			parent.CodeReconnect = nil // No DB: a code connection would resolve its members.
			require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
			remote := remotesessions.RemoteLoginState{ID: uuid.NewString(), ParentChallengeID: parent.ID, Subject: parent.Subject, UserSessionIssuerID: parent.UserSessionIssuerID}
			require.NoError(t, s.remoteLoginCache.Store(t.Context(), remote))
			callback := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/remote_login_callback?state="+remote.ID+"&code=code", nil)
			// The manager is nil: refusal must precede any token exchange or credential storage.
			require.ErrorContains(t, s.HandleRemoteLoginCallback(httptest.NewRecorder(), callback), "invalid remote login browser binding")
			wrong := httptest.NewRequest(http.MethodGet, callback.URL.String(), nil)
			wrong.AddCookie(&http.Cookie{Name: callbackCookie.Name, Value: "another-browser"})
			require.ErrorContains(t, s.HandleRemoteLoginCallback(httptest.NewRecorder(), wrong), "invalid remote login browser binding")
			callback.AddCookie(callbackCookie)
			require.NoError(t, s.validateRemoteLoginBrowser(callback))
		})
	}
}

// A remote client whose callback is on another host goes through the bind and
// confirm hop, and the confirm stop requires the first-party callback cookie.
func TestFirstPartyRemoteLoginHopConfirmsBrowser(t *testing.T) {
	t.Parallel()
	s := browserTestService(t)
	parent, _, callbackCookie := firstPartyReviewState(t, s, "https://custom.example")
	require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
	remoteOrigin, err := url.Parse("https://callbacks.example")
	require.NoError(t, err)
	target, hop, err := s.startRemoteLogin(t.Context(), parent, remoteOrigin, remotesessions.ParentChallenge{ID: parent.ID}, func(remotesessions.ParentChallenge) (string, error) {
		return "https://upstream.example/authorize", nil
	})
	require.NoError(t, err)
	require.True(t, hop)
	require.True(t, strings.HasPrefix(target, "https://callbacks.example"+remoteLoginBindPath))
	bound := httptest.NewRecorder()
	require.NoError(t, s.HandleRemoteLoginBind(bound, httptest.NewRequest(http.MethodGet, target, nil)))
	confirmURL := bound.Header().Get("Location")
	require.True(t, strings.HasPrefix(confirmURL, "https://gram.example"+remoteLoginBindPath), confirmURL)
	require.ErrorContains(t, s.HandleRemoteLoginBind(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, confirmURL, nil)), "invalid remote login browser")

	// The refused confirm spent its stop; a fresh hop confirms with the cookie.
	target, _, err = s.startRemoteLogin(t.Context(), parent, remoteOrigin, remotesessions.ParentChallenge{ID: parent.ID}, func(remotesessions.ParentChallenge) (string, error) {
		return "https://upstream.example/authorize", nil
	})
	require.NoError(t, err)
	bound = httptest.NewRecorder()
	require.NoError(t, s.HandleRemoteLoginBind(bound, httptest.NewRequest(http.MethodGet, target, nil)))
	confirm := httptest.NewRequest(http.MethodGet, bound.Header().Get("Location"), nil)
	confirm.AddCookie(callbackCookie)
	confirmed := httptest.NewRecorder()
	require.NoError(t, s.HandleRemoteLoginBind(confirmed, confirm))
	require.Equal(t, "https://upstream.example/authorize", confirmed.Header().Get("Location"))
}

func TestFirstPartyConsentMCPRejectsAnotherBrowser(t *testing.T) {
	t.Parallel()
	for _, cookie := range []string{"none", "wrong"} {
		t.Run(cookie, func(t *testing.T) {
			t.Parallel()
			s := browserTestService(t)
			state, owner, _ := firstPartyReviewState(t, s, "https://gram.example")
			require.NoError(t, s.authnChallengeCache.Store(t.Context(), state))
			// The private ingress origin skips the custom-domain lockdown lookup.
			ctx := requestorigin.WithContext(t.Context(), requestorigin.Origin{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "https://private.example.ts.net"})
			endpoint := &ResolvedMcpEndpoint{Slug: "test", RouteBase: mcproute.MCP, McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
			request := func(cookie *http.Cookie) *http.Request {
				req := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://private.example.ts.net/mcp/test/connect/mcp", strings.NewReader(`{}`))
				req.Header.Set(consentStateHeader, state.ID)
				req.Header.Set(consentCSRFHeader, state.CSRFToken)
				req.Header.Set(consentAttemptHeader, uuid.NewString())
				if cookie != nil {
					req.AddCookie(cookie)
				}
				return req
			}
			var other *http.Cookie
			if cookie == "wrong" {
				other = &http.Cookie{Name: owner.Name, Value: "another-browser"}
			}
			require.ErrorContains(t, s.ServeConsentMCP(httptest.NewRecorder(), request(other), endpoint), "invalid consent browser binding")
			err := s.ServeConsentMCP(httptest.NewRecorder(), request(owner), endpoint)
			require.Error(t, err, "this fixture has no endpoint to serve")
			require.NotContains(t, err.Error(), "invalid consent browser binding", "the owner passes the browser check")
		})
	}
}

// An organization-owned client's callback is on the organization's platform
// host, not the IdP callback host, so the upstream sign-in passes the bind and
// confirm hop: bind on the organization host, confirm where the callback
// cookie is, then the callback checks the organization host's cookie.
func TestFirstPartyRemoteLoginThroughOrganizationHost(t *testing.T) {
	t.Parallel()
	s := browserTestService(t)
	parent, _, callbackCookie := firstPartyReviewState(t, s, "https://gram.example")
	parent.CodeReconnect = nil // No DB: a code connection would resolve its members.
	require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
	orgHost, err := url.Parse("https://acme.platform.example")
	require.NoError(t, err)
	var minted remotesessions.ParentChallenge
	target, hop, err := s.startRemoteLogin(t.Context(), parent, orgHost, remotesessions.ParentChallenge{ID: parent.ID}, func(p remotesessions.ParentChallenge) (string, error) {
		minted = p
		return "https://upstream.example/authorize", nil
	})
	require.NoError(t, err)
	require.True(t, hop)
	platform := func(base string, req *http.Request) *http.Request {
		return req.WithContext(requestorigin.WithContext(req.Context(), requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: base}))
	}
	bound := httptest.NewRecorder()
	require.NoError(t, s.HandleRemoteLoginBind(bound, platform("https://acme.platform.example", httptest.NewRequest(http.MethodGet, target, nil))), "organization hosts are platform hosts, which serve the bind stop")
	orgCookies := bound.Result().Cookies()
	require.Len(t, orgCookies, 1)
	confirm := platform("https://gram.example", httptest.NewRequest(http.MethodGet, bound.Header().Get("Location"), nil))
	require.True(t, strings.HasPrefix(confirm.URL.String(), "https://gram.example"+remoteLoginBindPath))
	confirm.AddCookie(callbackCookie)
	confirmed := httptest.NewRecorder()
	require.NoError(t, s.HandleRemoteLoginBind(confirmed, confirm))
	require.Equal(t, "https://upstream.example/authorize", confirmed.Header().Get("Location"))

	remote := remotesessions.RemoteLoginState{ID: uuid.NewString(), ParentChallengeID: parent.ID, Subject: parent.Subject, UserSessionIssuerID: parent.UserSessionIssuerID, BrowserCookieID: minted.BrowserCookieID, BrowserHash: minted.BrowserHash}
	require.NoError(t, s.remoteLoginCache.Store(t.Context(), remote))
	callback := platform("https://acme.platform.example", httptest.NewRequest(http.MethodGet, "https://acme.platform.example/mcp/remote_login_callback?state="+remote.ID+"&code=code", nil))
	require.ErrorContains(t, s.validateRemoteLoginBrowser(callback), "invalid remote login browser binding")
	callback.AddCookie(orgCookies[0])
	require.NoError(t, s.validateRemoteLoginBrowser(callback))
}
