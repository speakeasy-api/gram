// IDP callback handler for the issuer-gated authn-challenge surface.
// Pairs with remote_login_callback (in remotesessions/) — the other
// callback on this surface, used for upstream OAuth resource providers
// (Linear, Notion, etc.). Reading the two side-by-side: IDP returns user
// identity; remote returns resource-access tokens.

package mcp

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// HandleIDPCallback is the GET endpoint the IDP redirects back to after the
// user authenticates on the private-toolset path. Mounted at
// `GET /mcp/idp_callback`; the legacy `GET /mcp/{mcpSlug}/idp_callback`
// route is still accepted, but the toolset is resolved from the stored
// AuthnChallengeState.
//
// Without a trusted client it drives the unchanged WorkOS bootstrap. With an
// explicit trusted client it verifies OIDC identity and resolves an existing
// provisioned human without creating or synchronizing users.
//
// Side effects on success: AuthnChallengeState rewrite (subject stamped);
// WorkOS alone may upsert users. Federation offers ephemeral credentials only
// to an explicitly installed, post-authorization consumer. No chat session
// persists.
func (s *Service) HandleIDPCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	routeMcpSlug := chi.URLParam(r, "mcpSlug")
	logger := s.logger

	q := r.URL.Query()
	stateID := q.Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger)
	}

	// Peek first so a transient live-authority lookup can return 503 without
	// burning the single-use callback state. After validation below, GETDEL
	// atomically elects one callback winner before any identity side effects.
	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		// No challenge in hand (expired / replayed / never existed): nothing to
		// attribute to an issuer, and an expired state is closer to abandonment
		// than a flow failure, so it is left to the started-without-terminal gap.
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, logger)
	}

	if challengeState.Subject != nil || challengeState.AuthorizerUserID != "" {
		// Resolved identities cannot be replaced, even in malformed legacy state.
		// Consume the invalid callback key before rejecting it.
		if _, err := s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+stateID); err != nil {
			return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge identity is already resolved").LogError(ctx, logger)
	}

	// Challenge in hand: correlate every subsequent log line by flow_id, and
	// use the cached ref's issuer/slug for flow metrics until the endpoint is
	// re-resolved below.
	logger = logger.With(attr.SlogOAuthFlowID(challengeState.FlowID))
	issuerID := challengeState.UserSessionIssuerID.String()
	mcpSlug := challengeState.Endpoint.McpSlug

	if mcpSlug == "" {
		// Corrupted in-flight state (a code/data integrity failure), terminal
		// for the flow.
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
		return oops.E(oops.CodeBadRequest, nil, "mcp slug is missing from authn challenge state").LogError(ctx, logger)
	}
	if routeMcpSlug != "" && routeMcpSlug != mcpSlug {
		// State-confusion guard (state minted for a different route). Attacker-
		// controllable, so deliberately NOT counted as a flow failure.
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge state does not match this MCP server").LogError(ctx, logger)
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(ctx, challengeState.Endpoint)
	if err != nil {
		// The endpoint backing an in-flight challenge could not be re-resolved
		// (e.g. toolset removed mid-flow) — a config-class terminal failure.
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
		return err
	}
	authorityStarted := time.Now()
	if err := endpoint.ValidateGlobalChallenge(ctx, s.db, challengeState.Endpoint, challengeState.UserSessionIssuerID); err != nil {
		s.recordPrivateOAuthAuthority(ctx, challengeState.Endpoint.Authority, authorityStarted, err)
		if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
			s.metrics.RecordOAuthAuthorityUnavailable(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
			return oops.E(oops.CodeUnavailable, err, "private OAuth authority lookup is unavailable").LogError(ctx, logger)
		}
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
		return oops.E(oops.CodeUnauthorized, err, "authn challenge endpoint authority changed while the flow was in progress").LogError(ctx, logger)
	}
	s.recordPrivateOAuthAuthority(ctx, challengeState.Endpoint.Authority, authorityStarted, nil)

	// Ready bootstrap URLs carry no browser proof themselves. Check the
	// host-only callback cookie before consuming state so a copied URL cannot
	// prevent the initiating browser from completing its login (including retries).
	if federation := challengeState.Federation; federation != nil && federation.StartPhase == "ready" {
		if err := validateChallengeBrowser(r, challengeState, true); err != nil {
			return s.finishFederatedFailure(w, r, endpoint, challengeState, mcpmetrics.OAuthFlowStageIDPCallback, oops.CodeUnauthorized, remotesessions.ErrFederatedIdentity, "Login browser binding is invalid. Restart login", false)
		}
	}

	challengeState, err = s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, logger)
	}
	// Reject identity replacement against the consumed value so invalid callbacks
	// stay single-use without consuming state on transient authority failures.
	if challengeState.Subject != nil || challengeState.AuthorizerUserID != "" {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge identity is already resolved").LogError(ctx, logger)
	}
	// Recheck the consumed snapshot without another live lookup.
	if err := endpoint.validateChallengeRef(challengeState.Endpoint, challengeState.UserSessionIssuerID); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge endpoint authority changed while the flow was in progress").LogError(ctx, logger)
	}

	logger = endpoint.LogWith(logger)
	// issuerID is unchanged (same issuer the ref resolved to); re-point mcpSlug
	// at the resolved endpoint's canonical slug for the flow-metric dimension.
	mcpSlug = endpoint.Slug

	// This handler is registered at a global URL and so has no
	// customdomains.Context of its own; every URL it emits hangs off the
	// mint-time origin instead. Both responses it can produce — the forwarded
	// IDP error and the consent redirect — use this one value, so the client
	// sees the same origin whichever way the flow goes.
	baseURL := challengeState.mintOriginOr(s.serverURL.String())

	finishFederation := func(code oops.Code, cause error, message string, declined bool) error {
		return s.finishFederatedFailure(w, r, endpoint, challengeState, mcpmetrics.OAuthFlowStageIDPCallback, code, cause, message, declined)
	}
	failFederationDependency := func(err error, message string) error {
		code, cause := federatedFailure(err)
		return finishFederation(code, cause, message, false)
	}
	// Resolve through the live endpoint organization; a callback cannot select
	// a provider, and an in-flight challenge can never switch to or from WorkOS.
	optionalRefused := false
	provider, trustedIssuerID, trustedClientID, configuration, err := s.federatedProvider(ctx, endpoint)
	if err != nil {
		return failFederationDependency(err, "Login configuration is unavailable. Restart login or contact your administrator")
	}
	if (provider == nil) != (challengeState.Federation == nil) {
		return finishFederation(oops.CodeFailedPrecondition, remotesessions.ErrFederatedConfiguration, "Login configuration changed. Restart login or contact your administrator", false)
	}
	if federation := challengeState.Federation; federation != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		for _, parameter := range []string{"state", "code", "iss", "error", "federated_start"} {
			if len(q[parameter]) > 1 {
				return finishFederation(oops.CodeUnauthorized, remotesessions.ErrFederatedIdentity, "Invalid login response", false)
			}
		}
		callbackURL, callbackErr := endpoint.IDPCallbackURL(s.serverURL.String())
		if callbackErr != nil || federation.OrganizationID != endpoint.OrganizationID || federation.IssuerID != trustedIssuerID || federation.ClientID != trustedClientID || federation.Configuration != configuration || federation.CallbackURL != callbackURL || challengeState.CreatedAt.IsZero() || time.Since(challengeState.CreatedAt) > challengeState.TTL() {
			return finishFederation(oops.CodeFailedPrecondition, remotesessions.ErrFederatedConfiguration, "Login configuration changed or expired. Restart login", false)
		}
		if q.Get("federated_start") == "1" && q.Get("code") == "" && q.Get("error") == "" {
			if federation.StartPhase == "bootstrap" {
				if err := s.prepareFederatedBrowserHandoff(w, r, endpoint, &challengeState); err != nil {
					return failFederationDependency(err, "Federated browser handoff failed. Restart login")
				}
				return nil
			}
			if federation.StartPhase != "ready" || validateChallengeBrowser(r, challengeState, true) != nil {
				return finishFederation(oops.CodeUnauthorized, remotesessions.ErrFederatedIdentity, "Login browser binding is invalid. Restart login", false)
			}
			if err := s.startFederatedLogin(w, r, &challengeState, provider); err != nil {
				return failFederationDependency(err, "Federated login is unavailable. Restart login or contact your administrator")
			}
			return nil
		}
		if err := validateFederatedBrowser(r, challengeState); err != nil || federation.StartPhase != "login" || q.Get("federated_start") != "" {
			return finishFederation(oops.CodeUnauthorized, remotesessions.ErrFederatedIdentity, "Login browser binding is invalid. Restart login", false)
		}
		if err := provider.ValidateResponseIssuer(q.Get("iss")); err != nil {
			return finishFederation(oops.CodeUnauthorized, remotesessions.ErrFederatedIdentity, "Login provider response is invalid. Restart login", false)
		}
		// Provider errors are untrusted input, not safe browser/log messages.
		// WorkOS error propagation below is intentionally unchanged.
		if q.Get("error") != "" {
			if q.Get("error") == "access_denied" {
				if federation.OfflineRequested && federation.ValidatedUserID != "" && federation.ValidatedIdentity != nil && q.Get("code") == "" {
					optionalRefused = true
				} else {
					return finishFederation(oops.CodeForbidden, nil, "Login was declined", true)
				}
			} else {
				return finishFederation(oops.CodeGatewayError, errors.New("federated provider returned an error"), "Federated login failed. Contact your administrator", false)
			}
		}
	}

	// If the IDP returned an error (user cancelled at the IDP, IDP refused
	// to authenticate, etc.) per OAuth 2.0, forward it back to the MCP
	// client with the same error code so the client can render an
	// appropriate message instead of seeing a generic "state and code are
	// required" 400.
	if idpErr := q.Get("error"); idpErr != "" && !optionalRefused {
		errDescription := q.Get("error_description")
		// First-party challenges have no MCP client to bounce the error back to
		// (no RedirectURI), so surface it directly. Declines are forbidden;
		// anything else is config/IDP trouble.
		if challengeState.FirstParty {
			if idpErr == "access_denied" {
				s.metrics.RecordOAuthFlowDeclined(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
				logger.InfoContext(ctx, "oauth flow declined at idp", attr.SlogOAuthError(idpErr), attr.SlogOAuthErrorDescription(errDescription))
				return oops.E(oops.CodeForbidden, nil, "login was declined").LogError(ctx, logger)
			}
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
			logger.InfoContext(ctx, "oauth flow failed at idp callback", attr.SlogOAuthError(idpErr), attr.SlogOAuthErrorDescription(errDescription))
			return oops.E(oops.CodeUnexpected, nil, "idp returned an error: %s", idpErr).LogError(ctx, logger)
		}
		issuer, err := s.issuerURL(endpoint, baseURL)
		if err != nil {
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
			return oops.E(oops.CodeUnexpected, err, "build authorization response issuer").LogError(ctx, logger)
		}
		clientRedirect, err := buildClientRedirect(clientRedirectParams{
			RedirectURI:      challengeState.RedirectURI,
			Issuer:           issuer,
			Code:             "",
			State:            challengeState.State,
			ErrorCode:        idpErr,
			ErrorDescription: errDescription,
		})
		if err != nil {
			// Recorded as failed even for access_denied: the IDP error never
			// reached the client, so this flow ended on a fault. Exactly one
			// terminal outcome is counted per started flow either way.
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
			return oops.E(oops.CodeUnexpected, err, "build client redirect").LogError(ctx, logger)
		}
		// access_denied is the user opting out at the IDP — a decline, not an
		// errant config. Any other IDP error code (server_error, invalid_scope,
		// ...) points at IDP/config trouble. Both are terminal; bucket them
		// accordingly before bouncing the error back to the client.
		if idpErr == "access_denied" {
			s.metrics.RecordOAuthFlowDeclined(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
			logger.InfoContext(ctx, "oauth flow declined at idp", attr.SlogOAuthError(idpErr), attr.SlogOAuthErrorDescription(errDescription))
		} else {
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
			logger.InfoContext(ctx, "oauth flow failed at idp callback", attr.SlogOAuthError(idpErr), attr.SlogOAuthErrorDescription(errDescription))
		}
		http.Redirect(w, r, clientRedirect, http.StatusFound)
		return nil
	}

	code := q.Get("code")
	if code == "" && !optionalRefused {
		if challengeState.Federation != nil {
			return finishFederation(oops.CodeBadRequest, remotesessions.ErrFederatedIdentity, "Invalid login response", false)
		}
		// IDP returned neither code nor error — a broken IDP redirect.
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
		return oops.E(oops.CodeBadRequest, nil, "code is required").LogError(ctx, logger)
	}

	var federatedIdentity *remotesessions.FederatedIdentity
	var gramUserID string
	impersonated := false
	if federation := challengeState.Federation; federation != nil {
		verified := federation.ValidatedIdentity
		if !optionalRefused {
			verified, err = s.remoteChallengeMgr.ExchangeFederatedCode(ctx, provider, federation.CallbackURL, code, federation.Nonce, federation.Verifier)
			if err != nil {
				return failFederationDependency(err, "Federated identity verification failed. Restart login or contact your administrator")
			}
			defer verified.DiscardCredentials()
		}
		if (federation.OfflineRequested || federation.ExplicitRetry) && (federation.ValidatedIdentity == nil || verified == nil || verified.Issuer != federation.ValidatedIdentity.Issuer || verified.Subject != federation.ValidatedIdentity.Subject) {
			return finishFederation(oops.CodeUnauthorized, remotesessions.ErrFederatedIdentity, "The offline consent account must match the login account", false)
		}
		federatedIdentity = verified
		gramUserID, err = s.resolveFederatedHuman(ctx, endpoint, verified)
		if err != nil {
			var failure *oops.ShareableError
			if errors.As(err, &failure) && failure.Code == oops.CodeForbidden {
				return finishFederation(oops.CodeForbidden, errors.New("federated user is not provisioned"), "Your account is not provisioned for this organization. Contact your administrator", false)
			}
			return failFederationDependency(err, "Identity verification is temporarily unavailable. Restart login")
		}
		if (federation.OfflineRequested || federation.ExplicitRetry) && gramUserID != federation.ValidatedUserID {
			return finishFederation(oops.CodeUnauthorized, remotesessions.ErrFederatedIdentity, "The offline consent account must match the login account", false)
		}
		// Hold only in this stack frame until organization authorization below.
	} else {
		// Preserve WorkOS bootstrap and membership synchronization unchanged.
		idpUser, err := s.identityResolver.ExchangeCodeForTokens(ctx, code)
		if err != nil {
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
			return oops.E(oops.CodeUnauthorized, err, "failed to exchange IDP code").LogError(ctx, logger)
		}
		login, err := s.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{SkipMembershipSync: false})
		if err != nil {
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
			return oops.E(oops.CodeUnexpected, err, "failed to bootstrap user").LogError(ctx, logger)
		}
		gramUserID = login.UserID
		impersonated = idpUser.ImpersonatorEmail() != ""
	}

	// Validate the user belongs to the endpoint's organization before
	// issuing a token. The mcp:connect RBAC policy operates at org level;
	// this is the first gate. Read from the database, not the user-info
	// cache, so the rows the bootstrap just reconciled are what gets judged.
	// The user wanted in but policy refused — a config-relevant failure
	// (e.g. the toolset is exposed to the wrong audience), not a user decline.
	member, err := s.identityResolver.IsOrganizationMember(ctx, endpoint.OrganizationID, gramUserID)
	if err != nil {
		if challengeState.Federation != nil {
			return failFederationDependency(err, "Organization membership verification is temporarily unavailable. Restart login")
		}
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
		return oops.E(oops.CodeUnexpected, err, "failed to check organization membership").LogError(ctx, logger)
	}
	if !member {
		if challengeState.Federation != nil {
			return finishFederation(oops.CodeForbidden, errors.New("federated organization membership denied"), "Your account is not provisioned for this organization. Contact your administrator", false)
		}
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
		return oops.E(oops.CodeForbidden, nil, "user is not a member of this MCP server's organization").LogError(ctx, logger)
	}

	if federation := challengeState.Federation; federation != nil {
		// A provider call may outlive a rotation or administrative unlink. Check
		// again before any credential handoff or consent state is produced.
		current, issuerID, clientID, version, err := s.federatedProvider(ctx, endpoint)
		if err != nil {
			return failFederationDependency(err, "Login configuration is unavailable. Restart login")
		}
		if current == nil || issuerID != federation.IssuerID || clientID != federation.ClientID || version != federation.Configuration {
			return finishFederation(oops.CodeFailedPrecondition, remotesessions.ErrFederatedConfiguration, "Login configuration changed. Restart login", false)
		}
		if s.federatedLoginConsumer != nil {
			policy, err := current.OfflinePolicy()
			if err != nil {
				return failFederationDependency(err, "Login configuration is unavailable. Restart login")
			}
			if federation.OfflineRequested && federation.ConfigurationHash != policy.ConfigurationHash {
				return finishFederation(oops.CodeFailedPrecondition, remotesessions.ErrFederatedConfiguration, "Login configuration changed. Restart login", false)
			}

			handedIdentity := federatedIdentity
			if optionalRefused {
				handedIdentity = nil
			}
			err = s.federatedLoginConsumer.ConsumeFederatedLogin(ctx, AuthorizedFederatedLogin{
				OrganizationID: endpoint.OrganizationID, UserID: gramUserID, UserSessionIssuerID: challengeState.UserSessionIssuerID,
				TrustedIssuerID: federation.IssuerID, TrustedClientID: federation.ClientID, Identity: handedIdentity,
				Provider: current, ConfigurationHash: policy.ConfigurationHash, OfflineRequested: federation.OfflineRequested, OptionalRefused: optionalRefused,
			})
			if err != nil {
				return finishFederation(oops.CodeUnavailable, errors.New("federated credential handoff failed"), "Login credential handoff failed. Restart login", false)
			}
			// Observe policy after retention: an unsolicited refresh credential from
			// minimal login can make the optional consent request unnecessary.
			requestOffline := false
			if lookup, ok := s.federatedLoginConsumer.(FederatedOfflinePolicy); ok && policy.Enabled && !federation.OfflineRequested {
				requestOffline, err = lookup.ShouldRequestFederatedOffline(ctx, FederatedOfflineRequest{
					Provider: current, OrganizationID: endpoint.OrganizationID, UserID: gramUserID,
					TrustedIssuerID: federation.IssuerID, TrustedClientID: federation.ClientID,
					ConfigurationHash: policy.ConfigurationHash, ExplicitRetry: federation.ExplicitRetry,
				})
				if err != nil {
					// Optional policy storage must not invalidate the retained base login.
					// An unknown policy never authorizes an additional consent prompt.
					requestOffline = false
				}
			}
			if requestOffline {
				// The first login is already retained. Cache identity only, never tokens,
				// and allow exactly one more authorization round trip for this challenge.
				federation.ValidatedUserID = gramUserID
				federation.ValidatedIdentity = &remotesessions.FederatedIdentity{ExpiresAt: time.Time{}, Nonce: "", Issuer: federatedIdentity.Issuer, Subject: federatedIdentity.Subject, Email: federatedIdentity.Email, EmailVerified: federatedIdentity.EmailVerified}
				federation.OfflineRequested = true
				federation.ConfigurationHash = policy.ConfigurationHash
				federation.Nonce, err = generateOpaqueToken()
				if err != nil {
					return failFederationDependency(err, "Login is unavailable. Restart login")
				}
				federation.Verifier, err = generateOpaqueToken()
				if err != nil {
					return failFederationDependency(err, "Login is unavailable. Restart login")
				}
				return s.startFederatedLogin(w, r, &challengeState, current)
			}
		}
	}

	// Mint a fresh state ID so the /connect URL we redirect to is NOT the
	// same value that just bounced through the IDP. The IDP-returned state
	// is consumed; the new ID is what /connect's GetAndDelete will burn.
	subject := urn.NewUserSubject(gramUserID)
	// Rotate only the cache-key ID (replay protection). challengeState.FlowID
	// is deliberately left untouched so the flow stays correlatable across the
	// rotation — do not regenerate it here.
	challengeState.ID = uuid.NewString()
	challengeState.Subject = &subject
	challengeState.AuthorizerUserID = gramUserID
	if federation := challengeState.Federation; federation != nil {
		challengeState.FederatedBinding = &FederatedConsentBinding{IssuerID: federation.IssuerID, ClientID: federation.ClientID, Issuer: federatedIdentity.Issuer, Subject: federatedIdentity.Subject}
	}
	challengeState.Federation = nil // Drop nonce and PKCE; Browser must survive consent and remote linking.
	challengeState.AuthorizerImpersonated = &impersonated
	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
		return oops.E(oops.CodeUnexpected, err, "failed to update authn challenge state").LogError(ctx, logger)
	}

	// The mint-time origin puts the consent page back on the host the user
	// started on, without a fresh custom_domains lookup, or on the
	// authentication host when the endpoint's issuer lives there.
	consentURL, err := endpoint.ConsentURL(s.authorizationServerBaseURL(endpoint, baseURL), challengeState.ID)
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageIDPCallback)
		return oops.E(oops.CodeUnexpected, err, "build consent URL").LogError(ctx, logger)
	}
	http.Redirect(w, r, consentURL, http.StatusFound)
	return nil
}
