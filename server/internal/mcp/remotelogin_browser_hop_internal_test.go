package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// hopBrowser models host-only cookies: a request carries only the cookies set
// on responses from its own host.
type hopBrowser map[string][]*http.Cookie

func (b hopBrowser) request(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for _, c := range b[req.URL.Host] {
		req.AddCookie(c)
	}
	return req
}

func (b hopBrowser) keep(host string, w *httptest.ResponseRecorder) {
	b[host] = append(b[host], w.Result().Cookies()...)
}

// get serves target with the bind handler and returns the redirect location.
func (b hopBrowser) get(t *testing.T, s *Service, target string) (string, error) {
	t.Helper()
	req := b.request(target)
	w := httptest.NewRecorder()
	if err := s.HandleRemoteLoginBind(w, req); err != nil {
		require.Empty(t, w.Header().Get("Location"))
		require.Empty(t, w.Result().Cookies())
		return "", err
	}
	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	b.keep(req.URL.Host, w)
	return w.Header().Get("Location"), nil
}

type hopFixture struct {
	s       *Service
	parent  AuthnChallengeState
	owner   hopBrowser
	remotes []remotesessions.RemoteLoginState
}

// newHopFixture is a federated consent challenge whose callback cookie lives
// on the server URL host (gram.example), held by the owner browser.
func newHopFixture(t *testing.T) *hopFixture {
	t.Helper()
	s := browserTestService(t)
	parent := browserTestState()
	parent.Browser.OriginHash = sha256Hex("owner-browser")
	parent.Browser.CallbackHash = parent.Browser.OriginHash
	require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
	owner := hopBrowser{"gram.example": {federatedBrowserCookie(parent.Browser.CookieID, "owner-browser", 600)}}
	return &hopFixture{s: s, parent: parent, owner: owner, remotes: nil}
}

// start mints a remote login the way ChallengeManager.BuildAuthorizationUrl
// does: the remote state copies the parent's browser binding.
func (f *hopFixture) start(t *testing.T, callbackOrigin string) (string, bool, remotesessions.RemoteLoginState) {
	t.Helper()
	origin, err := url.Parse(callbackOrigin)
	require.NoError(t, err)
	var remote remotesessions.RemoteLoginState
	target, hop, err := f.s.startRemoteLogin(t.Context(), f.parent, origin, remotesessions.ParentChallenge{ID: f.parent.ID, Subject: f.parent.Subject, UserSessionIssuerID: f.parent.UserSessionIssuerID}, func(p remotesessions.ParentChallenge) (string, error) {
		remote = remotesessions.RemoteLoginState{ID: uuid.NewString(), ParentChallengeID: p.ID, Subject: p.Subject, UserSessionIssuerID: p.UserSessionIssuerID, BrowserCookieID: p.BrowserCookieID, BrowserHash: p.BrowserHash}
		require.NoError(t, f.s.remoteLoginCache.Store(t.Context(), remote))
		return "https://idp.example/authorize?state=" + remote.ID, nil
	})
	require.NoError(t, err)
	return target, hop, remote
}

func remoteCallback(remote remotesessions.RemoteLoginState, host string) string {
	return "https://" + host + "/mcp/remote_login_callback?state=" + remote.ID + "&code=code"
}

func TestRemoteLoginHop_FederatedLoginOnOtherCallbackHost(t *testing.T) {
	t.Parallel()
	f := newHopFixture(t)

	bind, hop, remote := f.start(t, "https://ai.example")
	require.True(t, hop)
	require.NotEmpty(t, remote.BrowserHash)
	bindURL, err := url.Parse(bind)
	require.NoError(t, err)
	require.Equal(t, "ai.example", bindURL.Host)
	require.Equal(t, remoteLoginBindPath, bindURL.Path)
	require.NotContains(t, bind, remote.ID, "the upstream state must not leak before the browser is proven")

	confirm, err := f.owner.get(t, f.s, bind)
	require.NoError(t, err)
	confirmURL, err := url.Parse(confirm)
	require.NoError(t, err)
	require.Equal(t, "gram.example", confirmURL.Host)
	require.Equal(t, remoteLoginBindPath, confirmURL.Path)
	require.NotEqual(t, bindURL.Query().Get("state"), confirmURL.Query().Get("state"), "each stop rotates the one-time id")
	require.Len(t, f.owner["ai.example"], 1, "the bind stop sets a cookie on the callback host")

	upstream, err := f.owner.get(t, f.s, confirm)
	require.NoError(t, err)
	require.Equal(t, "https://idp.example/authorize?state="+remote.ID, upstream)

	require.NoError(t, f.s.validateRemoteLoginBrowser(f.owner.request(remoteCallback(remote, "ai.example"))))
	_, err = f.s.remoteLoginCache.Get(t.Context(), "remoteLogin:"+remote.ID)
	require.NoError(t, err, "validation must not consume the remote state")
}

