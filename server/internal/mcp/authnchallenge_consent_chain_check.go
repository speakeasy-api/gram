package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
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
	chainCheckConnectedResult = consentChainCheckResult{Status: chainCheckConnected, Message: "Connected through your identity provider", Action: ""}
	chainCheckUnknownResult   = consentChainCheckResult{Status: chainCheckUnknown, Message: "Managed by your identity provider", Action: ""}
)

// unmanagedAgainMessage refuses the account a person switched to.
const unmanagedAgainMessage = "The account you just signed in with is also personal, so Speakeasy can't sign you in to %s. Accounts you created yourself with your work email count as personal."

// consentChainCheckTimeout bounds the page's background exchange; a slower
// provider reads as unknown and the first tool call retries.
const consentChainCheckTimeout = 5 * time.Second

// consentChainCheckResult is the check's response body. Message is user-safe
// copy; provider errors and tokens never reach it.
type consentChainCheckResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	// Action names the consent action a rejected card offers besides the own-account sign-in.
	Action string `json:"action,omitempty"`
}

// consentSessionRouting loads the subject's connection state and the routing
// that decides which grants still route. Statuses are empty before the subject
// is stamped, so every card then reads not connected.
func (s *Service) consentSessionRouting(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, clients []remotesessions.Client, typed map[uuid.UUID]uuid.UUID) (map[uuid.UUID]remotesessions.RemoteSessionState, consentRouting, error) {
	routing := consentRouting{backend: consentBackendNone, upstream: endpoint.UpstreamResource, members: nil, hostedClients: nil}
	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return nil, routing, nil
	}
	hosted, err := s.hostedConsentRegistrations(ctx, endpoint, challengeState)
	if err != nil {
		return nil, routing, err
	}
	var statuses map[uuid.UUID]remotesessions.RemoteSessionState
	if !directProxiedConsent(endpoint) && slices.ContainsFunc(clients, func(client remotesessions.Client) bool {
		_, offered := typed[client.ID]
		_, assigned := hosted[client.ID]
		return !offered && !assigned
	}) {
		statuses, err = s.endpointRemoteSessionStatuses(ctx, *challengeState.Subject, endpoint)
		if err != nil {
			return nil, routing, fmt.Errorf("remote session statuses: %w", err)
		}
	}
	exact := make(map[uuid.UUID]uuid.UUID, len(hosted)+len(typed))
	maps.Copy(exact, hosted)
	maps.Copy(exact, typed)
	statuses, err = s.withTypedRegistrationStatuses(ctx, endpoint.OrganizationID, *challengeState.Subject, statuses, exact)
	if err != nil {
		return nil, routing, err
	}
	if len(statuses) > 0 {
		routing, err = s.resolveConsentRouting(ctx, endpoint, challengeState, clients, statuses, hosted)
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

// chainedConsentClient resolves a posted client_id to a card this consent
// renders chained for its own subject, with the requests that card relies on.
func (s *Service) chainedConsentClient(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, rawClientID string) (*remotesessions.Client, []identitychaining.Request, map[identitychaining.Request][]uuid.UUID, error) {
	clientID, err := uuid.Parse(rawClientID)
	if err != nil {
		return nil, nil, nil, oops.E(oops.CodeBadRequest, err, "invalid client_id").LogError(ctx, logger)
	}
	bound, typed, err := s.listEndpointClientsAndRegistrations(ctx, endpoint, challengeState)
	if err != nil {
		return nil, nil, nil, oops.E(oops.CodeUnexpected, err, "list remote session clients").LogError(ctx, logger)
	}
	clients := s.remoteChallengeMgr.WithCatalogBranding(ctx, subjectConnectedClients(bound))
	client := findConsentClient(clients, clientID)
	if client == nil {
		return nil, nil, nil, oops.E(oops.CodeBadRequest, nil, "unknown remote session client for this MCP server").LogError(ctx, logger)
	}
	statuses, routing, err := s.consentSessionRouting(ctx, endpoint, challengeState, clients, typed)
	if err != nil {
		return nil, nil, nil, oops.E(oops.CodeUnexpected, err, "resolve consent routing").LogError(ctx, logger)
	}
	if state, ok := statuses[client.ID]; ok && state.Status == remotesessions.RemoteSessionActive {
		return nil, nil, nil, oops.E(oops.CodeBadRequest, nil, "service is not managed by identity chaining for this consent").LogError(ctx, logger)
	}
	chained, members := s.consentChainingRequests(ctx, endpoint, challengeState, clients, routing)
	requests := chained[client.ID]
	if len(requests) == 0 {
		return nil, nil, nil, oops.E(oops.CodeBadRequest, nil, "service is not managed by identity chaining for this consent").LogError(ctx, logger)
	}
	return client, requests, members, nil
}

// serveIdentityChainingCheck runs the exchange a chained card relies on, so
// the page can report it before the first tool call. client_id only selects
// among the cards this consent renders chained for its own subject.
func (s *Service) serveIdentityChainingCheck(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger).With(attr.SlogOAuthFlowID(challengeState.FlowID))

	client, requests, requestMembers, err := s.chainedConsentClient(ctx, logger, endpoint, challengeState, r.PostForm.Get("client_id"))
	if err != nil {
		return err
	}
	clientID := client.ID

	display := s.serviceDisplayName(ctx, logger, s.newResourceDisplayOwner(endpoint, challengeState), *client)
	signInID := uuid.Nil
	if challengeState.SignInID != nil {
		signInID = *challengeState.SignInID
	}
	result, outcome, decidedBy := s.checkIdentityChaining(ctx, requests, display, signInID)
	switchEnabled := result.Status == chainCheckRejected && s.accountSwitchEnabled(ctx, endpoint.OrganizationID)
	// Off, refusals keep the key they had before the flag.
	refusedSignIn := uuid.Nil
	if switchEnabled {
		refusedSignIn = signInID
	}
	if switchEnabled && outcome.Reason == identitychaining.ReasonUnmanagedAccount {
		if challengeState.AccountSwitches > 0 {
			result.Message = fmt.Sprintf(unmanagedAgainMessage, display)
		}
		offer, oerr := s.accountSwitchOffered(r, endpoint, challengeState, time.Now())
		if oerr != nil {
			logger.WarnContext(ctx, "check account switch offer", attr.SlogError(oerr))
		}
		if offer == accountSwitchOK {
			result.Action = consentActionSwitchAccount
		}
	}
	logger.InfoContext(ctx, "identity chaining consent check",
		attr.SlogOutcome(result.Status),
		attr.SlogReason(string(outcome.Reason)),
		attr.SlogIdentityChainingProvider(string(outcome.Provider)),
	)

	if step, code, refused := chainingRefusalCode(outcome); refused && result.Status == chainCheckRejected {
		attempt := challengeState.connectionAttempt(s.aiCatalog.SnapshotFor(ctx), endpoint)
		opts := []mcpconnectionattempts.Option{
			mcpconnectionattempts.FromRequest(r), mcpconnectionattempts.Subject(challengeState.Subject),
			mcpconnectionattempts.Provider(client.IssuerURL), mcpconnectionattempts.RemoteSessionClient(client.ID),
		}
		// A gateway's refusal belongs to each member the refused request serves.
		s.recordRefusedConsents(ctx, logger, attempt, client.ID, refusedSignIn, requestMembers[decidedBy], step, code, opts...)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode identity chaining check").LogError(ctx, logger)
	}
	if challengeState.FirstParty && result.Status == chainCheckConnected {
		s.endChainedFirstParty(ctx, logger, mcpconnectionattempts.FromRequest(r), endpoint, challengeState, clientID)
	}
	return nil
}

