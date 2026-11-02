package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// An outage on our side must never tell a person to sign in again.
func TestGateHappened_OutagesAreNotThePersonsFault(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{"revocation_check_unavailable", "org_lookup_failed", "issuer_lookup_failed", "caller_profile_unavailable", "tool_selection_load_failed", "agent_session_load_failed", "workload_session_load_failed", "workload_admission_load_failed"} {
		require.Equal(t, mcpconnectionattempts.CodeAuthCheckUnavailable, gateHappened(reason), reason)
	}
	require.Equal(t, mcpconnectionattempts.CodeNoServerAccess, gateHappened(issuerGateReasonWorkloadSessionOutOfReach))
	require.Equal(t, mcpconnectionattempts.CodeInvalidBearerToken, gateHappened(issuerGateReasonInvalidBearerToken))
	require.Equal(t, mcpconnectionattempts.CodeInvalidBearerToken, gateHappened(issuerGateReasonIssuerMismatch))
	require.Equal(t, mcpconnectionattempts.CodeAuthCheckUnavailable, gateHappened("a_reason_added_later"))
}

func TestRemoteSessionHappened(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpconnectionattempts.CodeRemoteSessionUnavailable, remoteSessionHappened(issuerGateReasonRemoteSessionUnavailable))
	require.Equal(t, mcpconnectionattempts.CodeCredentialMisconfigured, remoteSessionHappened(issuerGateReasonRemoteSessionMisconfigured))
	require.Equal(t, mcpconnectionattempts.CodeCredentialMisconfigured, remoteSessionHappened(issuerGateReasonClientCredentialMisconfigured))
	require.Equal(t, mcpconnectionattempts.CodeInvalidRemoteSession, remoteSessionHappened(issuerGateReasonInvalidRemoteSession))
	require.Equal(t, mcpconnectionattempts.CodeInvalidRemoteSession, remoteSessionHappened(issuerGateReasonUpstreamSelectionUnavailable))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, remoteSessionHappened("a_reason_added_later"))
}

func TestToolsCallRefusal(t *testing.T) {
	t.Parallel()

	code, ok := toolsCallRefusal(fmt.Errorf("authorize MCP tool call: %w", mcpaccess.ToolPermissionDenied(oops.C(oops.CodeForbidden))))
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeToolPermissionDenied, code)

	code, ok = toolsCallRefusal(oops.E(oops.CodeForbidden, errToolUsageLimitReached, "tool usage limit reached"))
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

func TestServerAccessDenied(t *testing.T) {
	t.Parallel()

	require.True(t, serverAccessDenied(fmt.Errorf("authorize: %w", mcpaccess.ServerPermissionDenied(oops.C(oops.CodeForbidden), "https://app.example.com/access"))))
	require.False(t, serverAccessDenied(oops.C(oops.CodeUnauthorized)))
	require.False(t, serverAccessDenied(errors.New("load grants")))
}

func TestInboundRefreshHappened(t *testing.T) {
	t.Parallel()

	for reason, want := range map[string]mcpconnectionattempts.Code{
		"refresh_token_resource_mismatch":       mcpconnectionattempts.CodeResourceMismatch,
		"refresh_token_client_mismatch":         mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed,
		"refresh_token_expired":                 mcpconnectionattempts.CodeRefreshTokenExpired,
		"sign_in_revoked":                       mcpconnectionattempts.CodeSignInRevoked,
		"refresh_token_revoked":                 mcpconnectionattempts.CodeRefreshTokenRevoked,
		"agent_admission_denied":                mcpconnectionattempts.CodeNoServerAccess,
		"agent_admission_unavailable":           mcpconnectionattempts.CodeInternalError,
		"upstream_reconnect_required":           mcpconnectionattempts.CodeUpstreamReconnectRequired,
		"upstream_ema_refused":                  mcpconnectionattempts.CodeUpstreamEMARefused,
	} {
		code, ok := inboundRefreshHappened(reason)
		require.True(t, ok, reason)
		require.Equal(t, want, code, reason)
	}
	for _, reason := range []string{"refresh_token_replay_context_cancelled", "invalid_request", "resource_mismatch", "refresh_token_unknown_or_already_used"} {
		_, ok := inboundRefreshHappened(reason)
		require.False(t, ok, reason)
	}
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
