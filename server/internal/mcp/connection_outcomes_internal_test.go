package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCanceledConnectionChecksDoNotBlameProvider(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rec := mcpconnectionattemptstest.New(t)
	s := &Service{connections: rec.Recorder}
	s.recordRemoteSessionVerdict(ctx, mcpconnectionattempts.FromRequest(nil), directEndpoint(), openState(), remotesessions.Client{}, validationTarget{}, uuid.New(), remotesessions.ValidationOutcomeUnknown, remotesessionmetrics.ValidationTriggerVerify)
	gate := &metaGateContext{organizationID: "org-test", projectID: uuid.New(), metaServerID: uuid.New()}
	s.recordListingFailures(ctx, gate, []unavailableMember{{member: metaMember{serverID: uuid.New()}, err: context.Canceled}})
	rec.RequireNothing()
	// A concrete refusal remains true even if the caller subsequently leaves.
	s.recordRemoteSessionVerdict(ctx, mcpconnectionattempts.FromRequest(nil), directEndpoint(), openState(), remotesessions.Client{}, validationTarget{}, uuid.New(), remotesessions.ValidationOutcomeRejectedByMember, remotesessionmetrics.ValidationTriggerVerify)
	rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeRejectedByMember)
	s.recordListingFailures(ctx, gate, []unavailableMember{{member: metaMember{serverID: uuid.New()}, err: oops.E(oops.CodeForbidden, nil, "denied")}})
	rec.RequireEnded(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNoServerAccess)
}

func TestTokenAdmissionRefusalKeepsCIMDApp(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"cimd_admission_denied", "cimd_admission_disabled"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			s := &Service{connections: rec.Recorder}
			clientID := "https://claude.ai"
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/token", nil)
			s.recordTokenClientRefusal(req, directEndpoint(), oauthwire.GrantTypeAuthorizationCode, clientID, reason)
			row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeCIMDAdmissionDenied)
			require.Equal(t, mcpconnectionattempts.AppOf(clientID).String(), row.AppKey)
			require.NotEqual(t, mcpconnectionattempts.AppUnverified.String(), row.AppKey)
		})
	}
}

var testAttemptStart = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func directEndpoint() *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{OrganizationID: "org-test", ProjectID: uuid.New(), McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
}

func gatewayEndpoint() *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{OrganizationID: "org-test", ProjectID: uuid.New(), MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
}

func openState() AuthnChallengeState {
	subject := urn.NewUserSubject("user-test")
	return AuthnChallengeState{FlowID: uuid.NewString(), CreatedAt: testAttemptStart, Subject: &subject}
}

// Each member's status is one row under the sign-in, so a loop shows as a
// provider success followed by a status that still needs connecting.
func TestRecordConsentPage_OneRowPerGatewayMember(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	connected, stale, unset := uuid.New(), uuid.New(), uuid.New()
	gap := authClientResolution{outcome: authClientGap, reason: authClientReasonNoAuthStale}
	view := &gatewayConsentView{
		members: []metaMember{{serverID: stale, authClient: &gap}},
		Servers: []gatewayServer{
			{ServerID: connected.String(), Status: gatewayServerConnected},
			{ServerID: stale.String(), Status: gatewayServerReconnect},
			{ServerID: unset.String(), Status: gatewayServerNotSetUp},
		},
	}

	service.recordConsentPage(t.Context(), nil, mcpconnectionattempts.FromRequest(nil), gatewayEndpoint(), openState(), view, nil, nil)

	require.Equal(t, connected.String(), rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.ResultAllowed).MemberID)
	row := rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNeedsSignIn)
	require.Equal(t, stale.String(), row.MemberID)
	require.Equal(t, authClientReasonNoAuthStale, row.SubReason, "the gap that was only a debug log")
	require.Empty(t, row.AuthClientID, "that reason names no auth client")
	require.Equal(t, unset.String(), rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeUpstreamSignInNotSetUp).MemberID)
	rec.RequireNotEnded()
}

