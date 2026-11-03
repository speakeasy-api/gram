// What sign-in steps record on their connection attempt. None of this changes a response.

package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/privatekeyjwt"
)

// connectionAttemptEndTTL outlives the longest a browser attempt can stay live.
const connectionAttemptEndTTL = 2 * time.Hour

const (
	// attemptEndingTimeout bounds one claim of an attempt's ending.
	attemptEndingTimeout = 5 * time.Second

	// maxPendingAttemptEndings bounds claims waiting on a slow Redis.
	maxPendingAttemptEndings = 256
)

// federatedHappened names a failure of the organization's own sign-in service.
func federatedHappened(err error) mcpconnectionattempts.Code {
	switch {
	case errors.Is(err, remotesessions.ErrFederatedConfiguration):
		return mcpconnectionattempts.CodeConnectionChanged
	case errors.Is(err, remotesessions.ErrFederatedSigning):
		return mcpconnectionattempts.CodeInternalError
	case errors.Is(err, remotesessions.ErrFederatedIdentity):
		return mcpconnectionattempts.CodeSignInProviderError
	default:
		return mcpconnectionattempts.CodeIDPTransientFailure
	}
}

// recoveryScope is the server a recovery link connects.
func recoveryScope(recovery connectionRecovery) mcpconnectionattempts.Scope {
	return mcpconnectionattempts.ServerScope(recovery.OrganizationID, recovery.ProjectID, recovery.McpServerID, mcpconnectionattempts.AppRecoveryLink)
}

func recoveryHappened(err error) mcpconnectionattempts.Code {
	switch {
	case errors.Is(err, errRecoveryNoAccess):
		return mcpconnectionattempts.CodeNoServerAccess
	case errors.Is(err, errRecoveryUnavailable):
		return mcpconnectionattempts.CodeConnectionChanged
	case errors.Is(err, errRecoveryOtherUser):
		return mcpconnectionattempts.CodeSessionChanged
	default:
		return mcpconnectionattempts.CodeInternalError
	}
}

// refusedWithoutRow is a refusal with no proof of a real flow: a metric, never a row.
func (s *Service) refusedWithoutRow(ctx context.Context, t *mcpconnectionattempts.Tracker, reason string) {
	t.Continue()
	s.metrics.RecordConnectionRefusalWithoutRow(ctx, reason)
}

// recordWrongConsentBrowser leaves the page usable for the right browser. The
// endpoint comes from the route, so the state must belong to it before writing.
func (s *Service) recordWrongConsentBrowser(r *http.Request, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) {
	ctx := r.Context()
	fromRequest := mcpconnectionattempts.FromRequest(r)
	if endpoint.validateChallengeRef(challengeState.Endpoint, challengeState.UserSessionIssuerID) != nil {
		return
	}
	attempt := challengeState.connectionAttempt(endpoint)
	if challengeState.CodeReconnect.expired(time.Now()) {
		s.endAttemptOnce(ctx, s.logger, attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeExpired, fromRequest, mcpconnectionattempts.Subject(challengeState.Subject))
		return
	}
	s.connections.Step(ctx, attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeSessionChanged, fromRequest, mcpconnectionattempts.Subject(challengeState.Subject))
}

// reconnectFailure maps a failed catalogue reconnect, and whether a retry can't fix it.
func reconnectFailure(err error) (mcpconnectionattempts.Code, mcpconnectionattempts.Option, bool) {
	switch {
	case errors.Is(err, errReconnectCatalogSuperseded):
		return mcpconnectionattempts.CodeConnectionCatalogExpired, mcpconnectionattempts.Detail("another_approval"), true
	case errors.Is(err, errConnectionCatalogExpired), errors.Is(err, redisCache.ErrCacheMiss):
		return mcpconnectionattempts.CodeConnectionCatalogExpired, mcpconnectionattempts.Detail(""), true
	case errors.Is(err, errReconnectConnectionInactive):
		return mcpconnectionattempts.CodeRefreshTokenRevoked, mcpconnectionattempts.Detail("revoked_during_review"), true
	case errors.Is(err, errReconnectCatalogChanged), errors.Is(err, errReconnectCatalogNotFrozen), errors.Is(err, errConsentConfigurationChanged):
		return mcpconnectionattempts.CodeConnectionChanged, mcpconnectionattempts.Detail("catalog_setup"), true
	case errors.Is(err, errConnectionCatalogTooLarge):
		return mcpconnectionattempts.CodeMemberToolsMissing, mcpconnectionattempts.Detail("catalog_too_large"), false
	default:
		return mcpconnectionattempts.CodeInternalError, mcpconnectionattempts.Detail(""), false
	}
}

