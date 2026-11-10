// What sign-in steps record on their connection attempt. None of this changes a response.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
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
	// attemptEndingReferenceWait is how long a failure response waits for its ending to be accepted.
	attemptEndingReferenceWait = 200 * time.Millisecond

	// attemptEndingTimeout bounds one claim of an attempt's ending.
	attemptEndingTimeout = 5 * time.Second

	// memberStatusTimeout bounds the consent page's member status check; past it every row is written.
	memberStatusTimeout = time.Second

	// maxPendingAttemptEndings bounds claims waiting on a slow Redis.
	maxPendingAttemptEndings = 256
)

// federatedHappened names a failure of the organization's own sign-in service.
func federatedHappened(err error) mcpconnectionattempts.Code {
	switch {
	case errors.Is(err, remotesessions.ErrFederatedConfiguration):
		return mcpconnectionattempts.CodeConfigurationRequired
	case errors.Is(err, remotesessions.ErrFederatedSigning):
		return mcpconnectionattempts.CodeInternalError
	case errors.Is(err, remotesessions.ErrFederatedUnavailable):
		return mcpconnectionattempts.CodeIDPTransientFailure
	case errors.Is(err, remotesessions.ErrFederatedIdentity):
		return mcpconnectionattempts.CodeSignInProviderError
	default:
		return mcpconnectionattempts.CodeInternalError
	}
}

