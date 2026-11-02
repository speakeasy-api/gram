package mcp

import (
	"context"
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

	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.ResultAllowed), verdictHappened(remotesessions.ValidationOutcomeValid))
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeRejectedByMember), verdictHappened(remotesessions.ValidationOutcomeRejectedByMember))
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeProviderTokenInactive), verdictHappened(remotesessions.ValidationOutcomeInactive))
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeProviderUnreachable), verdictHappened(remotesessions.ValidationOutcomeUnknown))
}

func TestRecordRemoteSessionVerdict_KeepaliveBelongsToNoAttempt(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	service.recordRemoteSessionVerdict(t.Context(), directEndpoint(), openState(), remotesessions.Client{}, uuid.Nil, uuid.New(), remotesessions.ValidationOutcomeRejectedByMember, remotesessionmetrics.ValidationTriggerKeepalive)
	rec.RequireNothing()
}

func TestTokenClientRefusal(t *testing.T) {
	t.Parallel()

	for reason, what := range map[string]mcpconnectionattempts.Code{
		"cimd_admission_denied":     mcpconnectionattempts.CodeCIMDAdmissionDenied,
		"cimd_admission_disabled":   mcpconnectionattempts.CodeCIMDAdmissionDenied,
		"cimd_metadata_invalid":     mcpconnectionattempts.CodeCIMDMetadataInvalid,
		"ai_tool_blocked":           mcpconnectionattempts.CodeAIToolBlocked,
		"unknown_client_id":         mcpconnectionattempts.CodeUnknownClientID,
		clientIDNamesEndpointReason: mcpconnectionattempts.CodeCIMDMetadataInvalid,
	} {
		rec := mcpconnectionattemptstest.New(t)
		service := &Service{connections: rec.Recorder}
		service.recordTokenClientRefusal(t.Context(), directEndpoint(), oauthwire.GrantTypeAuthorizationCode, "client", reason)
		require.Equal(t, reason, rec.RequireEnded(mcpconnectionattempts.StepInboundToken, what).SubReason)
	}

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	service.recordTokenClientRefusal(t.Context(), directEndpoint(), oauthwire.GrantTypeRefreshToken, "client", "unknown_client_id")
	service.recordTokenClientFailure(t.Context(), directEndpoint(), oauthwire.GrantTypeRefreshToken, mcpmetrics.IDJAGRejectionClientMetadataUnavailable)
	rec.RequireNothing()

	service.recordTokenClientFailure(t.Context(), directEndpoint(), oauthwire.GrantTypeAuthorizationCode, mcpmetrics.IDJAGRejectionClientMetadataUnavailable)
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeCIMDFetchFailed)
}

func TestRecordMemberListing(t *testing.T) {
	t.Parallel()

	listed, signIn, broken, unapproved := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	gate := &metaGateContext{organizationID: "org-test", projectID: uuid.New(), metaServerID: uuid.New(), unapprovedMembers: []metaMember{{serverID: unapproved}}}
	members := []metaMember{{serverID: listed}, {serverID: signIn}}
	results := []memberDescribeResult{
		{catalog: &memberCatalog{}},
		{err: memberCredentialError{&metaMemberError{message: "sign in"}}},
	}

	t.Run("a member needing sign-in leaves the listing Connected", func(t *testing.T) {
		t.Parallel()
		rec := mcpconnectionattemptstest.New(t)
		service := &Service{connections: rec.Recorder}
		service.recordMemberListing(t.Context(), gate, members, results)

		require.Equal(t, listed.String(), rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.ResultListed).MemberID)
		require.Equal(t, signIn.String(), rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNeedsSignIn).MemberID)
		require.Equal(t, unapproved.String(), rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeConsentNotApproved).MemberID)
		row := rec.RequireEnded(mcpconnectionattempts.StepDiscoveryListing, mcpconnectionattempts.ResultComplete)
		require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)
		rec.RequireOneAttempt()
	})
	t.Run("a broken member marks the listing partial", func(t *testing.T) {
		t.Parallel()
		rec := mcpconnectionattemptstest.New(t)
		service := &Service{connections: rec.Recorder}
		service.recordMemberListing(t.Context(), gate, append(members, metaMember{serverID: broken}), append(results, memberDescribeResult{err: errors.New("down")}))
		require.Equal(t, broken.String(), rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeMemberToolsMissing).MemberID)
		rec.RequireEnded(mcpconnectionattempts.StepDiscoveryListing, mcpconnectionattempts.CodeMemberToolsMissing)
	})
	t.Run("a canceled request records nothing", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		rec := mcpconnectionattemptstest.New(t)
		service := &Service{connections: rec.Recorder}
		service.recordMemberListing(ctx, gate, members, results)
		rec.RequireNothing()
	})
	t.Run("a project gateway on the organization route keeps its project", func(t *testing.T) {
		t.Parallel()
		projectGate := *gate
		projectGate.organizationGateway = true
		rec := mcpconnectionattemptstest.New(t)
		service := &Service{connections: rec.Recorder}
		service.recordMemberListing(t.Context(), &projectGate, members, results)
		require.Equal(t, gate.projectID, rec.RequireEnded(mcpconnectionattempts.StepDiscoveryListing, mcpconnectionattempts.ResultComplete).ProjectID)
	})
	t.Run("the organization gateway has no project", func(t *testing.T) {
		t.Parallel()
		orgGate := *gate
		orgGate.organizationGateway = true
		orgGate.projectID = uuid.Nil
		rec := mcpconnectionattemptstest.New(t)
		service := &Service{connections: rec.Recorder}
		service.recordMemberListing(t.Context(), &orgGate, members, results)
		require.Equal(t, uuid.Nil, rec.RequireEnded(mcpconnectionattempts.StepDiscoveryListing, mcpconnectionattempts.ResultComplete).ProjectID)
	})
}

func TestMemberDescribeHappened_NoAccess(t *testing.T) {
	t.Parallel()

	got := memberDescribeHappened(memberDescribeResult{err: oops.E(oops.CodeForbidden, nil, "no")})
	require.Equal(t, mcpconnectionattempts.Happened(mcpconnectionattempts.CodeNoServerAccess), got)
}

func TestFederatedHappened(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpconnectionattempts.CodeConnectionChanged, federatedHappened(remotesessions.ErrFederatedConfiguration))
	require.Equal(t, mcpconnectionattempts.CodeIDPUnavailable, federatedHappened(remotesessions.ErrFederatedUnavailable))
}
