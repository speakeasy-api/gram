package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ChallengeBrowserBinding survives ID rotation and identity verification. The
// two hosts have independent, host-only cookies; no browser proof travels in a URL.
// Federated and first-party challenges are bound; MCP client challenges without
// federation, and first-party ones minted before their binding, are not.
type ChallengeBrowserBinding struct {
	CookieID     string `json:"cookie_id"`
	OriginHash   string `json:"origin_hash"`
	CallbackHash string `json:"callback_hash"`
	// CallbackOrigin is the origin of the federated IdP callback this challenge
	// was minted with, which holds the callback cookie. It outlives the
	// Federation state so the remote login hop can find that host. Empty on
	// challenges minted before it was recorded.
	CallbackOrigin string `json:"callback_origin,omitempty"`

	// ConsentOrigin is the origin of a first-party challenge's consent page,
	// which holds the origin cookie. Empty on federated challenges.
	ConsentOrigin string `json:"consent_origin,omitempty"`

	// Handoff is the step a first-party sign-in whose consent and IdP callback
	// hosts differ has reached in proving one browser holds both cookies:
	// bootstrap, origin, ready, then login. Empty when one host holds both.
	Handoff string `json:"handoff,omitempty"`

	// BoundAt starts a first-party binding's time limit, which a restarted
	// sign-in renews. Zero means the challenge's CreatedAt.
	BoundAt time.Time `json:"bound_at,omitzero"`
}

const (
	browserHandoffBootstrap = "bootstrap"
	browserHandoffOrigin    = "origin"
	browserHandoffReady     = "ready"
	browserHandoffLogin     = "login"
)

// errChallengeBrowserExpired is a browser binding past its time limit, which
// starting again fixes, unlike a request from another browser.
var errChallengeBrowserExpired = errors.New("challenge browser binding expired")

// browserBindingStart is when a bound challenge's time limit started.
func browserBindingStart(state AuthnChallengeState) time.Time {
	if state.Browser != nil && !state.Browser.BoundAt.IsZero() {
		return state.Browser.BoundAt
	}
	return state.CreatedAt
}

// browserBindingRemaining is how long a bound challenge's browser proof stays
// valid. Its cookies expire with it.
func browserBindingRemaining(state AuthnChallengeState, now time.Time) time.Duration {
	start := browserBindingStart(state)
	if start.IsZero() || start.After(now.Add(time.Minute)) {
		return 0
	}
	return start.Add(state.TTL()).Sub(now)
}

// browserBindingExpired reports a bound challenge past its time limit.
func browserBindingExpired(state AuthnChallengeState, now time.Time) bool {
	return state.Browser != nil && browserBindingRemaining(state, now) <= 0
}

func validateChallengeBrowser(r *http.Request, state AuthnChallengeState, callback bool) error {
	if err := validateCodeReconnectBinding(state); err != nil {
		return err
	}
	if err := validateWorkloadReconnectBinding(state); err != nil {
		return err
	}

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
	if browserBindingExpired(state, time.Now()) {
		return errChallengeBrowserExpired
	}
	if !browserCookieMatches(r, state.Browser.CookieID, hash) {
		return errors.New("challenge browser binding failed")
	}
	return nil
}

// browserCookieMatches reports whether the request carries the binding cookie whose hash is hash.
func browserCookieMatches(r *http.Request, cookieID, hash string) bool {
	if cookieID == "" || hash == "" {
		return false
	}
	cookie, err := r.Cookie(federationCookieName(cookieID))
	return err == nil && subtle.ConstantTimeCompare([]byte(sha256Hex(cookieValue(cookie))), []byte(hash)) == 1
}

