package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// consentActionSwitchAccount is the consent action and check result that let
// a person signed in with a personal account sign in again with another one.
const consentActionSwitchAccount = "switch_account"

// maxAccountSwitches lets a person pick the wrong account more than once
// without letting one attempt restart sign-in forever.
const maxAccountSwitches = 3

// switchAccountMinRemaining is the least of the attempt's lifetime a fresh
// sign-in must have left to finish in.
const switchAccountMinRemaining = 2 * time.Minute

// The switch action's errors: one short sentence each.
const (
	switchAccountRetry       = "Speakeasy couldn't start a new sign-in. Try again."
	switchAccountUnavailable = "This page can't start a new sign-in. Go back and use a separate sign-in."
	switchAccountExpired     = "This page expired; start connecting again."
	switchAccountChanged     = "This page changed. Go back, reload it, and try again."
)

// accountSwitchVerdict is whether a consent can offer, and accept, a switch.
type accountSwitchVerdict int

const (
	accountSwitchOK accountSwitchVerdict = iota
	accountSwitchNotOffered
	accountSwitchExpired
)

// accountSwitchAllowed reports whether a consent can send its person back to
// sign in to Speakeasy with another account: a WorkOS sign-in of their own,
// not one a dashboard, workload, code-mode or federated flow is bound to.
func accountSwitchAllowed(state AuthnChallengeState) bool {
	return !state.FirstParty &&
		state.Federation == nil && state.FederatedBinding == nil &&
		state.CodeReconnect == nil && state.WorkloadReconnect == nil &&
		state.SignInID != nil &&
		state.Subject != nil && state.Subject.Kind == urn.SessionSubjectKindUser &&
		state.AuthorizerUserID != "" && state.Subject.ID == state.AuthorizerUserID &&
		state.AuthorizerImpersonated != nil && !*state.AuthorizerImpersonated &&
		state.AccountSwitches < maxAccountSwitches
}

// accountSwitchTooLate reports an attempt without time left for a fresh sign-in.
func accountSwitchTooLate(state AuthnChallengeState, now time.Time) bool {
	return state.CreatedAt.IsZero() || now.After(state.CreatedAt.Add(state.TTL()-switchAccountMinRemaining))
}

// accountSwitchOffered is the one predicate both the check's offer and the
// action's acceptance use: an eligible consent, with time left, whose own
// sign-in Speakeasy refuses as a personal account.
func (s *Service) accountSwitchOffered(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, now time.Time) (accountSwitchVerdict, error) {
	if !accountSwitchAllowed(state) {
		return accountSwitchNotOffered, nil
	}
	if accountSwitchTooLate(state, now) {
		return accountSwitchExpired, nil
	}
	result, err := signins.CheckConsentIdentity(ctx, s.db, endpoint.OrganizationID, state.AuthorizerUserID, *state.SignInID, now)
	if err != nil {
		return accountSwitchNotOffered, fmt.Errorf("check consent sign-in: %w", err)
	}
	if result.Reason != signins.ReasonUnmanagedAccount {
		return accountSwitchNotOffered, nil
	}
	return accountSwitchOK, nil
}

// sameAccountSwitch reports whether the consumed challenge is still the one the
// action was checked against.
func sameAccountSwitch(consumed, checked AuthnChallengeState) bool {
	return consumed.ID == checked.ID && consumed.FlowID == checked.FlowID &&
		consumed.CSRFToken == checked.CSRFToken &&
		consumed.AuthorizerUserID == checked.AuthorizerUserID &&
		consumed.Subject != nil && *consumed.Subject == *checked.Subject &&
		consumed.SignInID != nil && *consumed.SignInID == *checked.SignInID &&
		consumed.AccountSwitches == checked.AccountSwitches &&
		accountSwitchAllowed(consumed)
}

