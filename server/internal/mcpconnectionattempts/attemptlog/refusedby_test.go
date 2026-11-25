package attemptlog_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/attemptlog"
)

// Readiness counts a refusal for the same party Connection Logs show as its source.
func TestRefusedByMatchesConnectionLogsSource(t *testing.T) {
	t.Parallel()
	counted := 0
	for _, code := range []mcpconnectionattempts.Code{
		mcpconnectionattempts.CodeConfigurationRequired, mcpconnectionattempts.CodeAccessDenied, mcpconnectionattempts.CodeScopePolicyDenied,
		mcpconnectionattempts.CodeInvalidTarget, mcpconnectionattempts.CodeMalformedAssertion, mcpconnectionattempts.CodeInsufficientScope,
		mcpconnectionattempts.CodeMalformedResponse, mcpconnectionattempts.CodeEMAProviderRejected, mcpconnectionattempts.CodeReauthenticationRequired,
		mcpconnectionattempts.CodeIneligibleIdentity, mcpconnectionattempts.CodeUnmanagedAccount, mcpconnectionattempts.CodeInvalidRemoteSession,
		mcpconnectionattempts.CodeUpstreamEMAIdentityProviderRefused, mcpconnectionattempts.CodeUpstreamEMAServiceRefused,
		mcpconnectionattempts.CodeUpstreamEMARefused, mcpconnectionattempts.CodeUpstreamEMANotRenewed,
	} {
		source := attemptlog.SourceOf(code.String(), attemptlog.OutcomeFailed)
		switch code.RefusedBy() {
		case mcpconnectionattempts.RefusedByIdentityProvider:
			counted++
			require.Equal(t, attemptlog.SourceIdentityProvider, source, code.String())
		case mcpconnectionattempts.RefusedByService:
			counted++
			require.Equal(t, attemptlog.SourceService, source, code.String())
		case "":
			if code == mcpconnectionattempts.CodeInvalidTarget {
				// The sign-in service returned it, but it is a setup problem, not a refusal.
				require.Equal(t, attemptlog.SourceIdentityProvider, source)
				continue
			}
			require.NotEqual(t, attemptlog.SourceIdentityProvider, source, "%s is the identity provider's but not counted", code.String())
		}
	}
	require.Equal(t, 5, counted)
}