func TestGatewayServerHappened_FirstConnectIsNotAStaleChoice(t *testing.T) {
	t.Parallel()

	noPick := func(reason string) *authClientResolution {
		return &authClientResolution{outcome: authClientGap, reason: reason}
	}
	named := &authClientResolution{outcome: authClientGap, reason: authClientReasonClientDisabled, clientID: uuid.New()}
	for name, tc := range map[string]struct {
		status     gatewayServerStatus
		authClient *authClientResolution
		want       string
	}{
		"first connect, server requires auth":    {gatewayServerNotConnected, noPick(authClientReasonNoAuthForbidden), "not_connected"},
		"first connect, clients not recorded":    {gatewayServerNotConnected, noPick(authClientReasonNoAuthStale), "not_connected"},
		"not connected, the chosen client broke": {gatewayServerNotConnected, named, authClientReasonClientDisabled},
		"lapsed connection keeps the gap":        {gatewayServerReconnect, noPick(authClientReasonNoAuthStale), authClientReasonNoAuthStale},
		"not set up keeps the gap":               {gatewayServerNotSetUp, noPick(authClientReasonNoAuthStale), authClientReasonNoAuthStale},
		"no access keeps its reason":             {gatewayServerNotConnected, &authClientResolution{outcome: authClientNoAccess, reason: authClientReasonNotPermitted}, authClientReasonNotPermitted},
	} {
		_, detail := gatewayServerHappened(tc.status, tc.authClient)
		require.Equal(t, tc.want, detail, name)
	}
}

func TestRecordConsentPage_ProblemIsAStepOnTheAttempt(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	service.recordConsentPage(t.Context(), nil, mcpconnectionattempts.FromRequest(nil), directEndpoint(), openState(), nil, []remoteSessionCard{{}}, mcpconnectionattempts.CodeNoServerAccess)

	rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeNoServerAccess)
	rec.RequireNotEnded()
}

func TestFirstPartyConnected(t *testing.T) {
	t.Parallel()

	done := remoteSessionCard{Connected: true}
	open := remoteSessionCard{}
	ready := gatewayServer{Status: gatewayServerConnected}
	waiting := gatewayServer{Status: gatewayServerNotConnected}

	require.True(t, firstPartyConnected(nil, nil), "nothing to connect")
	require.True(t, firstPartyConnected(nil, []remoteSessionCard{done}))
	require.False(t, firstPartyConnected(nil, []remoteSessionCard{{Connected: true, Unavailable: true}}), "an outage must not complete the sign-in")
	require.False(t, firstPartyConnected(nil, []remoteSessionCard{{Connected: true, ValidationNotice: "provider unavailable"}}), "an unresolved validation notice keeps the flow open")
	require.True(t, firstPartyConnected(nil, []remoteSessionCard{{Chained: true, ChainCheck: chainCheckConnected}}), "a chained service connects with no sign-in")
	require.False(t, firstPartyConnected(nil, []remoteSessionCard{{Chained: true, ChainCheck: chainCheckPending}}))
	require.False(t, firstPartyConnected(nil, []remoteSessionCard{done, open}))
	require.False(t, firstPartyConnected(nil, []remoteSessionCard{{Connected: true, Rejected: true}}), "a rejected sign-in can still be redone")
	require.True(t, firstPartyConnected(&gatewayConsentView{Servers: []gatewayServer{ready}}, nil))
	require.False(t, firstPartyConnected(&gatewayConsentView{Servers: []gatewayServer{ready, waiting}}, nil))
	require.False(t, firstPartyConnected(&gatewayConsentView{}, nil), "a gateway with nothing listed proves nothing")
	require.True(t, firstPartyConnected(&gatewayConsentView{Focus: &ready}, []remoteSessionCard{done}), "a code mode connect ends on its one server")
	require.False(t, firstPartyConnected(&gatewayConsentView{Focus: &waiting}, []remoteSessionCard{open}))
	require.False(t, firstPartyConnected(&gatewayConsentView{FocusUnavailable: true, Servers: []gatewayServer{ready}}, nil))
}

func TestVerdictHappened(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.ResultAllowed), verdictHappened(remotesessions.ValidationOutcomeValid, false))
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeRejectedByMember), verdictHappened(remotesessions.ValidationOutcomeRejectedByMember, false))
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeProviderRejected), verdictHappened(remotesessions.ValidationOutcomeInactive, false))
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeProviderUnavailable), verdictHappened(remotesessions.ValidationOutcomeUnknown, false))
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeTunnelUnavailable), verdictHappened(remotesessions.ValidationOutcomeUnknown, true))
}