// refuseReconnect ends the attempt once when a retry can't help. The review page
// stays reachable, so this can repeat.
func (s *Service) refuseReconnect(r *http.Request, logger *slog.Logger, t *mcpconnectionattempts.Tracker, attempt mcpconnectionattempts.Attempt, err error) {
	ctx := r.Context()
	code, detail, terminal := reconnectFailure(err)
	if !terminal {
		t.Attach(attempt)
		t.Refuse(ctx, code, detail)
		return
	}
	s.endAttemptOnce(ctx, logger, attempt, mcpconnectionattempts.StepInboundConsent, code, detail, mcpconnectionattempts.FromRequest(r))
	t.Continue()
}

// recordTicketExpired is a live connection ticket past its time, on the endpoint it was minted for.
func (s *Service) recordTicketExpired(r *http.Request, endpoint *ResolvedMcpEndpoint, ticket AuthnChallengeState, app mcpconnectionattempts.App) {
	if endpoint.validateChallengeRef(ticket.Endpoint, ticket.UserSessionIssuerID) != nil {
		return
	}
	s.connections.Once(r.Context(), connectionScope(endpoint, app), mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeExpired, mcpconnectionattempts.FromRequest(r))
}

// accessHappened is no_server_access only when access was truly refused.
func accessHappened(err error) mcpconnectionattempts.Code {
	if errors.Is(err, errConsentAgentDenied) {
		return mcpconnectionattempts.CodeNoServerAccess
	}
	if denied, ok := errors.AsType[*oops.ShareableError](err); ok && denied.Code == oops.CodeForbidden {
		return mcpconnectionattempts.CodeNoServerAccess
	}
	return mcpconnectionattempts.CodeInternalError
}

// noReadyServerHappened tells a first connect waiting on sign-ins from a real gateway problem.
func noReadyServerHappened(gateway *gatewayConsentView) mcpconnectionattempts.Code {
	for _, server := range gateway.Servers {
		switch server.Status {
		case gatewayServerNotConnected, gatewayServerReconnect, gatewayServerChainPending:
		case gatewayServerUnavailable, gatewayServerNoAccess, gatewayServerNotSetUp, gatewayServerNoSignIn, gatewayServerConnected, gatewayServerChained:
			return mcpconnectionattempts.CodeMemberToolsMissing
		}
	}
	if len(gateway.Servers) == 0 {
		return mcpconnectionattempts.CodeMemberToolsMissing
	}
	return mcpconnectionattempts.CodeNeedsSignIn
}

// recordConsentPage records what a rendered consent page told the person.
func (s *Service) recordConsentPage(
	ctx context.Context,
	logger *slog.Logger,
	fromRequest mcpconnectionattempts.Option,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	gateway *gatewayConsentView,
	cards []remoteSessionCard,
	problem mcpconnectionattempts.Happened,
) {
	attempt := challengeState.connectionAttempt(endpoint)
	subject := mcpconnectionattempts.Subject(challengeState.Subject)
	if gateway != nil {
		for _, server := range gateway.Servers {
			s.recordGatewayServerStatus(ctx, attempt, gateway, server, subject, fromRequest)
		}
	}
	if problem != nil {
		s.connections.Step(ctx, attempt, mcpconnectionattempts.StepInboundConsent, problem, subject, fromRequest)
	}
	if challengeState.FirstParty && !challengeState.CodeReconnect.undecidedOrCanceled() && firstPartyConnected(gateway, cards) {
		s.endAttemptOnce(ctx, logger, attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete, subject, fromRequest)
	}
}