// federatedOnward records a consumed handoff without changing its response.
func federatedOnward(ctx context.Context, t *mcpconnectionattempts.Tracker, err error) error {
	if err != nil {
		t.Refuse(ctx, federatedHappened(err))
		return err
	}
	t.Continue()
	return nil
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

// refuseRemoteSessionUnavailable records an outage under its owner; a lock-held refresh is the lock holder's row.
func (s *Service) refuseRemoteSessionUnavailable(ctx context.Context, t *mcpconnectionattempts.Tracker, err error) {
	code := remotesessions.ConnectionOutageHappened(err)
	if code == (mcpconnectionattempts.Code{}) {
		s.refusedWithoutRow(ctx, t, "refresh_busy")
		return
	}
	t.Refuse(ctx, code)
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
	if challengeState.CodeReconnect.expired(time.Now()) || challengeState.WorkloadReconnect.expired(time.Now()) || browserBindingExpired(challengeState, time.Now()) {
		s.endAttemptOnce(ctx, s.logger, attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeExpired, fromRequest, mcpconnectionattempts.Subject(challengeState.Subject))
		return
	}
	s.connections.Step(ctx, attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeSessionChanged, fromRequest, mcpconnectionattempts.Subject(challengeState.Subject))
}

// consentBrowserError records a consent request that failed its browser check
// and refuses it: a challenge out of time as expired, any other as a request
// from another browser.
func (s *Service) consentBrowserError(r *http.Request, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, err error) error {
	s.recordWrongConsentBrowser(r, endpoint, challengeState)
	if errors.Is(err, errChallengeBrowserExpired) {
		return fmt.Errorf("validate consent request: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, err, "authn challenge expired"), oauthui.ReasonExpired))
	}
	return fmt.Errorf("validate consent request: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, err, "invalid consent browser binding"), oauthui.ReasonSessionChanged))
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
		return mcpconnectionattempts.CodeCatalogTooLarge, mcpconnectionattempts.Detail(""), false
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
	waiting, notSetUp := false, false
	for _, server := range gateway.Servers {
		switch server.Status {
		case gatewayServerNotConnected, gatewayServerReconnect, gatewayServerChainPending:
			waiting = true
		case gatewayServerNotSetUp:
			notSetUp = true
		case gatewayServerNoAccess:
		case gatewayServerUnavailable, gatewayServerNoSignIn, gatewayServerConnected, gatewayServerChained:
			return mcpconnectionattempts.CodeMemberToolsMissing
		}
	}
	switch {
	case notSetUp:
		return mcpconnectionattempts.CodeUpstreamSignInNotSetUp
	case waiting:
		return mcpconnectionattempts.CodeNeedsSignIn
	default:
		return mcpconnectionattempts.CodeResourceNotAvailable
	}
}

// ticketClaimHappened reads a failed claim of a connection link. Only a spent or lapsed link is expired.
func ticketClaimHappened(err error) mcpconnectionattempts.Code {
	if errors.Is(err, redisCache.ErrCacheMiss) || errors.Is(err, errCodeReconnectTicketInvalid) {
		return mcpconnectionattempts.CodeExpired
	}
	return mcpconnectionattempts.CodeInternalError
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
		statuses := make([]memberStatus, 0, len(gateway.Servers))
		for _, server := range gateway.Servers {
			if status, ok := gatewayMemberStatus(gateway, server); ok {
				statuses = append(statuses, status)
			}
		}
		s.recordMemberStatuses(ctx, logger, attempt, statuses, subject, fromRequest)
	}
	if problem != nil {
		s.connections.Step(ctx, attempt, mcpconnectionattempts.StepInboundConsent, problem, subject, fromRequest)
	}
	if challengeState.FirstParty && !challengeState.CodeReconnect.undecidedOrCanceled() && firstPartyConnected(gateway, cards) {
		s.endAttemptOnce(ctx, logger, attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete, subject, fromRequest)
	}
}

// memberStatus is one gateway member's discovery row; status is its result and reason.
type memberStatus struct {
	id     string
	status string
	what   mcpconnectionattempts.Happened
	opts   []mcpconnectionattempts.Option
}

func gatewayMemberStatus(gateway *gatewayConsentView, server gatewayServer) (memberStatus, bool) {
	serverID, err := uuid.Parse(server.ServerID)
	if err != nil {
		return memberStatus{id: "", status: "", what: nil, opts: nil}, false
	}
	var authClient *authClientResolution
	for i := range gateway.members {
		if gateway.members[i].serverID == serverID {
			authClient = gateway.members[i].authClient
			break
		}
	}
	what, detail := gatewayServerHappened(server.Status, authClient)
	opts := []mcpconnectionattempts.Option{mcpconnectionattempts.Member(serverID), mcpconnectionattempts.Detail(detail)}
	status := fmt.Sprint(what) + "|" + detail
	if authClient != nil {
		opts = append(opts, mcpconnectionattempts.AuthClient(authClient.clientID))
		status += "|" + authClient.clientID.String()
	}
	return memberStatus{id: serverID.String(), status: status, what: what, opts: opts}, true
}

// memberStatusCache is the cache side of changedMemberStatuses.
type memberStatusCache interface {
	Mutate(ctx context.Context, key string, value any, ttl time.Duration, fn func(exists bool) error) error
}

// recordedMember is a slice element, not a map entry, so a retried read can't keep stale keys.
type recordedMember struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

var errMemberStatusesUnchanged = errors.New("member statuses unchanged")

func memberStatusesKey(attempt mcpconnectionattempts.Attempt) string {
	return "connectionAttemptMembers:" + attempt.ID().String()
}

// recordMemberStatuses writes members whose status changed this attempt.
func (s *Service) recordMemberStatuses(ctx context.Context, logger *slog.Logger, attempt mcpconnectionattempts.Attempt, statuses []memberStatus, opts ...mcpconnectionattempts.Option) {
	var dropped []memberStatus
	for _, status := range s.changedMemberStatuses(ctx, logger, attempt, statuses) {
		if !s.connections.Step(ctx, attempt, mcpconnectionattempts.StepDiscoveryMember, status.what, slices.Concat(status.opts, opts)...) {
			dropped = append(dropped, status)
		}
	}
	s.forgetMemberStatuses(ctx, logger, attempt, dropped)
}

// forgetMemberStatuses clears rows the recorder did not accept, so the next render records them again.
// Rows accepted but lost later in the writer are not retried.
func (s *Service) forgetMemberStatuses(ctx context.Context, logger *slog.Logger, attempt mcpconnectionattempts.Attempt, dropped []memberStatus) {
	store, ok := s.userSessionRefreshReplayCoordination.(memberStatusCache)
	if !ok || len(dropped) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, memberStatusTimeout)
	defer cancel()
	var recorded []recordedMember
	err := store.Mutate(ctx, memberStatusesKey(attempt), &recorded, connectionAttemptEndTTL, func(bool) error {
		kept := recorded[:0]
		for _, member := range recorded {
			if !slices.ContainsFunc(dropped, func(status memberStatus) bool { return status.id == member.ID && status.status == member.Status }) {
				kept = append(kept, member)
			}
		}
		recorded = kept
		return nil
	})
	if err != nil {
		logger.WarnContext(ctx, "forget dropped gateway member statuses", attr.SlogError(err))
	}
}

// changedMemberStatuses keeps members whose status changed this attempt; a cache failure keeps all.
func (s *Service) changedMemberStatuses(ctx context.Context, logger *slog.Logger, attempt mcpconnectionattempts.Attempt, statuses []memberStatus) []memberStatus {
	store, ok := s.userSessionRefreshReplayCoordination.(memberStatusCache)
	if !ok || attempt.IsZero() || len(statuses) == 0 {
		return statuses
	}
	ctx, cancel := context.WithTimeout(ctx, memberStatusTimeout)
	defer cancel()
	var changed []memberStatus
	var recorded []recordedMember
	err := store.Mutate(ctx, memberStatusesKey(attempt), &recorded, connectionAttemptEndTTL, func(bool) error {
		last := make(map[string]int, len(recorded))
		for i, member := range recorded {
			last[member.ID] = i
		}
		changed = changed[:0]
		for _, status := range statuses {
			i, seen := last[status.id]
			switch {
			case !seen:
				last[status.id] = len(recorded)
				recorded = append(recorded, recordedMember{ID: status.id, Status: status.status})
			case recorded[i].Status != status.status:
				recorded[i].Status = status.status
			default:
				continue
			}
			changed = append(changed, status)
		}
		if len(changed) == 0 {
			return errMemberStatusesUnchanged
		}
		return nil
	})
	switch {
	case errors.Is(err, errMemberStatusesUnchanged):
		return nil
	case err != nil:
		logger.WarnContext(ctx, "read recorded gateway member statuses", attr.SlogError(err))
		return statuses
	}
	return changed
}

// memberSetupChangedDetail marks a member whose saved choice no longer fits the sign-in setup an admin changed.
const memberSetupChangedDetail = "setup_changed"

// staleChoiceHappened is the code for whoever the stale choice's message tells to act first.
// An unchecked remedy is our failed lookup, not the person's or an admin's.
func staleChoiceHappened(remedy staleChoiceRemedy) mcpconnectionattempts.Code {
	switch remedy {
	case staleChoiceReconnect, staleChoiceReconnectThenAdmin:
		return mcpconnectionattempts.CodeNeedsSignIn
	case staleChoiceAskAdmin:
		return mcpconnectionattempts.CodeApprovalNeedsAdmin
	case staleChoiceUnchecked:
	}
	return mcpconnectionattempts.CodeInternalError
}

// gatewayServerHappened maps a server's consent status, and any gap in its
// connection choice, to what it records. The detail is the status or the gap.
// A member not connected yet with no client named has no choice to be stale.
func gatewayServerHappened(status gatewayServerStatus, authClient *authClientResolution) (mcpconnectionattempts.Happened, string) {
	detail := string(status)
	firstConnect := status == gatewayServerNotConnected && authClient != nil && authClient.outcome == authClientGap && authClient.clientID == uuid.Nil
	if authClient != nil && !firstConnect && (authClient.outcome == authClientGap || authClient.outcome == authClientNoAccess) {
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
	if challengeState.connectionAttempt(endpoint).IsZero() {
		return
	}
	s.runAttemptEnding(ctx, logger, func(ctx context.Context) {
		s.endChainedFirstPartyInBackground(ctx, logger, fromRequest, endpoint, challengeState, checked)
	})
}

func (s *Service) endChainedFirstPartyInBackground(ctx context.Context, logger *slog.Logger, fromRequest mcpconnectionattempts.Option, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, checked uuid.UUID) {
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
		s.claimAttemptEnding(ctx, logger, challengeState.connectionAttempt(endpoint), mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete, fromRequest, mcpconnectionattempts.Subject(challengeState.Subject))
	}
}

// attemptEndMarker's writers must share one store, which NewService gives them through cacheImpl.
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

// endAttemptOnce ends an attempt from a path that can repeat, off the request path. A failure
// someone will read a reference for waits up to attemptEndingReferenceWait for the ending to be accepted.
// If the marker can't be read or too many are waiting, the attempt stays open rather than ending twice.
func (s *Service) endAttemptOnce(ctx context.Context, logger *slog.Logger, attempt mcpconnectionattempts.Attempt, step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, opts ...mcpconnectionattempts.Option) {
	if attempt.IsZero() {
		return
	}
	wanted := contextvalues.ConnectionReferenceWanted(ctx) && mcpconnectionattempts.FailureWorthAReference(what)
	claimed := make(chan struct{})
	reference := ""
	if !s.runAttemptEnding(ctx, logger, func(ctx context.Context) {
		defer close(claimed)
		// Its own slot, so an ending accepted after the wait can't label a later error.
		ctx = contextvalues.WithConnectionReferenceSlot(ctx)
		s.claimAttemptEnding(ctx, logger, attempt, step, what, opts...)
		reference = contextvalues.ConnectionReference(ctx)
	}) {
		if wanted {
			contextvalues.SetConnectionReference(ctx, "")
		}
		return
	}
	if !wanted {
		return
	}
	// The accepted row's id, or none past the wait.
	select {
	case <-claimed:
		contextvalues.SetConnectionReference(ctx, reference)
	case <-time.After(attemptEndingReferenceWait):
		contextvalues.SetConnectionReference(ctx, "")
	case <-ctx.Done():
		contextvalues.SetConnectionReference(ctx, "")
	}
}

// runAttemptEnding bounds all ending work, including readiness reads, off the response path.
// It reports whether the work was taken on.
func (s *Service) runAttemptEnding(ctx context.Context, logger *slog.Logger, work func(context.Context)) bool {
	if s.connections == nil || s.userSessionRefreshReplayCoordination == nil || s.attemptEndings == nil {
		return false
	}
	select {
	case s.attemptEndings.slots <- struct{}{}:
	default:
		logger.WarnContext(ctx, "too many connection attempt endings waiting; leaving this one open")
		return false
	}
	claimCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), attemptEndingTimeout)
	s.attemptEndings.wg.Go(func() {
		defer cancel()
		defer func() { <-s.attemptEndings.slots }()
		work(claimCtx)
	})
	return true
}