// switchSignInAccount ends the person's own Speakeasy sign-in and sends them to
// sign in again, without reusing it, for this same authorization request. The
// action endpoint has already checked method, endpoint, browser, CSRF,
// resolution and a gateway's custom-domain lockdown.
func (s *Service) switchSignInAccount(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger).With(attr.SlogOAuthFlowID(state.FlowID))

	focusServer := r.PostForm.Get("server")
	if focusServer != "" {
		if !endpoint.MetaMcpServerID.Valid || len(focusServer) > maxFocusServerLength {
			return oops.E(oops.CodeBadRequest, nil, switchAccountChanged).LogWarn(ctx, logger)
		}
		members, err := s.consentSubjectMembers(ctx, endpoint, state)
		if err != nil {
			return oops.E(oops.CodeUnavailable, err, switchAccountRetry).LogError(ctx, logger)
		}
		if !slices.ContainsFunc(members, func(member metaMember) bool { return member.slug == focusServer }) {
			return oops.E(oops.CodeBadRequest, nil, switchAccountChanged).LogWarn(ctx, logger)
		}
	}
	if _, _, err := s.chainedConsentClient(ctx, logger, endpoint, state, r.PostForm.Get("client_id")); err != nil {
		return err
	}
	verdict, err := s.accountSwitchOffered(ctx, endpoint, state, time.Now())
	switch {
	case err != nil:
		return oops.E(oops.CodeUnavailable, err, switchAccountRetry).LogError(ctx, logger)
	case verdict == accountSwitchExpired:
		return oops.E(oops.CodeBadRequest, nil, switchAccountExpired).LogWarn(ctx, logger)
	case verdict != accountSwitchOK:
		return oops.E(oops.CodeBadRequest, nil, switchAccountUnavailable).LogWarn(ctx, logger)
	}
	session, err := signins.SessionForSignIn(ctx, s.db, endpoint.OrganizationID, state.AuthorizerUserID, *state.SignInID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, switchAccountRetry).LogError(ctx, logger)
	}
	if session.WorkOSSessionID == "" {
		return oops.E(oops.CodeBadRequest, nil, switchAccountUnavailable).LogWarn(ctx, logger)
	}

	// Single use: a replayed or stale action finds nothing to restart.
	consumed, err := s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+state.ID)
	switch {
	case errors.Is(err, redisCache.ErrCacheMiss):
		return oops.E(oops.CodeBadRequest, err, switchAccountExpired).LogWarn(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnavailable, err, switchAccountRetry).LogError(ctx, logger)
	}
	if !sameAccountSwitch(consumed, state) {
		s.restoreConsentChallenge(r, logger, consumed)
		return oops.E(oops.CodeBadRequest, nil, switchAccountChanged).LogWarn(ctx, logger)
	}

	next := consumed
	next.Subject = nil
	next.AuthorizerUserID = ""
	next.AuthorizerImpersonated = nil
	next.SignInID = nil
	next.AccountSwitches++
	if focusServer != "" {
		next.FocusServer = focusServer
	}
	location, err := s.restartSignIn(w, r, logger, endpoint, next, signInRestart{
		WorkOSSessionID:      session.WorkOSSessionID,
		Stage:                mcpmetrics.OAuthFlowStageConsent,
		MustEnd:              true,
		LoginHint:            session.WorkEmail,
		WorkOSOrganizationID: session.WorkOSOrganizationID,
	})
	if err != nil {
		s.restoreConsentChallenge(r, logger, consumed)
		return err
	}
	// The old page's URL, reached again with Back, goes on to the new sign-in.
	pointer := accountSwitchPointer{ID: consumed.ID, McpSlug: endpoint.Slug, Location: location}
	if err := s.accountSwitchCache.Store(context.WithoutCancel(ctx), pointer); err != nil {
		logger.WarnContext(ctx, "remember account switch for the old consent page", attr.SlogError(err))
	}
	return nil
}

// restoreConsentChallenge puts back a challenge this request consumed and did
// not move on, so the person's page keeps working.
func (s *Service) restoreConsentChallenge(r *http.Request, logger *slog.Logger, state AuthnChallengeState) {
	ctx := r.Context()
	if err := s.authnChallengeCache.Store(context.WithoutCancel(ctx), state); err != nil {
		logger.ErrorContext(ctx, "restore consent challenge", attr.SlogError(err))
	}
}

// accountSwitchPointer sends a consent page whose challenge an account switch
// spent on to that switch's fresh sign-in. The page's own URL already grants
// everything the pointer reaches, so it adds no capability.
type accountSwitchPointer struct {
	ID       string `json:"id"`
	McpSlug  string `json:"mcp_slug"`
	Location string `json:"location"`
}

var _ cache.CacheableObject[accountSwitchPointer] = (*accountSwitchPointer)(nil)

// CacheKey implements cache.CacheableObject.
func (p accountSwitchPointer) CacheKey() string { return "authnChallengeSwitched:" + p.ID }

// TTL implements cache.CacheableObject.
func (p accountSwitchPointer) TTL() time.Duration {
	var challenge AuthnChallengeState
	return challenge.TTL()
}

// followAccountSwitch redirects a consent page whose challenge an account
// switch spent to that switch's sign-in, reporting whether it did.
func (s *Service) followAccountSwitch(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, stateID string) bool {
	pointer, err := s.accountSwitchCache.Get(r.Context(), "authnChallengeSwitched:"+stateID)
	if err != nil || pointer.McpSlug != endpoint.Slug || pointer.Location == "" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, pointer.Location, http.StatusFound)
	return true
}