func TestRecordRemoteSessionVerdict_KeepaliveBelongsToNoAttempt(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	service.recordRemoteSessionVerdict(t.Context(), mcpconnectionattempts.FromRequest(nil), directEndpoint(), openState(), remotesessions.Client{}, validationTarget{}, uuid.New(), remotesessions.ValidationOutcomeRejectedByMember, remotesessionmetrics.ValidationTriggerKeepalive)
	rec.RequireNothing()
}

func TestTokenClientRefusal(t *testing.T) {
	t.Parallel()

	for reason, what := range map[string]mcpconnectionattempts.Code{
		"cimd_admission_denied":               mcpconnectionattempts.CodeCIMDAdmissionDenied,
		"cimd_admission_disabled":             mcpconnectionattempts.CodeCIMDAdmissionDenied,
		"cimd_metadata_invalid":               mcpconnectionattempts.CodeCIMDMetadataInvalid,
		"ai_tool_blocked":                     mcpconnectionattempts.CodeAIToolBlocked,
		"client_secret_mismatch":              mcpconnectionattempts.CodeClientAuthFailed,
		"public_client_presented_credentials": mcpconnectionattempts.CodeClientAuthFailed,
		"assertion_client_presented_secret":   mcpconnectionattempts.CodeClientAuthFailed,
		"assertion_verifier_unavailable":      mcpconnectionattempts.CodeAssertionVerifierMisconfigured,
		"assertion_replay_store_unavailable":  mcpconnectionattempts.CodeAssertionCheckUnavailable,
		"assertion_signature_invalid":         mcpconnectionattempts.CodeAssertionInvalid,
		"client_method_unreadable":            mcpconnectionattempts.CodeInternalError,
	} {
		rec := mcpconnectionattemptstest.New(t)
		service := &Service{connections: rec.Recorder}
		service.recordTokenClientRefusal(tokenRequest(t), directEndpoint(), oauthwire.GrantTypeAuthorizationCode, "client", reason)
		require.Equal(t, reason, rec.RequireEnded(mcpconnectionattempts.StepInboundToken, what).SubReason)
	}

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	code := oauthwire.GrantTypeAuthorizationCode
	for _, reason := range []string{"missing_client_id", "unknown_client_id", clientIDNamesEndpointReason} {
		service.recordTokenClientRefusal(tokenRequest(t), directEndpoint(), code, "client", reason)
	}
	service.recordTokenClientRefusal(tokenRequest(t), directEndpoint(), code, "", "assertion_malformed")
	service.recordTokenClientRefusal(tokenRequest(t), directEndpoint(), oauthwire.GrantTypeRefreshToken, "client", "client_secret_mismatch")
	service.recordTokenClientFailure(tokenRequest(t), directEndpoint(), oauthwire.GrantTypeRefreshToken, "https://client.example.com/app.json", mcpmetrics.IDJAGRejectionClientCheckUnavailable)
	rec.RequireNothing()

	service.recordTokenClientFailure(tokenRequest(t), directEndpoint(), code, "https://client.example.com/app.json", mcpmetrics.IDJAGRejectionClientCheckUnavailable)
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeAIToolCheckUnavailable)
	service.recordTokenClientFailure(tokenRequest(t), directEndpoint(), code, "https://client.example.com/app.json", mcpmetrics.IDJAGRejectionClientMetadataUnavailable)
	require.Equal(t, mcpconnectionattempts.AppOtherMetadataURL.String(), rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeCIMDFetchFailed).AppKey)
}