// continueFirstPartyBrowserHandoff runs the handoff's IdP callback host stops.
// Bootstrap sets this host's cookie and sends the browser to its consent host;
// ready checks this host's cookie, which the consent host stop vouched for, and
// sends the browser to sign in.
func (s *Service) continueFirstPartyBrowserHandoff(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	ctx := r.Context()
	invalid := fmt.Errorf("validate sign-in handoff: %w", oops.WithBrowserReason(oops.E(oops.CodeUnauthorized, nil, "invalid sign-in browser"), oauthui.ReasonSessionChanged))
	phase := state.Browser.Handoff
	if phase != browserHandoffBootstrap && phase != browserHandoffReady {
		return invalid
	}
	if phase == browserHandoffReady && !browserCookieMatches(r, state.Browser.CookieID, state.Browser.CallbackHash) {
		return invalid
	}
	consumed, err := s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+state.ID)
	if err != nil {
		return fmt.Errorf("validate sign-in handoff: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, err, "sign-in handoff expired or was already used"), oauthui.ReasonExpired))
	}
	if consumed.Browser == nil || consumed.Federation != nil || consumed.Browser.Handoff != phase {
		return invalid
	}
	if phase == browserHandoffBootstrap {
		return s.prepareFederatedBrowserHandoff(w, r, endpoint, &consumed)
	}
	consumed.ID = uuid.NewString()
	consumed.Browser.Handoff = browserHandoffLogin
	target, err := s.firstPartyIDPAuthorizationURL(ctx, endpoint, consumed)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build IDP authorization URL").LogError(ctx, s.logger)
	}
	if err := s.authnChallengeCache.Store(ctx, consumed); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store sign-in handoff").LogError(ctx, s.logger)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target.String(), http.StatusFound)
	return nil
}

// bindFirstPartyBrowser binds a first-party challenge to the browser starting
// it on its consent host. When the IdP callback is on that host it shares the
// cookie; otherwise the browser must prove both hosts' cookies through the
// browser handoff before it signs in.
func (s *Service) bindFirstPartyBrowser(endpoint *ResolvedMcpEndpoint, state *AuthnChallengeState, now time.Time) (*http.Cookie, error) {
	origin, err := url.Parse(s.challengeAuthorizationServerBaseURL(endpoint, *state))
	if err != nil {
		return nil, fmt.Errorf("parse consent origin: %w", err)
	}
	callback := s.outboundOrigin()
	state.Browser = &ChallengeBrowserBinding{CookieID: state.ID, OriginHash: "", CallbackHash: "", CallbackOrigin: callback.Scheme + "://" + callback.Host, ConsentOrigin: origin.Scheme + "://" + origin.Host, Handoff: "", BoundAt: now}
	cookie, err := rebindBrowserCookie(state, true, now)
	if err != nil {
		return nil, err
	}
	if sameOrigin(origin, callback) {
		state.Browser.CallbackHash = state.Browser.OriginHash
	} else {
		state.Browser.Handoff = browserHandoffBootstrap
	}
	return cookie, nil
}

// rebindBrowserCookie issues a new cookie for the consent host (origin) or the
// IdP callback host, lasting as long as the binding.
func rebindBrowserCookie(state *AuthnChallengeState, origin bool, now time.Time) (*http.Cookie, error) {
	browser, err := generateOpaqueToken()
	if err != nil {
		return nil, fmt.Errorf("generate browser binding secret: %w", err)
	}
	if origin {
		state.Browser.OriginHash = sha256Hex(browser)
	} else {
		state.Browser.CallbackHash = sha256Hex(browser)
	}
	return federatedBrowserCookie(state.Browser.CookieID, browser, int(browserBindingRemaining(*state, now).Seconds())), nil
}