// consentChainedUpstreamToken exchanges the sign-in under the consent discovery gate.
func (s *Service) consentChainedUpstreamToken(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, clientID uuid.UUID) *remotesessions.UpstreamToken {
	if s.identityChainer == nil {
		return nil
	}
	bound, typed, err := s.listEndpointClientsAndRegistrations(ctx, endpoint, challengeState)
	if err != nil {
		logger.WarnContext(ctx, "list clients for consent identity chaining", attr.SlogError(err))
		return nil
	}
	clients := subjectConnectedClients(bound)
	_, routing, err := s.consentSessionRouting(ctx, endpoint, challengeState, clients, typed)
	if err != nil {
		logger.WarnContext(ctx, "resolve routing for consent identity chaining", attr.SlogError(err))
		return nil
	}
	chained, _ := s.consentChainingRequests(ctx, endpoint, challengeState, clients, routing)
	var requests []identitychaining.Request
	switch {
	case clientID != uuid.Nil:
		requests = chained[clientID]
	case len(chained) == 1:
		for _, r := range chained {
			requests = r
		}
	}
	if len(requests) != 1 {
		return nil
	}
	sel := requests[0]
	req, ok := s.identityChainingRequestForKind(ctx, mcpidentity.KindConsentDiscovery, endpoint.OrganizationID, endpoint.ProjectID, endpoint.UserSessionIssuerID, sel.UpstreamResource, sel.RemoteSessionIssuerID.Valid, sel.RemoteSessionIssuerID, sel.RemoteSessionClientID)
	if !ok || req.UserID != sel.UserID {
		return nil
	}
	signInID := uuid.Nil
	if challengeState.SignInID != nil {
		signInID = *challengeState.SignInID
	}
	acquireCtx, cancel := context.WithTimeout(ctx, consentChainCheckTimeout)
	defer cancel()
	token, outcome := s.identityChainer.AcquireConsent(acquireCtx, req, signInID)
	if !outcome.Succeeded() {
		logger.InfoContext(ctx, "consent tool inventory could not use identity chaining",
			attr.SlogReason(string(outcome.Reason)),
			attr.SlogIdentityChainingProvider(string(outcome.Provider)),
		)
		return nil
	}
	upstream := chainedUpstreamToken(token)
	return &upstream
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
func (s *Service) checkIdentityChaining(ctx context.Context, requests []identitychaining.Request, service string, signInID uuid.UUID) (consentChainCheckResult, identitychaining.Outcome, identitychaining.Request) {
	ctx, cancel := context.WithTimeout(ctx, consentChainCheckTimeout)
	defer cancel()
	result := chainCheckConnectedResult
	var decided identitychaining.Outcome
	var decidedBy identitychaining.Request
	for _, req := range requests {
		outcome := s.identityChainer.CheckConsent(ctx, req, signInID)
		checked := classifyChainCheck(outcome, service)
		switch checked.Status {
		case chainCheckRejected:
			return checked, outcome, req
		case chainCheckUnknown:
			result, decided, decidedBy = checked, outcome, req
		case chainCheckConnected:
			if result.Status == chainCheckConnected {
				decided, decidedBy = outcome, req
			}
		}
	}
	return result, decided, decidedBy
}

// serviceDisplayName is the service a chained card names in its refusals.
func (s *Service) serviceDisplayName(ctx context.Context, logger *slog.Logger, owner *resourceDisplayOwner, client remotesessions.Client) string {
	display, _ := issuerCardBranding(client, owner.ownsOrFallsBack(ctx, logger, client), s.serverURL)
	return display
}

// classifyChainCheck maps an outcome to page copy from chainingVerdicts; only definitive failures read as rejected.
func classifyChainCheck(outcome identitychaining.Outcome, service string) consentChainCheckResult {
	if outcome.Succeeded() && !outcome.Retryable {
		return chainCheckConnectedResult
	}
	verdict, ok := chainingVerdictOf(outcome)
	if !ok || verdict.message == nil {
		return chainCheckUnknownResult
	}
	return consentChainCheckResult{Status: chainCheckRejected, Message: verdict.message.text(service), Action: ""}
}
