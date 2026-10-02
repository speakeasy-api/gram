package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// remoteLoginBindPath serves both stops of the callback-host browser hop.
const remoteLoginBindPath = "/mcp/remote_login_bind"

// remoteLoginHop carries a federated browser from its callback cookie host to
// a remote client's different callback host and back. The bind stop sets a
// host-only cookie on the callback host; the confirm stop proves the same
// browser still holds the parent challenge's callback cookie. Only then is the
// upstream authorization URL revealed, and the callback accepts the
// callback-host cookie only once a confirmed marker exists for it. Every stop
// is single-use.
type remoteLoginHop struct {
	ID                string    `json:"id"`
	Phase             string    `json:"phase"`
	ParentChallengeID string    `json:"parent_challenge_id"`
	CookieID          string    `json:"cookie_id"`
	Browser           string    `json:"browser,omitempty"`
	ConfirmURL        string    `json:"confirm_url"`
	AuthorizationURL  string    `json:"authorization_url"`
	CreatedAt         time.Time `json:"created_at"`
}

var _ cache.CacheableObject[remoteLoginHop] = (*remoteLoginHop)(nil)

func (h remoteLoginHop) CacheKey() string   { return "remoteLoginHop:" + h.ID }
func (h remoteLoginHop) TTL() time.Duration { return 10 * time.Minute }

const (
	remoteLoginHopBind      = "bind"
	remoteLoginHopConfirm   = "confirm"
	remoteLoginHopConfirmed = "confirmed"
)

// remoteLoginHopConfirmedID keys the marker a successful confirm stop leaves
// for the callback. It is never a valid one-time stop id.
func remoteLoginHopConfirmedID(cookieID string) string { return "confirmed-" + cookieID }

// startRemoteLogin mints the upstream login for parent. A federated challenge
// whose remote callback lands on a host other than its callback cookie host
// first binds the browser on that host; hop reports when the returned URL is
// that bind stop rather than the upstream authorization URL.
func (s *Service) startRemoteLogin(ctx context.Context, challengeState AuthnChallengeState, callbackOrigin *url.URL, parent remotesessions.ParentChallenge, mint func(remotesessions.ParentChallenge) (string, error)) (target string, hop bool, err error) {
	if challengeState.Browser == nil || callbackOrigin == nil || sameOrigin(callbackOrigin, s.outboundOrigin()) {
		target, err = mint(parent)
		return target, false, err
	}
	browser, err := generateOpaqueToken()
	if err != nil {
		return "", false, err
	}
	parent.BrowserCookieID = uuid.NewString()
	parent.BrowserHash = sha256Hex(browser)
	authorizationURL, err := mint(parent)
	if err != nil {
		return "", false, err
	}
	bindURL, err := url.JoinPath(strings.TrimRight(callbackOrigin.String(), "/"), remoteLoginBindPath)
	if err != nil {
		return "", false, fmt.Errorf("build remote login bind URL: %w", err)
	}
	confirmURL, err := url.JoinPath(strings.TrimRight(s.outboundOrigin().String(), "/"), remoteLoginBindPath)
	if err != nil {
		return "", false, fmt.Errorf("build remote login confirm URL: %w", err)
	}
	state := remoteLoginHop{
		ID:                uuid.NewString(),
		Phase:             remoteLoginHopBind,
		ParentChallengeID: challengeState.ID,
		CookieID:          parent.BrowserCookieID,
		Browser:           browser,
		ConfirmURL:        confirmURL,
		AuthorizationURL:  authorizationURL,
		CreatedAt:         time.Now(),
	}
	if err := s.remoteLoginHopCache.Store(ctx, state); err != nil {
		return "", false, fmt.Errorf("store remote login browser hop: %w", err)
	}
	return bindURL + "?" + url.Values{"state": {state.ID}}.Encode(), true, nil
}