func (s *Service) recordGatewayServerStatus(ctx context.Context, attempt mcpconnectionattempts.Attempt, gateway *gatewayConsentView, server gatewayServer, subject, fromRequest mcpconnectionattempts.Option) {
	serverID, err := uuid.Parse(server.ServerID)
	if err != nil {
		return
	}
	var authClient *authClientResolution
	for i := range gateway.members {
		if gateway.members[i].serverID == serverID {
			authClient = gateway.members[i].authClient
			break
		}
	}
	what, detail := gatewayServerHappened(server.Status, authClient)
	opts := []mcpconnectionattempts.Option{subject, fromRequest, mcpconnectionattempts.Member(serverID), mcpconnectionattempts.Detail(detail)}
	if authClient != nil {
		opts = append(opts, mcpconnectionattempts.AuthClient(authClient.clientID))
	}
	s.connections.Step(ctx, attempt, mcpconnectionattempts.StepDiscoveryMember, what, opts...)
}

// gatewayServerHappened maps a server's consent status, and any gap in its
// connection choice, to what it records. The detail is the status or the gap.
func gatewayServerHappened(status gatewayServerStatus, authClient *authClientResolution) (mcpconnectionattempts.Happened, string) {
	detail := string(status)
	if authClient != nil && (authClient.outcome == authClientGap || authClient.outcome == authClientNoAccess) {
		detail = authClient.reason
	}
	switch status {
	case gatewayServerConnected, gatewayServerChained, gatewayServerNoSignIn:
		return mcpconnectionattempts.ResultAllowed, detail
	case gatewayServerChainPending:
		return mcpconnectionattempts.ResultAccepted, detail
	case gatewayServerReconnect, gatewayServerNotConnected:
		return mcpconnectionattempts.CodeNeedsSignIn, detail
	case gatewayServerNotSetUp:
		return mcpconnectionattempts.CodeUpstreamSignInNotSetUp, detail
	case gatewayServerNoAccess:
		return mcpconnectionattempts.CodeNoServerAccess, detail
	case gatewayServerUnavailable:
		return mcpconnectionattempts.CodeMemberToolsMissing, detail
	}
	return mcpconnectionattempts.CodeMemberToolsMissing, detail
}

// firstPartyConnected reports a first-party page with nothing left to connect.
// It has no /token to end on, so this is its Connected ending.
func firstPartyConnected(gateway *gatewayConsentView, cards []remoteSessionCard) bool {
	switch {
	case gateway == nil:
		return firstPartyCardsDone(cards)
	case gateway.FocusUnavailable:
		return false
	case gateway.Focus != nil:
		if len(cards) == 0 {
			return gateway.Focus.Ready()
		}
		return firstPartyCardsDone(cards)
	}
	visible := gateway.VisibleServers()
	for _, server := range visible {
		if !server.Ready() {
			return false
		}
	}
	return len(visible) > 0
}

func firstPartyCardsDone(cards []remoteSessionCard) bool {
	for _, c := range cards {
		chained := c.Chained && c.ChainCheck == chainCheckConnected
		if !chained && (!c.Connected || c.needsAction()) {
			return false
		}
	}
	return true
}

