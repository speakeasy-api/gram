package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
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
	routing := consentRouting{backend: consentBackendNone, upstream: endpoint.UpstreamResource, issuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, members: nil, grants: nil}
	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return nil, routing, nil
	}
	statuses, err := s.remoteChallengeMgr.RemoteSessionStatuses(ctx, *challengeState.Subject, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	if err != nil {
		return nil, routing, fmt.Errorf("remote session statuses: %w", err)
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
	bound, err := s.remoteChallengeMgr.ListClients(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
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

	owner := s.newResourceDisplayOwner(endpoint)
	display, _ := issuerCardBranding(*client, owner.ownsOrFallsBack(ctx, logger, *client), s.serverURL)
	result := s.checkIdentityChaining(ctx, requests, display)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode identity chaining check").LogError(ctx, logger)
	}
	return nil
}

// checkIdentityChaining acquires every request under one deadline. A
// rejection anywhere decides the card; any other failure leaves it unknown.
// Success publishes the credential, so the first tool call reuses it.
func (s *Service) checkIdentityChaining(ctx context.Context, requests []identitychaining.Request, service string) consentChainCheckResult {
	ctx, cancel := context.WithTimeout(ctx, consentChainCheckTimeout)
	defer cancel()
	result := chainCheckConnectedResult
	for _, req := range requests {
		_, outcome := s.identityChainer.Acquire(ctx, req)
		checked := classifyChainCheck(outcome, service)
		switch checked.Status {
		case chainCheckRejected:
			return checked
		case chainCheckUnknown:
			result = checked
		}
	}
	return result
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
		return rejected("Your identity provider sign-in for %s has expired. Sign in again, or use a separate sign-in.")
	case identitychaining.ReasonConfigurationRequired:
		return rejected("%s isn't set up for identity provider sign-in yet. Ask your administrator, or use a separate sign-in.")
	case identitychaining.ReasonNotApplicable, identitychaining.ReasonBindingNotReady, identitychaining.ReasonMalformedAssertion,
		identitychaining.ReasonMalformedResponse, identitychaining.ReasonTransientFailure, identitychaining.ReasonUnknownRejection,
		identitychaining.ReasonStaleConfiguration:
	}
	return chainCheckUnknownResult
}
