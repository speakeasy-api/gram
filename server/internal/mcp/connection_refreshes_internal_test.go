package mcp

import (
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUnconfirmedRefreshCheck(t *testing.T) {
	t.Parallel()

	for _, reason := range []identitychaining.Reason{identitychaining.ReasonTransientFailure, identitychaining.ReasonProviderUnavailable, identitychaining.ReasonUnknownRejection} {
		require.Equal(t, upstreamRefreshCheckFailed, unconfirmedRefreshCheck(reason), reason)
	}
	for _, reason := range []identitychaining.Reason{identitychaining.ReasonConfigurationRequired, identitychaining.ReasonStaleConfiguration, identitychaining.ReasonInvalidClient} {
		require.Equal(t, upstreamRefreshUnverified, unconfirmedRefreshCheck(reason), reason)
	}
}

func TestInboundRefreshHappened(t *testing.T) {
	t.Parallel()

	for reason, want := range map[string]mcpconnectionattempts.Code{
		"refresh_token_resource_mismatch":       mcpconnectionattempts.CodeResourceMismatch,
		"refresh_token_unknown_or_already_used": mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed,
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
	for _, reason := range []string{"invalid_request", "resource_mismatch"} {
		_, ok := inboundRefreshHappened(reason)
		require.False(t, ok, reason)
	}
	code, ok := inboundRefreshHappened("a_reason_added_later")
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code, "an unknown reason is ours")
}

// The wrapper carries a code without changing what the gateway answers or how it is matched.