func TestRecordListingFailures(t *testing.T) {
	t.Parallel()

	gate := &metaGateContext{organizationID: "org-test", projectID: uuid.New(), metaServerID: uuid.New()}
	broken, denied := uuid.New(), uuid.New()
	unavailable := []unavailableMember{
		{member: metaMember{serverID: uuid.New()}, err: memberCredentialError{&metaMemberError{message: "sign in"}}},
		{member: metaMember{serverID: broken}, err: errors.New("down")},
		{member: metaMember{serverID: denied}, err: oops.E(oops.CodeForbidden, nil, "no")},
	}

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	service.recordListingFailures(t.Context(), gate, unavailable)
	require.Len(t, rec.Rows(), 2, "waiting on sign-in is not a failure")
	require.Equal(t, broken.String(), rec.RequireEnded(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeMemberToolsMissing).MemberID)
	require.Equal(t, denied.String(), rec.RequireEnded(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNoServerAccess).MemberID)

	orgGate := *gate
	orgGate.projectID = uuid.Nil
	orgRec := mcpconnectionattemptstest.New(t)
	orgService := &Service{connections: orgRec.Recorder}
	orgService.recordListingFailures(t.Context(), &orgGate, unavailable[1:2])
	require.Equal(t, uuid.Nil, orgRec.RequireEnded(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeMemberToolsMissing).ProjectID)
}

func TestAccessHappened(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpconnectionattempts.CodeNoServerAccess, accessHappened(errConsentAgentDenied))
	require.Equal(t, mcpconnectionattempts.CodeNoServerAccess, accessHappened(oops.E(oops.CodeForbidden, nil, "no")))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, accessHappened(errors.New("database down")))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, accessHappened(oops.E(oops.CodeUnavailable, nil, "down")))
}

func TestNoReadyServerHappened(t *testing.T) {
	t.Parallel()

	view := func(statuses ...gatewayServerStatus) *gatewayConsentView {
		servers := make([]gatewayServer, 0, len(statuses))
		for _, status := range statuses {
			servers = append(servers, gatewayServer{Status: status})
		}
		return &gatewayConsentView{Servers: servers}
	}
	for name, tc := range map[string]struct {
		gateway *gatewayConsentView
		want    mcpconnectionattempts.Code
	}{
		"waiting on sign-ins":          {view(gatewayServerNotConnected, gatewayServerReconnect), mcpconnectionattempts.CodeNeedsSignIn},
		"chain pending can connect":    {view(gatewayServerChainPending), mcpconnectionattempts.CodeNeedsSignIn},
		"no servers":                   {view(), mcpconnectionattempts.CodeResourceNotAvailable},
		"no access to any":             {view(gatewayServerNoAccess, gatewayServerNoAccess), mcpconnectionattempts.CodeResourceNotAvailable},
		"no access but one to connect": {view(gatewayServerNoAccess, gatewayServerNotConnected), mcpconnectionattempts.CodeNeedsSignIn},
		"not set up":                   {view(gatewayServerNotSetUp), mcpconnectionattempts.CodeUpstreamSignInNotSetUp},
		"not set up and one waiting":   {view(gatewayServerNotConnected, gatewayServerNotSetUp), mcpconnectionattempts.CodeUpstreamSignInNotSetUp},
		"a real outage":                {view(gatewayServerNotConnected, gatewayServerUnavailable), mcpconnectionattempts.CodeMemberToolsMissing},
	} {
		require.Equal(t, tc.want, noReadyServerHappened(tc.gateway), name)
	}
}

func TestRemoteLoginRefusalHappened(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpconnectionattempts.CodeSessionChanged, remoteLoginRefusalHappened(oops.E(oops.CodeUnauthorized, nil, "wrong browser")))
	require.Equal(t, mcpconnectionattempts.CodeSessionChanged, remoteLoginRefusalHappened(errRecoveryWrongLogin))
	require.Equal(t, mcpconnectionattempts.CodeSessionChanged, remoteLoginRefusalHappened(errRecoveryNoBrowserBinding))
	require.Equal(t, mcpconnectionattempts.CodeSessionChanged, remoteLoginRefusalHappened(errRecoveryOtherBrowser))
	require.Equal(t, mcpconnectionattempts.CodeSessionChanged, remoteLoginRefusalHappened(errRecoveryBrowserUnconfirmed))
	require.Equal(t, mcpconnectionattempts.CodeSessionChanged, remoteLoginRefusalHappened(errRecoveryOtherUser))
	require.Equal(t, mcpconnectionattempts.CodeSessionChanged, remoteLoginRefusalHappened(errRecoveryNotSignedIn))
	require.Equal(t, mcpconnectionattempts.CodeConnectionChanged, remoteLoginRefusalHappened(errToolsetEndpointMismatch))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, remoteLoginRefusalHappened(errors.New("check connection user membership: down")))
	require.Equal(t, mcpconnectionattempts.CodeNoServerAccess, remoteLoginRefusalHappened(errRecoveryNoAccess))
	require.Equal(t, mcpconnectionattempts.CodeNoServerAccess, remoteLoginRefusalHappened(oops.E(oops.CodeForbidden, nil, "no")))
	require.Equal(t, mcpconnectionattempts.CodeConnectionChanged, remoteLoginRefusalHappened(errRecoveryUnavailable))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, remoteLoginRefusalHappened(oops.E(oops.CodeUnavailable, nil, "redis down")))
}

