// What sign-in steps record on their connection attempt. None of this changes a response.

package mcp

import (
	"context"
	"errors"
	"log/slog"
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
)

// connectionAttemptEndTTL outlives the longest a browser attempt can stay live.
const connectionAttemptEndTTL = 2 * time.Hour

// federatedHappened names a failure of the organization's own sign-in service.
func federatedHappened(err error) mcpconnectionattempts.Code {
	if errors.Is(err, remotesessions.ErrFederatedConfiguration) {
		return mcpconnectionattempts.CodeConnectionChanged
	}
	return mcpconnectionattempts.CodeIDPUnavailable
}

// recordConsentStateGone records a consent page opened after its sign-in expired.
// The state held the attempt, so this is its own one-row attempt.
func (s *Service) recordConsentStateGone(ctx context.Context, endpoint *ResolvedMcpEndpoint, err error) {
	if errors.Is(err, redisCache.ErrCacheMiss) {
		s.connections.Once(ctx, connectionScope(endpoint, mcpconnectionattempts.AppUnverified), mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeExpired)
	}
}

// recordConsentPage records what a rendered consent page told the person.
func (s *Service) recordConsentPage(
	ctx context.Context,
	logger *slog.Logger,
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
			s.recordGatewayServerStatus(ctx, attempt, gateway, server, subject)
		}
	}
	if problem != nil {
		s.connections.Step(ctx, attempt, mcpconnectionattempts.StepInboundConsent, problem, subject)
	}
	if challengeState.FirstParty && firstPartyConnected(gateway, cards) {
		s.endFirstPartyAttempt(ctx, logger, attempt, subject)
	}
}

func (s *Service) recordGatewayServerStatus(ctx context.Context, attempt mcpconnectionattempts.Attempt, gateway *gatewayConsentView, server gatewayServer, subject mcpconnectionattempts.Option) {
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
	opts := []mcpconnectionattempts.Option{subject, mcpconnectionattempts.Member(serverID), mcpconnectionattempts.Detail(detail)}
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
		return len(cards) == 0 || shouldAutoCloseFirstParty(true, cards)
	case gateway.FocusUnavailable:
		return false
	case gateway.Focus != nil:
		if len(cards) == 0 {
			return gateway.Focus.Ready()
		}
		return shouldAutoCloseFirstParty(true, cards)
	}
	visible := gateway.VisibleServers()
	for _, server := range visible {
		if !server.Ready() {
			return false
		}
	}
	return len(visible) > 0
}

// endFirstPartyAttempt ends the attempt once however often the page renders. If
// the once-only marker cannot be read, the attempt stays open and reads as
// abandoned rather than ending twice.
func (s *Service) endFirstPartyAttempt(ctx context.Context, logger *slog.Logger, attempt mcpconnectionattempts.Attempt, subject mcpconnectionattempts.Option) {
	if attempt.IsZero() || s.userSessionRefreshReplayCoordination == nil {
		return
	}
	won, err := s.userSessionRefreshReplayCoordination.Add(ctx, "connectionAttemptEnded:"+attempt.ID().String(), connectionAttemptEndTTL)
	if err != nil {
		logger.WarnContext(ctx, "claim connection attempt ending", attr.SlogError(err))
		return
	}
	if won {
		s.connections.End(ctx, attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete, subject)
	}
}

// recordTokenClientRefusal records /token refusing the client of an
// authorization code. Other grants have their own flows.
func (s *Service) recordTokenClientRefusal(ctx context.Context, endpoint *ResolvedMcpEndpoint, grantType, clientID, reason string) {
	if grantType != oauthwire.GrantTypeAuthorizationCode {
		return
	}
	app := mcpconnectionattempts.AppUnverified
	what := mcpconnectionattempts.CodeUnknownClientID
	switch reason {
	case "cimd_admission_denied", "cimd_admission_disabled":
		what = mcpconnectionattempts.CodeCIMDAdmissionDenied
	case "cimd_metadata_invalid", clientIDNamesEndpointReason:
		what = mcpconnectionattempts.CodeCIMDMetadataInvalid
	case "ai_tool_blocked":
		what = mcpconnectionattempts.CodeAIToolBlocked
		app = mcpconnectionattempts.AppOf(clientID)
	}
	s.connections.Once(ctx, connectionScope(endpoint, app), mcpconnectionattempts.StepInboundToken, what, mcpconnectionattempts.Grant(grantType), mcpconnectionattempts.Detail(reason))
}

