package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Chained card check states, shared by the template and the page script.
const (
	chainCheckPending   = "pending"
	chainCheckConnected = "connected"
	chainCheckRejected  = "rejected"
	chainCheckUnknown   = "unknown"
)

var (
	chainCheckConnectedResult = consentChainCheckResult{Status: chainCheckConnected, Message: "Connected through your identity provider"}
	chainCheckUnknownResult   = consentChainCheckResult{Status: chainCheckUnknown, Message: "Managed by your identity provider"}
)

// consentChainCheckTimeout bounds the page's background exchange; a slower
// provider reads as unknown and the first tool call retries.
const consentChainCheckTimeout = 5 * time.Second

// consentChainCheckResult is the check's response body. Message is user-safe
// copy; provider errors and tokens never reach it.
type consentChainCheckResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// consentSessionRouting loads the subject's connection state and the routing
// that decides which grants still route. Statuses are empty before the subject
// is stamped, so every card then reads not connected.
func (s *Service) consentSessionRouting(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, clients []remotesessions.Client) (map[uuid.UUID]remotesessions.RemoteSessionState, consentRouting, error) {
	routing := consentRouting{backend: consentBackendNone, upstream: endpoint.UpstreamResource, members: nil}
	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return nil, routing, nil
	}
	statuses, err := s.endpointRemoteSessionStatuses(ctx, *challengeState.Subject, endpoint)
	if err != nil {
		return nil, routing, fmt.Errorf("remote session statuses: %w", err)
	}
	typed, err := s.typedConsentRegistrations(ctx, endpoint, challengeState)
	if err != nil {
		return nil, routing, err
	}
	statuses, err = s.withTypedRegistrationStatuses(ctx, endpoint.OrganizationID, *challengeState.Subject, statuses, typed)
	if err != nil {
		return nil, routing, err
	}
	if len(statuses) > 0 {
		routing, err = s.resolveConsentRouting(ctx, endpoint, challengeState, clients, statuses)
		if err != nil {
			return nil, routing, err
		}
	}
	return statuses, routing, nil
}

// consentChainCheckAtRender reads stored credentials only: a chained card
// renders connected when every request already holds a usable one.
func (s *Service) consentChainCheckAtRender(ctx context.Context, requests []identitychaining.Request) string {
	for _, req := range requests {
		if !s.identityChainer.HasUsableCredential(ctx, req) {
			return chainCheckPending
		}
	}
	return chainCheckConnected
}

// serveIdentityChainingCheck runs the exchange a chained card relies on, so
// the page can report it before the first tool call. client_id only selects
// among the cards this consent renders chained for its own subject.
func (s *Service) serveIdentityChainingCheck(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger).With(attr.SlogOAuthFlowID(challengeState.FlowID))

	clientID, err := uuid.Parse(r.PostForm.Get("client_id"))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid client_id").LogError(ctx, logger)
	}
	bound, err := s.listEndpointClients(ctx, endpoint, challengeState)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list remote session clients").LogError(ctx, logger)
	}
	clients := s.remoteChallengeMgr.WithCatalogBranding(ctx, subjectConnectedClients(bound))
	client := findConsentClient(clients, clientID)
	if client == nil {
		return oops.E(oops.CodeBadRequest, nil, "unknown remote session client for this MCP server").LogError(ctx, logger)
	}
	statuses, routing, err := s.consentSessionRouting(ctx, endpoint, challengeState, clients)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve consent routing").LogError(ctx, logger)
	}
	if state, ok := statuses[client.ID]; ok && state.Status == remotesessions.RemoteSessionActive {
		return oops.E(oops.CodeBadRequest, nil, "service is not managed by identity chaining for this consent").LogError(ctx, logger)
	}
	requests := s.consentChainingRequests(ctx, endpoint, challengeState, clients, routing)[client.ID]
	if len(requests) == 0 {
		return oops.E(oops.CodeBadRequest, nil, "service is not managed by identity chaining for this consent").LogError(ctx, logger)
	}

	owner := s.newResourceDisplayOwner(endpoint, challengeState)
	display, _ := issuerCardBranding(*client, owner.ownsOrFallsBack(ctx, logger, *client), s.serverURL)
	signInID := uuid.Nil
	if challengeState.SignInID != nil {
		signInID = *challengeState.SignInID
	}
	result, outcome := s.checkIdentityChaining(ctx, requests, display, signInID)
	logger.InfoContext(ctx, "identity chaining consent check",
		attr.SlogOutcome(result.Status),
		attr.SlogReason(string(outcome.Reason)),
		attr.SlogIdentityChainingProvider(string(outcome.Provider)),
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode identity chaining check").LogError(ctx, logger)
	}
	return nil
}