// firstPartyBrowserHandoffURL is the IdP callback host stop of a first-party
// browser handoff.
func firstPartyBrowserHandoffURL(endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (string, error) {
	target, err := endpoint.IDPCallbackURL(state.Browser.CallbackOrigin)
	if err != nil {
		return "", err
	}
	return target + "?" + url.Values{"state": {state.ID}, "federated_start": {"1"}}.Encode(), nil
}

// rebindFirstPartyBrowser renews a first-party binding's time for a restarted
// sign-in, from the IdP callback host its browser already proved. A browser
// whose consent host differs goes back there through the handoff.
func (s *Service) rebindFirstPartyBrowser(w http.ResponseWriter, endpoint *ResolvedMcpEndpoint, state *AuthnChallengeState, now time.Time) (handoff string, err error) {
	state.Browser.BoundAt = now
	if state.Browser.Handoff == "" {
		cookie, err := rebindBrowserCookie(state, true, now)
		if err != nil {
			return "", err
		}
		state.Browser.CallbackHash = state.Browser.OriginHash
		http.SetCookie(w, cookie)
		return "", nil
	}
	cookie, err := rebindBrowserCookie(state, false, now)
	if err != nil {
		return "", err
	}
	state.Browser.Handoff = browserHandoffOrigin
	http.SetCookie(w, cookie)
	return s.challengeConsentURL(endpoint, *state)
}

// Cross-origin bootstrap establishes only a callback-origin cookie. Before any
// login, the browser must return to the mint origin and prove ownership of the
// cookie issued when the challenge started. A copied bootstrap link cannot do that.
func (s *Service) prepareFederatedBrowserHandoff(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state *AuthnChallengeState) error {
	if state.Browser == nil || state.Browser.OriginHash == "" {
		return errors.New("missing initiating browser binding")
	}
	state.ID = uuid.NewString()
	cookie, err := rebindBrowserCookie(state, false, time.Now())
	if err != nil {
		return err
	}
	if state.Federation != nil {
		state.Federation.BrowserHash = state.Browser.CallbackHash
		state.Federation.StartPhase = "origin"
	} else {
		state.Browser.Handoff = browserHandoffOrigin
	}
	target, err := s.challengeConsentURL(endpoint, *state)
	if err != nil {
		return err
	}
	if err := s.authnChallengeCache.Store(r.Context(), *state); err != nil {
		return fmt.Errorf("store browser handoff: %w", err)
	}
	http.SetCookie(w, cookie)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target, http.StatusFound)
	return nil
}

// completeFederatedBrowserHandoff runs the handoff's consent host stop, which
// checks the origin cookie before the browser may sign in on the callback host.
// A first-party browser gets a fresh origin cookie, lasting as long as the binding.
func (s *Service) completeFederatedBrowserHandoff(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	if !browserHandoffAtOrigin(state) {
		return oops.E(oops.CodeBadRequest, nil, "invalid login handoff")
	}
	consumed, err := s.authnChallengeCache.GetAndDelete(r.Context(), "authnChallenge:"+state.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "login handoff expired")
	}
	if err := validateChallengeBrowser(r, consumed, false); err != nil {
		if errors.Is(err, errChallengeBrowserExpired) {
			return fmt.Errorf("validate login handoff: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, err, "login handoff expired"), oauthui.ReasonExpired))
		}
		return oops.E(oops.CodeBadRequest, err, "invalid login browser")
	}
	if !browserHandoffAtOrigin(consumed) || consumed.Browser.CallbackHash == "" {
		return oops.E(oops.CodeBadRequest, nil, "invalid login handoff")
	}
	consumed.ID = uuid.NewString()
	var target string
	if consumed.Federation != nil {
		callback, err := federatedCallbackURL(consumed.Federation.CallbackURL)
		if err != nil {
			return err
		}
		consumed.Federation.StartPhase = "ready"
		callback.RawQuery = url.Values{"state": {consumed.ID}, "federated_start": {"1"}}.Encode()
		target = callback.String()
	} else {
		cookie, err := rebindBrowserCookie(&consumed, true, time.Now())
		if err != nil {
			return err
		}
		consumed.Browser.Handoff = browserHandoffReady
		if target, err = firstPartyBrowserHandoffURL(endpoint, consumed); err != nil {
			return err
		}
		http.SetCookie(w, cookie)
	}
	if err := s.authnChallengeCache.Store(r.Context(), consumed); err != nil {
		return fmt.Errorf("store completed browser handoff: %w", err)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target, http.StatusFound)
	return nil
}

// browserHandoffAtOrigin reports a challenge waiting at the handoff's consent host stop.
func browserHandoffAtOrigin(state AuthnChallengeState) bool {
	if state.Federation != nil {
		return state.Federation.StartPhase == "origin"
	}
	return state.FirstParty && state.Browser != nil && state.Browser.Handoff == browserHandoffOrigin
}