// recordTokenClientFailure records /token unable to check the client of an authorization code.
func (s *Service) recordTokenClientFailure(ctx context.Context, endpoint *ResolvedMcpEndpoint, grantType string, reason mcpmetrics.IDJAGRejectionReason) {
	if grantType != oauthwire.GrantTypeAuthorizationCode {
		return
	}
	what := mcpconnectionattempts.CodeAIToolCheckUnavailable
	if reason == mcpmetrics.IDJAGRejectionClientMetadataUnavailable {
		what = mcpconnectionattempts.CodeCIMDFetchFailed
	}
	s.connections.Once(ctx, connectionScope(endpoint, mcpconnectionattempts.AppUnverified), mcpconnectionattempts.StepInboundToken, what, mcpconnectionattempts.Grant(grantType))
}

// recordRemoteSessionVerdict records the check of a provider sign-in on the
// attempt that made it. The person can reconnect, so it never ends the attempt.
// A keepalive belongs to no attempt.
func (s *Service) recordRemoteSessionVerdict(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	memberID, remoteSessionID uuid.UUID,
	verdict remotesessions.ValidationOutcome,
	trigger remotesessionmetrics.ValidationTrigger,
) {
	if trigger == remotesessionmetrics.ValidationTriggerKeepalive {
		return
	}
	s.connections.Step(ctx, challengeState.connectionAttempt(endpoint), mcpconnectionattempts.StepProviderSignIn, verdictHappened(verdict),
		mcpconnectionattempts.Detail(string(trigger)),
		mcpconnectionattempts.Member(memberID),
		mcpconnectionattempts.Provider(client.IssuerURL),
		mcpconnectionattempts.RemoteSession(remoteSessionID),
		mcpconnectionattempts.RemoteSessionClient(client.ID),
		mcpconnectionattempts.RemoteSessionIssuer(client.RemoteSessionIssuerID),
		mcpconnectionattempts.Subject(challengeState.Subject),
	)
}

func verdictHappened(verdict remotesessions.ValidationOutcome) mcpconnectionattempts.Happened {
	switch verdict {
	case remotesessions.ValidationOutcomeValid:
		return mcpconnectionattempts.ResultAllowed
	case remotesessions.ValidationOutcomeRejectedByMember:
		return mcpconnectionattempts.CodeRejectedByMember
	case remotesessions.ValidationOutcomeInactive:
		return mcpconnectionattempts.CodeProviderTokenInactive
	case remotesessions.ValidationOutcomeUnknown:
		return mcpconnectionattempts.CodeProviderUnreachable
	}
	return mcpconnectionattempts.CodeProviderUnreachable
}

// recordMemberListing records one result per member under one listing attempt.
func (s *Service) recordMemberListing(ctx context.Context, gate *metaGateContext, members []metaMember, described []memberDescribeResult) {
	// A canceled request failed the listing itself, and says nothing about its members.
	if ctx.Err() != nil {
		return
	}
	app := mcpconnectionattempts.AppUnverified
	if clientID, ok := contextvalues.GetOAuthClientID(ctx); ok {
		app = mcpconnectionattempts.AppOf(clientID)
	}
	scope := mcpconnectionattempts.GatewayScope(gate.organizationID, gate.projectID, gate.metaServerID, app)
	if gate.organizationGateway {
		scope = mcpconnectionattempts.OrganizationGatewayScope(gate.organizationID, gate.metaServerID, app)
	}
	attempt := mcpconnectionattempts.Begin(scope)
	var listing mcpconnectionattempts.Happened = mcpconnectionattempts.ResultComplete
	for i, member := range members {
		what := memberDescribeHappened(described[i])
		if what == mcpconnectionattempts.Happened(mcpconnectionattempts.CodeMemberToolsMissing) {
			listing = mcpconnectionattempts.CodeMemberToolsMissing
		}
		s.connections.Step(ctx, attempt, mcpconnectionattempts.StepDiscoveryMember, what, mcpconnectionattempts.Member(member.serverID))
	}
	for _, member := range gate.unapprovedMembers {
		s.connections.Step(ctx, attempt, mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeConsentNotApproved, mcpconnectionattempts.Member(member.serverID))
	}
	s.connections.End(ctx, attempt, mcpconnectionattempts.StepDiscoveryListing, listing)
}

func memberDescribeHappened(result memberDescribeResult) mcpconnectionattempts.Happened {
	if result.err == nil {
		if result.catalog == nil || result.catalog.incomplete {
			return mcpconnectionattempts.CodeMemberToolsMissing
		}
		return mcpconnectionattempts.ResultListed
	}
	if _, credential := errors.AsType[memberCredentialError](result.err); credential {
		return mcpconnectionattempts.CodeNeedsSignIn
	}
	if denied, ok := errors.AsType[*oops.ShareableError](result.err); ok && (denied.Code == oops.CodeForbidden || denied.Code == oops.CodeNotFound) {
		return mcpconnectionattempts.CodeNoServerAccess
	}
	return mcpconnectionattempts.CodeMemberToolsMissing
}