func TestFederatedHappened(t *testing.T) {
	t.Parallel()
	require.Equal(t, mcpconnectionattempts.CodeInternalError, federatedHappened(errors.New("database unavailable")))

	require.Equal(t, mcpconnectionattempts.CodeConfigurationRequired, federatedHappened(remotesessions.ErrFederatedConfiguration))
	require.Equal(t, mcpconnectionattempts.CodeIDPTransientFailure, federatedHappened(remotesessions.ErrFederatedUnavailable))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, federatedHappened(errors.Join(remotesessions.ErrFederatedUnavailable, remotesessions.ErrFederatedSigning)))
	require.Equal(t, mcpconnectionattempts.CodeSignInProviderError, federatedHappened(remotesessions.ErrFederatedIdentity))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, federatedHappened(remotesessions.ErrFederatedSigning))
}

func TestReconnectFailure(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err      error
		code     mcpconnectionattempts.Code
		terminal bool
	}{
		"catalog expired":  {errConnectionCatalogExpired, mcpconnectionattempts.CodeConnectionCatalogExpired, true},
		"review evicted":   {redisCache.ErrCacheMiss, mcpconnectionattempts.CodeConnectionCatalogExpired, true},
		"another approval": {errReconnectCatalogSuperseded, mcpconnectionattempts.CodeConnectionCatalogExpired, true},
		"revoked":          {errReconnectConnectionInactive, mcpconnectionattempts.CodeRefreshTokenRevoked, true},
		"setup changed":    {errReconnectCatalogChanged, mcpconnectionattempts.CodeConnectionChanged, true},
		"not frozen":       {errReconnectCatalogNotFrozen, mcpconnectionattempts.CodeConnectionChanged, true},
		"config changed":   {errConsentConfigurationChanged, mcpconnectionattempts.CodeConnectionChanged, true},
		"too large":        {errConnectionCatalogTooLarge, mcpconnectionattempts.CodeCatalogTooLarge, false},
		"database down":    {errors.New("database down"), mcpconnectionattempts.CodeInternalError, false},
	} {
		code, _, terminal := reconnectFailure(tc.err)
		require.Equal(t, tc.code, code, name)
		require.Equal(t, tc.terminal, terminal, name)
	}
}

// A review not yet approved has changed nothing, so it isn't Connected however the cards read.
func TestRecordConsentPage_PendingReviewDoesNotComplete(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	state := openState()
	state.FirstParty = true
	state.CodeReconnect = &CodeReconnectBinding{CatalogID: uuid.New(), ReviewReady: true}
	service.recordConsentPage(t.Context(), nil, mcpconnectionattempts.FromRequest(nil), directEndpoint(), state, nil, nil, nil)
	rec.RequireNothing()
}

// addOnlyCache is the once-only marker alone, optionally stalled like an unreachable Redis.
type addOnlyCache struct {
	cache.Cache
	release chan struct{}
	mu      sync.Mutex
	seen    map[string]bool
}

