package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// An outage on our side must never tell a person to sign in again.
func TestGateHappened(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want mcpconnectionattempts.Code
	}{
		{name: "revocation store down", err: fmt.Errorf("validate: %w", sessiontokens.ErrRevocationUnavailable), want: mcpconnectionattempts.CodeAuthCheckUnavailable},
		{name: "org lookup failed", err: fmt.Errorf("%w: database", errIssuerGateOrgLookup), want: mcpconnectionattempts.CodeAuthCheckUnavailable},
		{name: "an admission store outage", err: errors.New("load agent admission"), want: mcpconnectionattempts.CodeAuthCheckUnavailable},
		{name: "rejected credential", err: fmt.Errorf("%w: bad signature", errCredentialRejected), want: mcpconnectionattempts.CodeInvalidBearerToken},
		{name: "unauthorized", err: oops.C(oops.CodeUnauthorized), want: mcpconnectionattempts.CodeInvalidBearerToken},
		{name: "forbidden", err: oops.C(oops.CodeForbidden), want: mcpconnectionattempts.CodeNoServerAccess},
		{name: "out of reach", err: errWorkloadSessionOutOfReach, want: mcpconnectionattempts.CodeNoServerAccess},
		{name: "session row gone", err: fmt.Errorf("%w: %w", errToolSelectionLoad, errSessionPolicyGone), want: mcpconnectionattempts.CodeInvalidBearerToken},
		{name: "session store down", err: fmt.Errorf("%w: database", errToolSelectionLoad), want: mcpconnectionattempts.CodeAuthCheckUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, gateHappened(tc.err))
		})
	}
}

func TestAuthClientGapHappened(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpconnectionattempts.CodeCredentialMisconfigured, authClientGapHappened(authClientReasonSetup))
	require.Equal(t, mcpconnectionattempts.CodeInvalidRemoteSession, authClientGapHappened("no_auth_server_has_clients"))
}

// Both ways of reading a gateway must agree on which rows lose their project.
func TestGateConnectionScope(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	gateway := uuid.New()
	organizationGate := &metaGateContext{organizationID: "org", metaServerID: gateway, organizationGateway: true}
	rec.Recorder.Once(t.Context(), organizationGate.connectionScope(t.Context()), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected)
	require.Equal(t, uuid.Nil, rec.Rows()[0].ProjectID)
	require.Equal(t, gateway.String(), rec.Rows()[0].ServerID)

	project := uuid.New()
	projectGate := &metaGateContext{organizationID: "org", projectID: project, metaServerID: gateway}
	rec.Recorder.Once(t.Context(), projectGate.connectionScope(t.Context()), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected)
	require.Equal(t, project, rec.Rows()[1].ProjectID)
}

// A provider outage while resolving must not read as the person's fault.
func TestMemberNoCredentialHappened(t *testing.T) {
	t.Parallel()

	issuer := uuid.New()
	member := metaMember{remoteSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true}}
	gate := &metaGateContext{}
	require.Equal(t, mcpconnectionattempts.CodeInvalidRemoteSession, gate.memberNoCredentialHappened(member))

	skipped := new(remotesessions.SkippedClients)
	gate.authentication = &issuerGateAuthentication{skippedUpstreams: skipped}
	skipped.Note(issuer, remotesessions.ErrRemoteSessionUnavailable)
	require.Equal(t, mcpconnectionattempts.CodeRemoteSessionUnavailable, gate.memberNoCredentialHappened(member))

	skipped.Note(issuer, remotesessions.ErrRemoteSessionMisconfigured)
	require.Equal(t, mcpconnectionattempts.CodeCredentialMisconfigured, gate.memberNoCredentialHappened(member))

	setup := authClientResolution{outcome: authClientGap, reason: authClientReasonSetup}
	require.Equal(t, mcpconnectionattempts.CodeCredentialMisconfigured, (&metaGateContext{}).memberNoCredentialHappened(metaMember{authClient: &setup}))
}

func TestToolsCallRefusal(t *testing.T) {
	t.Parallel()

	code, ok := toolsCallRefusal(fmt.Errorf("authorize MCP tool call: %w", mcpaccess.ToolPermissionDenied(oops.C(oops.CodeForbidden))))
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeToolPermissionDenied, code)

	code, ok = toolsCallRefusal(oops.E(oops.CodeForbidden, remotemcp.ErrToolUsageLimitReached, "tool usage limit reached"))
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeUsageLimitReached, code)

	code, ok = toolsCallRefusal(oops.E(oops.CodeForbidden, errRiskPolicyDenied, "blocked"))
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodePolicyBlocked, code)

	_, ok = toolsCallRefusal(nil)
	require.False(t, ok)
	_, ok = toolsCallRefusal(errors.New("tool execution failed"))
	require.False(t, ok)
}

func TestKillswitchHappened(t *testing.T) {
	t.Parallel()

	paused := &oops.MCPError{ID: mcpjsonrpc.ID{Number: 0, String: ""}, Code: oops.MCPCodeForbidden, Message: "paused", Data: &oops.MCPErrorData{Code: oops.MCPErrorDataCodeToolCallsPaused, Supported: nil, Requested: "", RequiredCapabilities: nil}}
	require.Equal(t, mcpconnectionattempts.CodeMCPToolCallsPaused, killswitchHappened(paused))
	require.Equal(t, mcpconnectionattempts.CodeKillswitchCheckUnavailable, killswitchHappened(errors.New("evaluate hosted MCP kill switch")))
}

