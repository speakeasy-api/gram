package mcp

import (
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUnconfirmedRefreshCheck(t *testing.T) {
	t.Parallel()

	for _, reason := range []identitychaining.Reason{identitychaining.ReasonTransientFailure, identitychaining.ReasonProviderUnavailable, identitychaining.ReasonUnknownRejection} {
		require.Equal(t, upstreamRefreshCheckFailed, unconfirmedRefreshCheck(reason), reason)
	}
	for _, reason := range []identitychaining.Reason{
		identitychaining.ReasonConfigurationRequired,
		identitychaining.ReasonStaleConfiguration,
		identitychaining.ReasonBindingNotReady,
		identitychaining.ReasonInvalidTarget,
		identitychaining.ReasonScopePolicyDenied,
		identitychaining.ReasonAccessDenied,
		identitychaining.ReasonInvalidClient,
		identitychaining.ReasonMalformedAssertion,
		identitychaining.ReasonInsufficientScope,
		identitychaining.ReasonIneligibleIdentity,
	} {
		require.Equal(t, upstreamRefreshUnverified, unconfirmedRefreshCheck(reason), reason)
	}
}

func TestInboundRefreshHappened(t *testing.T) {
	t.Parallel()

	for reason, want := range map[string]mcpconnectionattempts.Code{
		"resource_mismatch":                     mcpconnectionattempts.CodeResourceMismatch,
		"agent_credential_invalid":              mcpconnectionattempts.CodeRefreshTokenRevoked,
		"refresh_token_resource_mismatch":       mcpconnectionattempts.CodeResourceMismatch,
		"refresh_token_unknown_or_already_used": mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed,
		"refresh_token_client_mismatch":         mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed,
		"refresh_token_expired":                 mcpconnectionattempts.CodeRefreshTokenExpired,
		"sign_in_revoked":                       mcpconnectionattempts.CodeSignInRevoked,
		"refresh_token_revoked":                 mcpconnectionattempts.CodeRefreshTokenRevoked,
		"refresh_token_replay_session_mismatch": mcpconnectionattempts.CodeRefreshTokenRevoked,
		"refresh_token_replay_rejected":         mcpconnectionattempts.CodeRefreshTokenRevoked,
		"tool_selection_malformed":              mcpconnectionattempts.CodeRefreshTokenRevoked,
		"tool_selection_resource_mismatch":      mcpconnectionattempts.CodeRefreshTokenRevoked,
		"agent_admission_denied":                mcpconnectionattempts.CodeNoServerAccess,
		"agent_admission_unavailable":           mcpconnectionattempts.CodeInternalError,
		"upstream_reconnect_required":           mcpconnectionattempts.CodeUpstreamReconnectRequired,
		"upstream_ema_refused":                  mcpconnectionattempts.CodeUpstreamEMARefused,
	} {
		code, ok := inboundRefreshHappened(reason)
		require.True(t, ok, reason)
		require.Equal(t, want, code, reason)
	}
	for _, reason := range []string{"invalid_request"} {
		_, ok := inboundRefreshHappened(reason)
		require.False(t, ok, reason)
	}
	code, ok := inboundRefreshHappened("a_reason_added_later")
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code, "an unknown reason is ours")
}

// The wrapper carries a code without changing what the gateway answers or how it is matched.

func TestAgentRefreshAdmissionReason(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err    error
		reason string
	}{
		{oops.C(oops.CodeUnauthorized), "agent_credential_invalid"},
		{oops.C(oops.CodeForbidden), "agent_admission_denied"},
		{errors.New("dependency unavailable"), "agent_admission_unavailable"},
		{oops.C(oops.CodeUnexpected), "agent_admission_unavailable"},
	} {
		require.Equal(t, tc.reason, agentAdmissionRejection(tc.err))
	}
}

func TestReplayLookupFailureReason(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"refresh_token_revoked", "sign_in_revoked"} {
		require.Equal(t, missing, replayLookupFailureReason(fmt.Errorf("lookup: %w", pgx.ErrNoRows), missing))
		require.Equal(t, "refresh_token_replay_lookup_failed", replayLookupFailureReason(errors.New("database unavailable"), missing))
	}
}