// endChainedFirstParty ends a first-party attempt once a chained check leaves nothing to do. It never touches the response.
func (s *Service) endChainedFirstParty(ctx context.Context, logger *slog.Logger, fromRequest mcpconnectionattempts.Option, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, checked uuid.UUID) {
	if challengeState.CodeReconnect.undecidedOrCanceled() {
		return
	}
	cards, err := s.buildRemoteSessionCards(ctx, endpoint, challengeState, s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID))
	if err != nil {
		logger.WarnContext(ctx, "read cards to end connection attempt", attr.SlogError(err))
		return
	}
	for i := range cards {
		if cards[i].ClientID == checked.String() {
			cards[i].ChainCheck = chainCheckConnected
		}
	}
	var gateway *gatewayConsentView
	if endpoint.MetaMcpServerID.Valid {
		gateway, cards, err = s.gatewayConsent(ctx, endpoint, challengeState, cards, challengeState.FocusServer)
		if err != nil {
			logger.WarnContext(ctx, "read gateway to end connection attempt", attr.SlogError(err))
			return
		}
	}
	if firstPartyConnected(gateway, cards) {
		s.endAttemptOnce(ctx, logger, challengeState.connectionAttempt(endpoint), mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete, fromRequest, mcpconnectionattempts.Subject(challengeState.Subject))
	}
}

func attemptEndMarker(attempt mcpconnectionattempts.Attempt) string {
	return "connectionAttemptEnded:" + attempt.ID().String()
}

// consumeChallenge spends a sign-in state and reserves its attempt's ending in one
// Redis call, so a provider callback can't read the gap as an expired sign-in.
func (s *Service) consumeChallenge(ctx context.Context, stateID string, attempt mcpconnectionattempts.Attempt) (AuthnChallengeState, error) {
	if attempt.IsZero() {
		return s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+stateID) //nolint:wrapcheck // the typed cache names the key
	}
	return s.authnChallengeCache.GetAndDeleteMarking(ctx, "authnChallenge:"+stateID, attemptEndMarker(attempt), connectionAttemptEndTTL) //nolint:wrapcheck // the typed cache names the key
}

// attemptEndings runs once-only endings off the request path, so a slow Redis can't slow a sign-in.
type attemptEndings struct {
	slots chan struct{}
	wg    sync.WaitGroup
}

func newAttemptEndings() *attemptEndings {
	return &attemptEndings{slots: make(chan struct{}, maxPendingAttemptEndings), wg: sync.WaitGroup{}}
}

// drain waits for endings in flight, or ctx.
func (a *attemptEndings) drain(ctx context.Context) error {
	if a == nil {
		return nil
	}
	drained := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the caller names the drain
	}
}

// endAttemptOnce ends an attempt from a path that can repeat, off the request path.
// If the marker can't be read or too many are waiting, the attempt stays open rather than ending twice.
func (s *Service) endAttemptOnce(ctx context.Context, logger *slog.Logger, attempt mcpconnectionattempts.Attempt, step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, opts ...mcpconnectionattempts.Option) {
	if attempt.IsZero() || s.connections == nil || s.userSessionRefreshReplayCoordination == nil || s.attemptEndings == nil {
		return
	}
	select {
	case s.attemptEndings.slots <- struct{}{}:
	default:
		logger.WarnContext(ctx, "too many connection attempt endings waiting; leaving this one open")
		return
	}
	claimCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), attemptEndingTimeout)
	s.attemptEndings.wg.Go(func() {
		defer cancel()
		defer func() { <-s.attemptEndings.slots }()
		won, err := s.userSessionRefreshReplayCoordination.Add(claimCtx, attemptEndMarker(attempt), connectionAttemptEndTTL)
		if err != nil {
			logger.WarnContext(claimCtx, "claim connection attempt ending", attr.SlogError(err))
			return
		}
		if won {
			s.connections.End(claimCtx, attempt, step, what, opts...)
		}
	})
}

// recordRemoteLoginRefusal records a provider callback refused before its login
// state is spent. Only a parent that is gone ends the attempt.
func (s *Service) recordRemoteLoginRefusal(r *http.Request, remote remotesessions.RemoteLoginState, err error) {
	ctx := r.Context()
	opts := append(remote.ConnectionOptions(), mcpconnectionattempts.FromRequest(r))
	if errors.Is(err, redisCache.ErrCacheMiss) || errors.Is(err, errRecoveryLoginExpired) {
		s.endAttemptOnce(ctx, s.logger, remote.Attempt, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeExpired, opts...)
		return
	}
	s.connections.Step(ctx, remote.Attempt, mcpconnectionattempts.StepProviderSignIn, remoteLoginRefusalHappened(err), opts...)
}