func TestInboundRefreshHappened(t *testing.T) {
	t.Parallel()

	for reason, want := range map[string]mcpconnectionattempts.Code{
		"refresh_token_resource_mismatch": mcpconnectionattempts.CodeResourceMismatch,
		"refresh_token_client_mismatch":   mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed,
		"refresh_token_expired":           mcpconnectionattempts.CodeRefreshTokenExpired,
		"sign_in_revoked":                 mcpconnectionattempts.CodeSignInRevoked,
		"refresh_token_revoked":           mcpconnectionattempts.CodeRefreshTokenRevoked,
		"agent_admission_denied":          mcpconnectionattempts.CodeNoServerAccess,
		"agent_admission_unavailable":     mcpconnectionattempts.CodeInternalError,
		"upstream_reconnect_required":     mcpconnectionattempts.CodeUpstreamReconnectRequired,
		"upstream_ema_refused":            mcpconnectionattempts.CodeUpstreamEMARefused,
	} {
		code, ok := inboundRefreshHappened(reason)
		require.True(t, ok, reason)
		require.Equal(t, want, code, reason)
	}
	for _, reason := range []string{"invalid_request", "resource_mismatch", "refresh_token_unknown_or_already_used"} {
		_, ok := inboundRefreshHappened(reason)
		require.False(t, ok, reason)
	}
	code, ok := inboundRefreshHappened("a_reason_added_later")
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code, "an unknown reason is ours")
}

// The wrapper carries a code without changing what the gateway answers or how it is matched.
func TestMemberConnectionFailureKeepsTextAndType(t *testing.T) {
	t.Parallel()

	inner := memberCredentialError{&metaMemberError{message: "server is not connected"}}
	var err error = memberConnectionError{inner, mcpconnectionattempts.CodeInvalidRemoteSession}
	require.EqualError(t, err, "server is not connected")
	_, credential := errors.AsType[memberCredentialError](err)
	require.True(t, credential)
	memberErr, ok := errors.AsType[*metaMemberError](err)
	require.True(t, ok)
	require.Equal(t, "server is not connected", memberErr.message)
}

type stubMemberOutcome struct {
	err      error
	rejected int
	code     int
}

func (o stubMemberOutcome) failure() error { return o.err }

func (o stubMemberOutcome) rejection() (int, bool) { return o.rejected, o.rejected != 0 }

func (o stubMemberOutcome) status() int { return o.code }

func TestMemberClientFailure_CarriesWhatHappened(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		outcome stubMemberOutcome
		dial    memberDial
		want    mcpconnectionattempts.Code
	}{
		{name: "person's credential rejected", outcome: stubMemberOutcome{rejected: http.StatusUnauthorized}, want: mcpconnectionattempts.CodeProviderRejected},
		{name: "no credential to send", outcome: stubMemberOutcome{rejected: http.StatusUnauthorized}, dial: memberDial{anonymous: true}, want: mcpconnectionattempts.CodeInvalidRemoteSession},
		{name: "configured key rejected", outcome: stubMemberOutcome{rejected: http.StatusUnauthorized}, dial: memberDial{anonymous: true, configuredHeaders: true}, want: mcpconnectionattempts.CodeCredentialMisconfigured},
		{name: "tunnel down", outcome: stubMemberOutcome{code: http.StatusBadGateway}, dial: memberDial{tunneled: true}, want: mcpconnectionattempts.CodeTunnelUnavailable},
		{name: "server's own credential rejected", outcome: stubMemberOutcome{rejected: http.StatusForbidden}, dial: memberDial{clientCredential: true}, want: mcpconnectionattempts.CodeCredentialMisconfigured},
		{name: "no answer", outcome: stubMemberOutcome{err: errors.New("dial failed")}, want: mcpconnectionattempts.CodeProviderUnreachable},
		{name: "no route", outcome: stubMemberOutcome{err: fmt.Errorf("%w: %w", errMemberUnroutable, errTunnelNoLiveRoute)}, want: mcpconnectionattempts.CodeTunnelUnavailable},
		{name: "route store down", outcome: stubMemberOutcome{err: fmt.Errorf("%w: store unavailable", errMemberUnroutable)}, want: mcpconnectionattempts.CodeInternalError},
		{name: "rate limited", outcome: stubMemberOutcome{code: http.StatusTooManyRequests}, want: mcpconnectionattempts.CodeProviderRateLimited},
		{name: "server error", outcome: stubMemberOutcome{code: http.StatusInternalServerError}, want: mcpconnectionattempts.CodeProviderServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := memberClientFailure(t.Context(), testenv.NewLogger(t), tc.outcome, tc.dial, metaMember{slug: "fixture"}, errors.New("call failed"))
			failure, ok := errors.AsType[memberConnectionError](err)
			require.True(t, ok, err)
			require.Equal(t, tc.want, failure.code)
		})
	}
}

// A caller that went away says nothing about the member.
func TestMemberClientFailure_CanceledCallerHasNoCode(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := memberClientFailure(ctx, testenv.NewLogger(t), stubMemberOutcome{}, memberDial{}, metaMember{slug: "fixture"}, context.Canceled)
	_, ok := errors.AsType[memberConnectionError](err)
	require.False(t, ok)
}

func TestRecordMemberFailure(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder}
	gate := &metaGateContext{organizationID: "org", projectID: uuid.New(), metaServerID: uuid.New()}
	first, second := metaMember{serverID: uuid.New()}, metaMember{serverID: uuid.New()}

	service.recordMemberFailure(t.Context(), gate, first, mcpconnectionattempts.CodeProviderServerError)
	service.recordMemberFailure(t.Context(), gate, second, mcpconnectionattempts.CodeProviderServerError)
	require.Len(t, rec.Rows(), 2, "two members with one code are two rows")
	require.NotEqual(t, rec.Rows()[0].MemberID, rec.Rows()[1].MemberID)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service.recordMemberFailure(ctx, gate, first, mcpconnectionattempts.CodeProviderServerError)
	require.Len(t, rec.Rows(), 2, "a caller that went away says nothing")
}