func (c *addOnlyCache) Add(_ context.Context, key string, _ time.Duration) (bool, error) {
	if c.release != nil {
		<-c.release
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[key] {
		return false, nil
	}
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	c.seen[key] = true
	return true, nil
}

// A stalled Redis can't hold up a sign-in: the claim runs off the request path.
func TestEndAttemptOnce_StalledRedisDoesNotBlockTheRequest(t *testing.T) {
	t.Parallel()

	stalled := &addOnlyCache{release: make(chan struct{})}
	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder, userSessionRefreshReplayCoordination: stalled, attemptEndings: newAttemptEndings()}
	state := openState()
	attempt := state.connectionAttempt(directEndpoint())
	logger := testenv.NewLogger(t)

	returned := make(chan struct{})
	go func() {
		service.endAttemptOnce(t.Context(), logger, attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("recording held up the request")
	}
	rec.RequireNothing()

	close(stalled.release)
	require.NoError(t, service.attemptEndings.drain(t.Context()))
	rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete)
}

// Past the cap an ending is dropped rather than queued without limit.
func TestEndAttemptOnce_FullQueueLeavesTheAttemptOpen(t *testing.T) {
	t.Parallel()

	stalled := &addOnlyCache{release: make(chan struct{})}
	rec := mcpconnectionattemptstest.New(t)
	endings := newAttemptEndings()
	service := &Service{connections: rec.Recorder, userSessionRefreshReplayCoordination: stalled, attemptEndings: endings}
	logger := testenv.NewLogger(t)
	for range maxPendingAttemptEndings + 5 {
		state := openState()
		service.endAttemptOnce(t.Context(), logger, state.connectionAttempt(directEndpoint()), mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete)
	}
	close(stalled.release)
	require.NoError(t, endings.drain(t.Context()))
	require.Len(t, rec.Rows(), maxPendingAttemptEndings)
}

// A canceled review never reads as complete, on a reload or if its own ending was lost.
func TestRecordConsentPage_CanceledReviewNeverCompletes(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder, userSessionRefreshReplayCoordination: &addOnlyCache{}, attemptEndings: newAttemptEndings()}
	state := openState()
	state.FirstParty = true
	state.CodeReconnect = &CodeReconnectBinding{CatalogID: uuid.New(), ReviewReady: true, CatalogComplete: true, CatalogCanceled: true}

	service.recordConsentPage(t.Context(), testenv.NewLogger(t), mcpconnectionattempts.FromRequest(nil), directEndpoint(), state, nil, nil, nil)

	require.NoError(t, service.attemptEndings.drain(t.Context()))
	rec.RequireNothing()

	state.CodeReconnect.CatalogCanceled = false
	service.recordConsentPage(t.Context(), testenv.NewLogger(t), mcpconnectionattempts.FromRequest(nil), directEndpoint(), state, nil, nil, nil)
	require.NoError(t, service.attemptEndings.drain(t.Context()))
	rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete)
}

func tokenRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/token", nil)
}

func TestRecordIDPAuthorityRefusal(t *testing.T) {
	t.Parallel()
	for _, unavailable := range []bool{true, false} {
		rec := mcpconnectionattemptstest.New(t)
		service := &Service{connections: rec.Recorder, userSessionRefreshReplayCoordination: &addOnlyCache{}, attemptEndings: newAttemptEndings()}
		endpoint, state := directEndpoint(), openState()
		err := errors.New("authority changed")
		if unavailable {
			err = networkingress.ErrAuthorityUnavailable
		}
		for range 2 {
			service.recordIDPAuthorityRefusal(t.Context(), testenv.NewLogger(t), endpoint, state, mcpconnectionattempts.FromRequest(nil), err)
		}
		require.NoError(t, service.attemptEndings.drain(t.Context()))
		if unavailable {
			rec.RequireStep(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeInternalError)
		} else {
			require.Len(t, rec.Rows(), 1)
			rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeConnectionChanged)
		}
	}
}

func TestRecordRemoteSessionVerdict_SetupFailure(t *testing.T) {
	t.Parallel()
	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	member := uuid.New()
	service.recordRemoteSessionVerdict(t.Context(), mcpconnectionattempts.FromRequest(nil), gatewayEndpoint(), openState(), remotesessions.Client{}, validationTarget{setupFailed: true, memberID: member}, uuid.New(), remotesessions.ValidationOutcomeUnknown, remotesessionmetrics.ValidationTriggerVerify)
	require.Equal(t, member.String(), rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeMemberToolsMissing).MemberID)
}
