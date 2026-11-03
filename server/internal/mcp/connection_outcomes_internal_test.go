package mcp

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

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
	gap := authClientResolution{outcome: authClientGap, reason: authClientReasonNoAuthStale, clientID: uuid.New()}
	view := &gatewayConsentView{
		members: []metaMember{{serverID: stale, authClient: &gap}},
		Servers: []gatewayServer{
			{ServerID: connected.String(), Status: gatewayServerConnected},
			{ServerID: stale.String(), Status: gatewayServerReconnect},
			{ServerID: unset.String(), Status: gatewayServerNotSetUp},
		},
	}

	service.recordConsentPage(t.Context(), nil, gatewayEndpoint(), openState(), view, nil, nil)

	require.Equal(t, connected.String(), rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.ResultAllowed).MemberID)
	row := rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNeedsSignIn)
	require.Equal(t, stale.String(), row.MemberID)
	require.Equal(t, authClientReasonNoAuthStale, row.SubReason, "the gap that was only a debug log")
	require.Equal(t, gap.clientID.String(), row.AuthClientID)
	require.Equal(t, unset.String(), rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeUpstreamSignInNotSetUp).MemberID)
	rec.RequireNotEnded()
}

func TestRecordConsentPage_ProblemIsAStepOnTheAttempt(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	service.recordConsentPage(t.Context(), nil, directEndpoint(), openState(), nil, []remoteSessionCard{{}}, mcpconnectionattempts.CodeNoServerAccess)

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
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeMemberToolsMissing), verdictHappened(remotesessions.ValidationOutcomeUnknown, false))
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeTunnelUnavailable), verdictHappened(remotesessions.ValidationOutcomeUnknown, true))
}

func TestRecordRemoteSessionVerdict_KeepaliveBelongsToNoAttempt(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	service.recordRemoteSessionVerdict(t.Context(), directEndpoint(), openState(), remotesessions.Client{}, validationTarget{}, uuid.New(), remotesessions.ValidationOutcomeRejectedByMember, remotesessionmetrics.ValidationTriggerKeepalive)
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
		service.recordTokenClientRefusal(t.Context(), directEndpoint(), oauthwire.GrantTypeAuthorizationCode, "client", reason)
		require.Equal(t, reason, rec.RequireEnded(mcpconnectionattempts.StepInboundToken, what).SubReason)
	}

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	code := oauthwire.GrantTypeAuthorizationCode
	for _, reason := range []string{"missing_client_id", "unknown_client_id", clientIDNamesEndpointReason} {
		service.recordTokenClientRefusal(t.Context(), directEndpoint(), code, "client", reason)
	}
	service.recordTokenClientRefusal(t.Context(), directEndpoint(), code, "", "assertion_malformed")
	service.recordTokenClientRefusal(t.Context(), directEndpoint(), oauthwire.GrantTypeRefreshToken, "client", "client_secret_mismatch")
	service.recordTokenClientFailure(t.Context(), directEndpoint(), oauthwire.GrantTypeRefreshToken, "https://client.example.com/app.json", mcpmetrics.IDJAGRejectionClientCheckUnavailable)
	rec.RequireNothing()

	service.recordTokenClientFailure(t.Context(), directEndpoint(), code, "https://client.example.com/app.json", mcpmetrics.IDJAGRejectionClientCheckUnavailable)
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeAIToolCheckUnavailable)
	service.recordTokenClientFailure(t.Context(), directEndpoint(), code, "https://client.example.com/app.json", mcpmetrics.IDJAGRejectionClientMetadataUnavailable)
	require.Equal(t, "client.example.com", rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeCIMDFetchFailed).AppKey)
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

	waiting := &gatewayConsentView{Servers: []gatewayServer{{Status: gatewayServerNotConnected}, {Status: gatewayServerReconnect}}}
	require.Equal(t, mcpconnectionattempts.CodeNeedsSignIn, noReadyServerHappened(waiting), "a first-time gateway connect blames nobody")
	broken := &gatewayConsentView{Servers: []gatewayServer{{Status: gatewayServerNotConnected}, {Status: gatewayServerUnavailable}}}
	require.Equal(t, mcpconnectionattempts.CodeMemberToolsMissing, noReadyServerHappened(broken))
}

func TestRemoteLoginRefusalHappened(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpconnectionattempts.CodeSessionChanged, remoteLoginRefusalHappened(oops.E(oops.CodeUnauthorized, nil, "wrong browser")))
	require.Equal(t, mcpconnectionattempts.CodeSessionChanged, remoteLoginRefusalHappened(errors.New("connection login does not match its link")))
	require.Equal(t, mcpconnectionattempts.CodeNoServerAccess, remoteLoginRefusalHappened(errRecoveryNoAccess))
	require.Equal(t, mcpconnectionattempts.CodeNoServerAccess, remoteLoginRefusalHappened(oops.E(oops.CodeForbidden, nil, "no")))
	require.Equal(t, mcpconnectionattempts.CodeConnectionChanged, remoteLoginRefusalHappened(errRecoveryUnavailable))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, remoteLoginRefusalHappened(oops.E(oops.CodeUnavailable, nil, "redis down")))
}

func TestFederatedHappened(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpconnectionattempts.CodeConnectionChanged, federatedHappened(remotesessions.ErrFederatedConfiguration))
	require.Equal(t, mcpconnectionattempts.CodeIDPTransientFailure, federatedHappened(remotesessions.ErrFederatedUnavailable))
	require.Equal(t, mcpconnectionattempts.CodeSignInProviderError, federatedHappened(remotesessions.ErrFederatedIdentity))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, federatedHappened(remotesessions.ErrFederatedSigning))
}
