package mcp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
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

const switchAccountUnavailable = "You can't switch accounts here."

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
// action endpoint has already checked method, endpoint, browser, CSRF and
// resolution. Only a sign-in Speakeasy refused as a personal account qualifies.
func (s *Service) switchSignInAccount(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger).With(attr.SlogOAuthFlowID(state.FlowID))
	if !accountSwitchAllowed(state) {
		return oops.E(oops.CodeBadRequest, nil, switchAccountUnavailable).LogWarn(ctx, logger)
	}
	userID, signInID := state.AuthorizerUserID, *state.SignInID
	result, err := signins.CheckConsentIdentity(ctx, s.db, endpoint.OrganizationID, userID, signInID, time.Now())
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "Speakeasy couldn't check your account; try again.").LogError(ctx, logger)
	}
	if result.Reason != signins.ReasonUnmanagedAccount {
		return oops.E(oops.CodeBadRequest, nil, switchAccountUnavailable).LogWarn(ctx, logger)
	}
	workosSessionID, err := signins.WorkOSSessionForSignIn(ctx, s.db, endpoint.OrganizationID, userID, signInID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "Speakeasy couldn't check your account; try again.").LogError(ctx, logger)
	}
	if workosSessionID == "" {
		return oops.E(oops.CodeBadRequest, nil, switchAccountUnavailable).LogWarn(ctx, logger)
	}

	// Single use: a replayed or stale action finds nothing to restart.
	consumed, err := s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+state.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "This page expired; start connecting again.").LogWarn(ctx, logger)
	}
	if !sameAccountSwitch(consumed, state) {
		s.restoreConsentChallenge(r, logger, consumed)
		return oops.E(oops.CodeBadRequest, nil, "This page changed; go back and try again.").LogWarn(ctx, logger)
	}

	next := consumed
	next.Subject = nil
	next.AuthorizerUserID = ""
	next.AuthorizerImpersonated = nil
	next.SignInID = nil
	next.AccountSwitches++
	if err := s.restartSignIn(w, r, logger, endpoint, next, workosSessionID, mcpmetrics.OAuthFlowStageConsent, true); err != nil {
		s.restoreConsentChallenge(r, logger, consumed)
		return err
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