func remoteLoginRefusalHappened(err error) mcpconnectionattempts.Code {
	switch {
	case errors.Is(err, errRecoveryNoAccess):
		return mcpconnectionattempts.CodeNoServerAccess
	case errors.Is(err, errRecoveryUnavailable):
		return mcpconnectionattempts.CodeConnectionChanged
	}
	if failure, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch failure.Code {
		case oops.CodeUnauthorized:
			return mcpconnectionattempts.CodeSessionChanged
		case oops.CodeForbidden:
			return mcpconnectionattempts.CodeNoServerAccess
		default:
			return mcpconnectionattempts.CodeInternalError
		}
	}
	return mcpconnectionattempts.CodeSessionChanged
}

// recordTokenClientRefusal records /token refusing the client of an authorization
// code. A client that never resolved is no proof of a real flow, so it has no row.
func (s *Service) recordTokenClientRefusal(r *http.Request, endpoint *ResolvedMcpEndpoint, grantType, clientID, reason string) {
	if grantType != oauthwire.GrantTypeAuthorizationCode {
		return
	}
	what, recorded := tokenClientRefusalHappened(reason)
	if !recorded || clientID == "" {
		return
	}
	app := mcpconnectionattempts.AppUnverified
	if reason == "ai_tool_blocked" || reason == "cimd_metadata_invalid" {
		app = mcpconnectionattempts.AppOf(clientID)
	}
	s.connections.Once(r.Context(), connectionScope(endpoint, app), mcpconnectionattempts.StepInboundToken, what, mcpconnectionattempts.FromRequest(r), mcpconnectionattempts.Grant(grantType), mcpconnectionattempts.Detail(reason))
}

func tokenClientRefusalHappened(reason string) (mcpconnectionattempts.Code, bool) {
	switch reason {
	case "missing_client_id", "unknown_client_id", clientIDNamesEndpointReason:
		return mcpconnectionattempts.Code{}, false
	case "cimd_metadata_invalid":
		return mcpconnectionattempts.CodeCIMDMetadataInvalid, true
	case "cimd_admission_denied", "cimd_admission_disabled":
		return mcpconnectionattempts.CodeCIMDAdmissionDenied, true
	case "ai_tool_blocked":
		return mcpconnectionattempts.CodeAIToolBlocked, true
	case "assertion_verifier_unavailable", string(privatekeyjwt.ReasonVerifierMisconfigured):
		return mcpconnectionattempts.CodeAssertionVerifierMisconfigured, true
	case string(privatekeyjwt.ReasonReplayStoreUnavailable):
		return mcpconnectionattempts.CodeAssertionCheckUnavailable, true
	case "client_method_unreadable":
		return mcpconnectionattempts.CodeInternalError, true
	}
	if strings.HasPrefix(reason, "assertion_") && reason != "assertion_client_presented_secret" {
		return mcpconnectionattempts.CodeAssertionInvalid, true
	}
	return mcpconnectionattempts.CodeClientAuthFailed, true
}

// recordTokenClientFailure records /token unable to check an authorization code's client.
func (s *Service) recordTokenClientFailure(r *http.Request, endpoint *ResolvedMcpEndpoint, grantType, clientID string, reason mcpmetrics.IDJAGRejectionReason) {
	if grantType != oauthwire.GrantTypeAuthorizationCode {
		return
	}
	what := mcpconnectionattempts.CodeAIToolCheckUnavailable
	if reason == mcpmetrics.IDJAGRejectionClientMetadataUnavailable {
		what = mcpconnectionattempts.CodeCIMDFetchFailed
	}
	s.connections.Once(r.Context(), connectionScope(endpoint, mcpconnectionattempts.AppOf(clientID)), mcpconnectionattempts.StepInboundToken, what, mcpconnectionattempts.FromRequest(r), mcpconnectionattempts.Grant(grantType))
}