// retireDeniedConsentCredentials erases the vendor credentials this sign-in's
// consent pages stored that no grant adopted, as of the denial. An exchange
// already in flight can still store one afterwards; only another eligible
// grant of the same sign-in could adopt it, and it expires within the hour.
// Retirement also clears unadopted credentials from other in-flight consent
// flows for this issuer, subject and sign-in. Those flows re-exchange on retry;
// credentials already adopted by a session remain usable.
func (s *Service) retireDeniedConsentCredentials(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) {
	subject := challengeState.Subject
	if s.identityChainer == nil || challengeState.SignInID == nil || subject == nil || subject.Kind != urn.SessionSubjectKindUser || subject.ID == "" {
		return
	}
	if err := s.identityChainer.RetireConsentCredentials(ctx, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject.ID, *challengeState.SignInID); err != nil {
		logger.WarnContext(ctx, "retire denied consent identity chaining credentials", attr.SlogError(err))
	}
}

// checkIdentityChaining checks every request under one deadline. Every
// provider runs its live exchange, so a card reads connected only after the
// vendor accepted one: a stored downstream token or, when the credential slot
// is held, a vendor acceptance cached for a few minutes. It also returns the
// outcome that decided the result.
func (s *Service) checkIdentityChaining(ctx context.Context, requests []identitychaining.Request, service string, signInID uuid.UUID) (consentChainCheckResult, identitychaining.Outcome) {
	ctx, cancel := context.WithTimeout(ctx, consentChainCheckTimeout)
	defer cancel()
	result := chainCheckConnectedResult
	var decided identitychaining.Outcome
	for _, req := range requests {
		outcome := s.identityChainer.CheckConsent(ctx, req, signInID)
		checked := classifyChainCheck(outcome, service)
		switch checked.Status {
		case chainCheckRejected:
			return checked, outcome
		case chainCheckUnknown:
			result, decided = checked, outcome
		case chainCheckConnected:
			if result.Status == chainCheckConnected {
				decided = outcome
			}
		}
	}
	return result, decided
}

// classifyChainCheck maps an outcome to page copy. Only definitive refusals
// read as rejected; transient, ambiguous and stale outcomes stay unknown.
func classifyChainCheck(outcome identitychaining.Outcome, service string) consentChainCheckResult {
	if outcome.Retryable {
		return chainCheckUnknownResult
	}
	rejected := func(format string) consentChainCheckResult {
		return consentChainCheckResult{Status: chainCheckRejected, Message: fmt.Sprintf(format, service)}
	}
	switch outcome.Reason {
	case identitychaining.ReasonSuccess:
		return chainCheckConnectedResult
	case identitychaining.ReasonInvalidGrant, identitychaining.ReasonAccessDenied, identitychaining.ReasonScopePolicyDenied,
		identitychaining.ReasonInvalidClient, identitychaining.ReasonInvalidTarget, identitychaining.ReasonInsufficientScope:
		switch outcome.Stage {
		case identitychaining.StageExchange, identitychaining.StageValidation:
			return rejected("Your identity provider didn't allow access to %s. Ask your administrator, or use a separate sign-in.")
		case identitychaining.StageSelection, identitychaining.StageAuthorization, identitychaining.StageDelegation,
			identitychaining.StageRedemption, identitychaining.StagePersistence, identitychaining.StageComplete:
			return rejected("%s didn't accept your identity provider sign-in. Ask your administrator, or use a separate sign-in.")
		}
	case identitychaining.ReasonReauthenticationRequired:
		return rejected("A current identity provider sign-in for %s is required. Sign in again, or use a separate sign-in.")
	case identitychaining.ReasonConfigurationRequired:
		return rejected("%s isn't set up for identity provider sign-in yet. Ask your administrator, or use a separate sign-in.")
	case identitychaining.ReasonIneligibleIdentity:
		return rejected("Your organization can't vouch for your account to %s, for example because your email isn't on one of its verified domains. Ask your administrator, or use a separate sign-in.")
	case identitychaining.ReasonNotApplicable, identitychaining.ReasonBindingNotReady, identitychaining.ReasonMalformedAssertion,
		identitychaining.ReasonMalformedResponse, identitychaining.ReasonTransientFailure, identitychaining.ReasonUnknownRejection,
		identitychaining.ReasonStaleConfiguration, identitychaining.ReasonProviderUnavailable:
	}
	return chainCheckUnknownResult
}