// The remote state is immutable and single-use. Peek before the manager can
// exchange a code or persist credentials; it still owns the atomic consumption.
// Dashboard-owned linking (FinalRedirectURI) does not have a consent parent.
func (s *Service) validateRemoteLoginBrowser(r *http.Request) (retErr error) {
	remote, err := s.remoteLoginCache.Get(r.Context(), "remoteLogin:"+r.URL.Query().Get("state"))
	if err != nil {
		return fmt.Errorf("validate remote login browser: %w", s.remoteLoginStateError(r.Context(), err))
	}
	var parentReconnect *CodeReconnectBinding
	var parentWorkload *WorkloadReconnectBinding
	parentExpired := false
	defer func() {
		if retErr == nil {
			return
		}
		if parentReconnect.expired(time.Now()) || parentWorkload.expired(time.Now()) || parentExpired {
			s.endAttemptOnce(r.Context(), s.logger, remote.Attempt, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeExpired, append(remote.ConnectionOptions(), mcpconnectionattempts.FromRequest(r))...)
			return
		}
		s.recordRemoteLoginRefusal(r, remote, retErr)
	}()
	if recovery, err := s.verifyConnectionRecoveryCompletion(r, remote); recovery {
		return err
	}
	if remote.FinalRedirectURI != "" {
		return nil
	}
	parent, err := s.authnChallengeCache.Get(r.Context(), "authnChallenge:"+remote.ParentChallengeID)
	if err != nil {
		return fmt.Errorf("load parent login state: %w", s.remoteLoginStateError(r.Context(), err))
	}
	parentReconnect = parent.CodeReconnect
	parentWorkload = parent.WorkloadReconnect
	if browserBindingExpired(parent, time.Now()) {
		parentExpired = true
		return fmt.Errorf("validate remote login browser: %w", oops.WithBrowserReason(oops.E(oops.CodeUnauthorized, errChallengeBrowserExpired, "remote login link expired"), oauthui.ReasonExpired))
	}
	if parent.Subject == nil || remote.Subject == nil || parent.Subject.String() != remote.Subject.String() || parent.UserSessionIssuerID != remote.UserSessionIssuerID {
		return fmt.Errorf("validate remote login browser: %w", oops.WithBrowserReason(oops.E(oops.CodeUnauthorized, nil, "remote login parent mismatch"), oauthui.ReasonSessionChanged))
	}
	// A login on another callback host carries its own browser binding, set
	// there by the bind stop of the remote login browser hop.
	if remote.BrowserHash != "" {
		if err := s.validateRemoteLoginHopBrowser(r, parent, remote); err != nil {
			return err
		}
	} else if err := validateChallengeBrowser(r, parent, true); err != nil {
		return fmt.Errorf("validate remote login browser: %w", oops.WithBrowserReason(oops.E(oops.CodeUnauthorized, err, "invalid remote login browser binding"), oauthui.ReasonSessionChanged))
	}
	if err := validateWorkloadReconnectBinding(parent); err != nil {
		return fmt.Errorf("validate remote login browser: %w", oops.WithBrowserReason(oops.E(oops.CodeUnauthorized, err, "invalid workload connection binding"), oauthui.ReasonSessionChanged))
	}
	if err := validateCodeReconnectBinding(parent); err != nil {
		return fmt.Errorf("validate remote login browser: %w", oops.WithBrowserReason(oops.E(oops.CodeUnauthorized, err, "invalid code connection binding"), oauthui.ReasonSessionChanged))
	}
	if parent.CodeReconnect != nil {
		endpoint, err := s.loadResolvedMcpEndpointByRef(r.Context(), parent.Endpoint)
		if err != nil {
			return err
		}
		if _, err := s.consentSubjectMembers(r.Context(), endpoint, parent); err != nil {
			return err
		}
	}
	return nil
}

// remoteLoginStateError distinguishes spent links from transient storage failures.
func (s *Service) remoteLoginStateError(ctx context.Context, err error) error {
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return fmt.Errorf("load remote login state: %w", oops.WithBrowserReason(oops.E(oops.CodeUnauthorized, err, "remote login link expired or was already used"), oauthui.ReasonExpired))
	}
	return fmt.Errorf("load remote login state: %w", oops.WithBrowserReason(oops.E(oops.CodeUnavailable, err, "remote login state is temporarily unavailable").LogError(ctx, s.logger), oauthui.ReasonUnavailable))
}