func (s *Service) claimAttemptEnding(ctx context.Context, logger *slog.Logger, attempt mcpconnectionattempts.Attempt, step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, opts ...mcpconnectionattempts.Option) {
	won, err := s.userSessionRefreshReplayCoordination.Add(ctx, attemptEndMarker(attempt), connectionAttemptEndTTL)
	if err != nil {
		logger.WarnContext(ctx, "claim connection attempt ending", attr.SlogError(err))
		return
	}
	if won {
		s.connections.End(ctx, attempt, step, what, opts...)
	}
}

// recordRemoteLoginRefusal records a provider callback refused before its login
// state is spent. A missing parent ends the attempt. The browser preflight also
// ends an expired reconnect lifetime, even if the same request has a browser or
// session mismatch: no browser can complete that expired connection anymore.
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
	case errors.Is(err, errRecoveryUnavailable), errors.Is(err, errToolsetEndpointMismatch):
		return mcpconnectionattempts.CodeConnectionChanged
	case errors.Is(err, errRecoveryWrongLogin), errors.Is(err, errRecoveryNoBrowserBinding), errors.Is(err, errRecoveryOtherBrowser), errors.Is(err, errRecoveryBrowserUnconfirmed), errors.Is(err, errRecoveryOtherUser), errors.Is(err, errRecoveryNotSignedIn):
		return mcpconnectionattempts.CodeSessionChanged
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
	return mcpconnectionattempts.CodeInternalError
}

