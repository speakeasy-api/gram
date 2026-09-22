package mcp

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ChallengeBrowserBinding survives ID rotation and identity verification. The
// two hosts have independent, host-only cookies; no browser proof travels in a URL.
// Legacy non-federated challenges have no binding.
type ChallengeBrowserBinding struct {
	CookieID     string `json:"cookie_id"`
	OriginHash   string `json:"origin_hash"`
	CallbackHash string `json:"callback_hash"`
}

func validateChallengeBrowser(r *http.Request, state AuthnChallengeState, callback bool) error {
	if state.Browser == nil {
		if state.Federation != nil {
			return errors.New("missing federated browser binding")
		}
		return nil
	}
	hash := state.Browser.OriginHash
	if callback {
		hash = state.Browser.CallbackHash
	}
	cookie, err := r.Cookie(federationCookieName(state.Browser.CookieID))
	if err != nil || hash == "" || state.Browser.CookieID == "" || state.CreatedAt.IsZero() || time.Since(state.CreatedAt) > state.TTL() || state.CreatedAt.After(time.Now().Add(time.Minute)) || subtle.ConstantTimeCompare([]byte(sha256Hex(cookieValue(cookie))), []byte(hash)) != 1 {
		return errors.New("challenge browser binding failed")
	}
	return nil
}

// Cross-origin bootstrap establishes only a callback-origin cookie. Before any
// upstream login, the browser must return to the mint origin and prove ownership
// of the cookie issued with /authorize. A copied bootstrap link cannot do that.
func (s *Service) prepareFederatedBrowserHandoff(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state *AuthnChallengeState) error {
	if state.Browser == nil || state.Browser.OriginHash == "" {
		return errors.New("missing initiating browser binding")
	}
	browser, err := generateOpaqueToken()
	if err != nil {
		return err
	}
	state.ID = uuid.NewString()
	state.Browser.CallbackHash = sha256Hex(browser)
	state.Federation.BrowserHash = state.Browser.CallbackHash
	state.Federation.StartPhase = "origin"
	target, err := endpoint.ConsentURL(state.mintOriginOr(s.serverURL.String()), state.ID)
	if err != nil {
		return err
	}
	if err := s.authnChallengeCache.Store(r.Context(), *state); err != nil {
		return fmt.Errorf("store federated browser handoff: %w", err)
	}
	http.SetCookie(w, federatedBrowserCookie(state.Browser.CookieID, browser, int(state.TTL().Seconds())))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target, http.StatusFound)
	return nil
}

func (s *Service) completeFederatedBrowserHandoff(w http.ResponseWriter, r *http.Request, state AuthnChallengeState) error {
	if state.Federation.StartPhase != "origin" {
		return oops.E(oops.CodeUnauthorized, nil, "invalid login handoff")
	}
	consumed, err := s.authnChallengeCache.GetAndDelete(r.Context(), "authnChallenge:"+state.ID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "login handoff expired")
	}
	if err := validateChallengeBrowser(r, consumed, false); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "invalid login browser")
	}
	if consumed.Federation == nil || consumed.Federation.StartPhase != "origin" || consumed.Browser.CallbackHash == "" {
		return oops.E(oops.CodeUnauthorized, nil, "invalid login handoff")
	}
	target, err := federatedCallbackURL(consumed.Federation.CallbackURL)
	if err != nil {
		return err
	}
	consumed.ID = uuid.NewString()
	consumed.Federation.StartPhase = "ready"
	if err := s.authnChallengeCache.Store(r.Context(), consumed); err != nil {
		return fmt.Errorf("store completed browser handoff: %w", err)
	}
	target.RawQuery = url.Values{"state": {consumed.ID}, "federated_start": {"1"}}.Encode()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target.String(), http.StatusFound)
	return nil
}

// The remote state is immutable and single-use. Peek before the manager can
// exchange a code or persist credentials; it still owns the atomic consumption.
// Dashboard-owned linking (FinalRedirectURI) does not have a consent parent.
func (s *Service) validateRemoteLoginBrowser(r *http.Request) error {
	remote, err := s.remoteLoginCache.Get(r.Context(), "remoteLogin:"+r.URL.Query().Get("state"))
	if err != nil {
		return fmt.Errorf("load remote login browser state: %w", err)
	}
	if remote.FinalRedirectURI != "" {
		return nil
	}
	parent, err := s.authnChallengeCache.Get(r.Context(), "authnChallenge:"+remote.ParentChallengeID)
	if err != nil {
		return fmt.Errorf("load parent browser state: %w", err)
	}
	if parent.Subject == nil || remote.Subject == nil || parent.Subject.String() != remote.Subject.String() || parent.UserSessionIssuerID != remote.UserSessionIssuerID {
		return errors.New("remote login parent mismatch")
	}
	return validateChallengeBrowser(r, parent, true)
}