// recordRemoteSessionVerdict records the check of a provider sign-in on its
// attempt. The person can reconnect, so it never ends it. A keepalive has no attempt.
func (s *Service) recordRemoteSessionVerdict(
	ctx context.Context,
	fromRequest mcpconnectionattempts.Option,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	target validationTarget,
	remoteSessionID uuid.UUID,
	verdict remotesessions.ValidationOutcome,
	trigger remotesessionmetrics.ValidationTrigger,
) {
	if trigger == remotesessionmetrics.ValidationTriggerKeepalive {
		return
	}
	s.connections.Step(ctx, challengeState.connectionAttempt(endpoint), mcpconnectionattempts.StepProviderSignIn, verdictHappened(verdict, target.tunnelID.Valid),
		fromRequest,
		mcpconnectionattempts.Detail(string(trigger)),
		mcpconnectionattempts.Member(target.memberID),
		mcpconnectionattempts.Provider(client.IssuerURL),
		mcpconnectionattempts.RemoteSession(remoteSessionID),
		mcpconnectionattempts.RemoteSessionClient(client.ID),
		mcpconnectionattempts.RemoteSessionIssuer(client.RemoteSessionIssuerID),
		mcpconnectionattempts.Subject(challengeState.Subject),
	)
}

func verdictHappened(verdict remotesessions.ValidationOutcome, tunneled bool) mcpconnectionattempts.Happened {
	switch verdict {
	case remotesessions.ValidationOutcomeValid:
		return mcpconnectionattempts.ResultAllowed
	case remotesessions.ValidationOutcomeRejectedByMember:
		return mcpconnectionattempts.CodeRejectedByMember
	case remotesessions.ValidationOutcomeInactive:
		return mcpconnectionattempts.CodeProviderRejected
	case remotesessions.ValidationOutcomeUnknown:
	}
	if tunneled {
		return mcpconnectionattempts.CodeTunnelUnavailable
	}
	return mcpconnectionattempts.CodeMemberToolsMissing
}

// recordListingFailures writes one row per member a Direct listing lost. Sign-ins not done yet are not failures.
func (s *Service) recordListingFailures(ctx context.Context, gate *metaGateContext, unavailable []unavailableMember) {
	app := mcpconnectionattempts.AppUnverified
	if clientID, ok := contextvalues.GetOAuthClientID(ctx); ok {
		app = mcpconnectionattempts.AppOf(clientID)
	}
	scope := mcpconnectionattempts.GatewayScope(gate.organizationID, gate.projectID, gate.metaServerID, app)
	if gate.projectID == uuid.Nil {
		scope = mcpconnectionattempts.OrganizationGatewayScope(gate.organizationID, gate.metaServerID, app)
	}
	var subject *urn.SessionSubject
	if gate.authentication != nil {
		subject = &gate.authentication.subject
	}
	for _, failed := range unavailable {
		if what, recorded := listingFailureHappened(failed.err); recorded {
			s.connections.Once(ctx, scope, mcpconnectionattempts.StepDiscoveryMember, what, gate.fromRequest, mcpconnectionattempts.Member(failed.member.serverID), mcpconnectionattempts.Subject(subject))
		}
	}
}

func listingFailureHappened(err error) (mcpconnectionattempts.Code, bool) {
	if _, credential := errors.AsType[memberCredentialError](err); credential {
		return mcpconnectionattempts.Code{}, false
	}
	if denied, ok := errors.AsType[*oops.ShareableError](err); ok && (denied.Code == oops.CodeForbidden || denied.Code == oops.CodeNotFound) {
		return mcpconnectionattempts.CodeNoServerAccess, true
	}
	return mcpconnectionattempts.CodeMemberToolsMissing, true
}