// HandleRemoteLoginBind serves GET /mcp/remote_login_bind. The bind stop runs
// on the remote client's callback host and the confirm stop on the IdP
// callback host, which holds the parent challenge's callback cookie.
func (s *Service) HandleRemoteLoginBind(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface != requestorigin.SurfacePlatform {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}
	stateID := r.URL.Query().Get("state")
	if _, err := uuid.Parse(stateID); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "remote login handoff expired")
	}
	state, err := s.remoteLoginHopCache.GetAndDelete(ctx, "remoteLoginHop:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "remote login handoff expired")
	}
	remaining := time.Until(state.CreatedAt.Add(state.TTL()))
	if state.CreatedAt.IsZero() || remaining <= 0 || state.CreatedAt.After(time.Now().Add(time.Minute)) {
		return oops.E(oops.CodeUnauthorized, nil, "remote login handoff expired")
	}
	var target string
	switch state.Phase {
	case remoteLoginHopBind:
		if state.Browser == "" || state.CookieID == "" {
			return oops.E(oops.CodeUnauthorized, nil, "invalid remote login handoff")
		}
		http.SetCookie(w, federatedBrowserCookie(state.CookieID, state.Browser, int(remaining.Seconds())))
		state.ID = uuid.NewString()
		state.Phase = remoteLoginHopConfirm
		state.Browser = ""
		if err := s.remoteLoginHopCache.Store(ctx, state); err != nil {
			return fmt.Errorf("store remote login browser confirm: %w", err)
		}
		target = state.ConfirmURL + "?" + url.Values{"state": {state.ID}}.Encode()
	case remoteLoginHopConfirm:
		if err := s.validateRemoteLoginHopOwner(r, state); err != nil {
			return oops.E(oops.CodeUnauthorized, err, "invalid remote login browser")
		}
		target = state.AuthorizationURL
		confirmed := state
		confirmed.ID = remoteLoginHopConfirmedID(state.CookieID)
		confirmed.Phase = remoteLoginHopConfirmed
		confirmed.AuthorizationURL = ""
		if err := s.remoteLoginHopCache.Store(ctx, confirmed); err != nil {
			return fmt.Errorf("store remote login browser confirmation: %w", err)
		}
	default:
		return oops.E(oops.CodeUnauthorized, nil, "invalid remote login handoff")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target, http.StatusFound)
	return nil
}

// validateRemoteLoginHopOwner proves the confirming browser holds the parent
// challenge's callback cookie, so a copied bind link never reaches upstream.
func (s *Service) validateRemoteLoginHopOwner(r *http.Request, state remoteLoginHop) error {
	parent, err := s.authnChallengeCache.Get(r.Context(), "authnChallenge:"+state.ParentChallengeID)
	if err != nil {
		return fmt.Errorf("load parent browser state: %w", err)
	}
	if parent.Browser == nil {
		return errors.New("missing parent browser binding")
	}
	return validateChallengeBrowser(r, parent, true)
}

// validateRemoteLoginHopBrowser checks the callback-host cookie a bind stop set,
// and that the same browser then passed the confirm stop. A browser that
// followed a copied bind link holds the cookie but was never confirmed.
func (s *Service) validateRemoteLoginHopBrowser(r *http.Request, parent AuthnChallengeState, remote remotesessions.RemoteLoginState) error {
	if parent.Browser == nil || remote.BrowserCookieID == "" || parent.CreatedAt.IsZero() || time.Since(parent.CreatedAt) > parent.TTL() || parent.CreatedAt.After(time.Now().Add(time.Minute)) {
		return errors.New("remote login browser binding failed")
	}
	cookie, err := r.Cookie(federationCookieName(remote.BrowserCookieID))
	if err != nil || subtle.ConstantTimeCompare([]byte(sha256Hex(cookieValue(cookie))), []byte(remote.BrowserHash)) != 1 {
		return errors.New("remote login browser binding failed")
	}
	confirmed, err := s.remoteLoginHopCache.Get(r.Context(), "remoteLoginHop:"+remoteLoginHopConfirmedID(remote.BrowserCookieID))
	if err != nil || confirmed.Phase != remoteLoginHopConfirmed || confirmed.CookieID != remote.BrowserCookieID || confirmed.ParentChallengeID != remote.ParentChallengeID {
		return errors.New("remote login browser was not confirmed")
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