func TestRemoteLoginHop_CallbackWithoutBindIsRejected(t *testing.T) {
	t.Parallel()
	f := newHopFixture(t)
	_, hop, remote := f.start(t, "https://ai.example")
	require.True(t, hop)

	// The browser skipped the bind stop, so it holds no cookie on the callback host.
	err := f.s.HandleRemoteLoginCallback(httptest.NewRecorder(), f.owner.request(remoteCallback(remote, "ai.example")))
	require.ErrorContains(t, err, "invalid remote login browser binding")

	// Even the server-host callback cookie cannot stand in for the bind cookie.
	transferred := f.owner.request(remoteCallback(remote, "ai.example"))
	transferred.AddCookie(f.owner["gram.example"][0])
	require.Error(t, f.s.validateRemoteLoginBrowser(transferred))
	_, err = f.s.remoteLoginCache.Get(t.Context(), "remoteLogin:"+remote.ID)
	require.NoError(t, err)
}

func TestRemoteLoginHop_CopiedBindLinkNeverReachesUpstream(t *testing.T) {
	t.Parallel()
	f := newHopFixture(t)
	bind, hop, remote := f.start(t, "https://ai.example")
	require.True(t, hop)

	attacker := hopBrowser{}
	confirm, err := attacker.get(t, f.s, bind)
	require.NoError(t, err)
	_, err = attacker.get(t, f.s, confirm)
	require.ErrorContains(t, err, "invalid remote login browser")
	require.Error(t, f.s.validateRemoteLoginBrowser(attacker.request(remoteCallback(remote, "ai.example"))))

	// The owner cannot resume a hop another browser consumed.
	_, err = f.owner.get(t, f.s, bind)
	require.ErrorContains(t, err, "remote login handoff expired")
	_, err = f.owner.get(t, f.s, confirm)
	require.ErrorContains(t, err, "remote login handoff expired")
}

func TestRemoteLoginHop_StopsAreSingleUse(t *testing.T) {
	t.Parallel()
	f := newHopFixture(t)
	bind, _, remote := f.start(t, "https://ai.example")

	confirm, err := f.owner.get(t, f.s, bind)
	require.NoError(t, err)
	_, err = f.owner.get(t, f.s, bind)
	require.ErrorContains(t, err, "remote login handoff expired", "a consumed bind id is rejected")
	require.Error(t, f.s.validateRemoteLoginBrowser(f.owner.request(remoteCallback(remote, "ai.example"))), "the callback needs the confirm stop too")

	_, err = f.owner.get(t, f.s, confirm)
	require.NoError(t, err)
	_, err = f.owner.get(t, f.s, confirm)
	require.ErrorContains(t, err, "remote login handoff expired", "a consumed confirm id is rejected")

	// The confirmation marker is not addressable as a stop.
	_, err = f.owner.get(t, f.s, "https://gram.example"+remoteLoginBindPath+"?state="+remoteLoginHopConfirmedID(remote.BrowserCookieID))
	require.ErrorContains(t, err, "remote login handoff expired")
	require.NoError(t, f.s.validateRemoteLoginBrowser(f.owner.request(remoteCallback(remote, "ai.example"))))
}

func TestRemoteLoginHop_SameHostClientIsUnchanged(t *testing.T) {
	t.Parallel()
	f := newHopFixture(t)

	target, hop, remote := f.start(t, "https://gram.example")
	require.False(t, hop)
	require.Equal(t, "https://idp.example/authorize?state="+remote.ID, target)
	require.Empty(t, remote.BrowserCookieID)
	require.Empty(t, remote.BrowserHash)
	require.NoError(t, f.s.validateRemoteLoginBrowser(f.owner.request(remoteCallback(remote, "gram.example"))))
}

func TestRemoteLoginHop_NonFederatedParentIsUnchanged(t *testing.T) {
	t.Parallel()
	f := newHopFixture(t)
	f.parent.Browser = nil
	require.NoError(t, f.s.authnChallengeCache.Store(t.Context(), f.parent))

	target, hop, remote := f.start(t, "https://ai.example")
	require.False(t, hop)
	require.Equal(t, "https://idp.example/authorize?state="+remote.ID, target)
	require.Empty(t, remote.BrowserHash)
	require.NoError(t, f.s.validateRemoteLoginBrowser(hopBrowser{}.request(remoteCallback(remote, "ai.example"))))
}

func TestRemoteLoginHop_RefusedOnCustomDomain(t *testing.T) {
	t.Parallel()
	f := newHopFixture(t)
	bind, _, _ := f.start(t, "https://ai.example")

	req := f.owner.request(bind)
	req = req.WithContext(requestorigin.WithContext(req.Context(), requestorigin.Origin{Surface: requestorigin.SurfaceCustomDomain, BaseURL: "https://ai.example"}))
	require.ErrorContains(t, f.s.HandleRemoteLoginBind(httptest.NewRecorder(), req), "not found")
	_, err := f.owner.get(t, f.s, bind)
	require.NoError(t, err, "a refused request must not consume the hop")
}