// recordTokenClientRefusal records /token refusing the client of an authorization
// code. A client that never resolved is no proof of a real flow: a metric, never a row.
func (s *Service) recordTokenClientRefusal(r *http.Request, endpoint *ResolvedMcpEndpoint, grantType, clientID, reason string) {
	if grantType != oauthwire.GrantTypeAuthorizationCode {
		return
	}
	what, recorded := tokenClientRefusalHappened(reason)
	if !recorded || clientID == "" {
		s.metrics.RecordConnectionRefusalWithoutRow(r.Context(), "token_"+reason)
		return
	}
	app := mcpconnectionattempts.AppUnverified
	switch what {
	case mcpconnectionattempts.CodeCIMDMetadataInvalid, mcpconnectionattempts.CodeCIMDAdmissionDenied, mcpconnectionattempts.CodeAIToolBlocked:
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
	if verdict == remotesessions.ValidationOutcomeUnknown && errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	what := verdictHappened(verdict, target.tunnelID.Valid)
	if verdict == remotesessions.ValidationOutcomeUnknown && target.setupFailed {
		what = mcpconnectionattempts.CodeMemberToolsMissing
	}
	s.recordRemoteSessionCheck(ctx, fromRequest, endpoint, challengeState, client, trigger, what,
		mcpconnectionattempts.Member(target.memberID),
		mcpconnectionattempts.RemoteSession(remoteSessionID),
	)
}

// recordRemoteSessionCheck writes one check's row on the challenge's attempt; a keepalive writes none.
func (s *Service) recordRemoteSessionCheck(
	ctx context.Context,
	fromRequest mcpconnectionattempts.Option,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	trigger remotesessionmetrics.ValidationTrigger,
	what mcpconnectionattempts.Happened,
	opts ...mcpconnectionattempts.Option,
) {
	if trigger == remotesessionmetrics.ValidationTriggerKeepalive {
		return
	}
	opts = append([]mcpconnectionattempts.Option{
		fromRequest,
		mcpconnectionattempts.Detail(string(trigger)),
		mcpconnectionattempts.Provider(client.IssuerURL),
		mcpconnectionattempts.RemoteSessionClient(client.ID),
		mcpconnectionattempts.RemoteSessionIssuer(client.RemoteSessionIssuerID),
		mcpconnectionattempts.Subject(challengeState.Subject),
	}, opts...)
	s.connections.Step(ctx, challengeState.connectionAttempt(endpoint), mcpconnectionattempts.StepProviderSignIn, what, opts...)
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
	return mcpconnectionattempts.CodeProviderUnavailable
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
		if errors.Is(ctx.Err(), context.Canceled) && errors.Is(failed.err, context.Canceled) {
			continue
		}
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

// recordIDPAuthorityRefusal keeps dependency outages retryable but ends a revoked authority once.
func (s *Service) recordIDPAuthorityRefusal(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, request mcpconnectionattempts.Option, err error) {
	code, detail := remotesessions.AuthorityHappened(err)
	if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
		s.connections.Step(ctx, state.connectionAttempt(endpoint), mcpconnectionattempts.StepInboundSignIn, code, detail, request)
		return
	}
	s.endAttemptOnce(ctx, logger, state.connectionAttempt(endpoint), mcpconnectionattempts.StepInboundSignIn, code, detail, request)
}

// refuseWorkloadGrantRow records a refused workload or assistant token grant.
func refuseWorkloadGrantRow(ctx context.Context, t *mcpconnectionattempts.Tracker, err error) {
	code, detail := workloadGrantHappened(err)
	t.Refuse(ctx, code, detail)
}

// workloadGrantHappened names a refused workload or assistant token grant; the detail is the logged reason.
// Workload refusals before the assertion's signature verifies never reach it: they are a metric.
func workloadGrantHappened(err error) (mcpconnectionattempts.Code, mcpconnectionattempts.Option) {
	if refusal, ok := errors.AsType[*workloadGrantError](err); ok {
		detail := mcpconnectionattempts.Detail(refusal.reason)
		switch refusal.reason {
		case "issuer_untrusted":
			return mcpconnectionattempts.CodeIssuerNotTrusted, detail
		case "subject_not_admitted":
			return mcpconnectionattempts.CodeSubjectNotAdmitted, detail
		case "endpoint_not_agent_addressable":
			return mcpconnectionattempts.CodeEndpointNotAgentAddressable, detail
		case "agent_admission_denied":
			return mcpconnectionattempts.CodeNoServerAccess, detail
		case "issuer_policy_changed":
			return mcpconnectionattempts.CodeTransientFailure, detail
		}
		switch refusal.outcome {
		case workloadGrantUnavailable, workloadGrantRateLimited:
			return mcpconnectionattempts.CodeAssertionCheckUnavailable, detail
		case workloadGrantRefused:
		}
		if strings.HasPrefix(refusal.reason, "assertion_") {
			return mcpconnectionattempts.CodeAssertionInvalid, detail
		}
		return mcpconnectionattempts.CodeAssertionOther, detail
	}
	if problem, ok := errors.AsType[headlessChoiceError](err); ok {
		detail := mcpconnectionattempts.Detail(string(problem))
		switch problem {
		case errChoiceNotSetUp, errChoiceNotReady, errChoiceNoneReady:
			return mcpconnectionattempts.CodeUpstreamSignInNotSetUp, detail
		case errChoiceDenied, errChoiceNoneAllowed:
			return mcpconnectionattempts.CodeUpstreamCredentialDenied, detail
		case errChoiceNotNamed:
			return mcpconnectionattempts.CodeUpstreamCredentialNotChosen, detail
		case errChoiceNotAccepted:
			return mcpconnectionattempts.CodeUpstreamCredentialChoice, detail
		case errChoiceServerNotReached, errChoiceTakesNoCredential, errChoiceNeedsCredential:
			return mcpconnectionattempts.CodeCredentialChoiceInvalid, detail
		}
	}
	return mcpconnectionattempts.CodeInternalError, mcpconnectionattempts.Detail("")
}
