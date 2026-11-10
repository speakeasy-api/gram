package mcpconnectionattempts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// touchedOnlyFixes are the admin-fixable reasons left out of adminFixes, and why. A
// new admin-fixable reason fails TestEveryAdminReasonIsDecided until it is added here
// or to adminFixes.
var touchedOnlyFixes = map[string]string{
	// Fixed on the server, gateway, auth client or issuer the attempts used.
	CodeEndpointNotAgentAddressable.String(): "the gateway's settings",
	CodeUpstreamSignInNotSetUp.String():      "the server's auth clients, which the success touches",
	CodeProviderOAuthInvalidClient.String():  "the auth client",
	CodeProviderOAuthInvalidScope.String():   "the auth client",
	CodeIssuerMismatch.String():              "the auth client or issuer",
	CodeRejectedByMember.String():            "the gateway member",
	CodeSharedCredentialRejected.String():    "the server's shared credential",
	CodeStaticKeyRejected.String():           "the server's key",
	CodeConfigurationRequired.String():       "the server's sign-in setup",
	CodeInvalidTarget.String():               "the server's connection in the sign-in service",
	CodeMalformedAssertion.String():          "the server's sign-in setup",
	CodeInsufficientScope.String():           "the server's allowed permissions",
	CodeMalformedResponse.String():           "the sign-in setup",
	CodeEMAProviderRejected.String():         "the auth client",
	CodeCredentialMisconfigured.String():     "the server's connection settings",
	CodeInvalidIdentity.String():             "the issuer",
	CodeCatalogTooLarge.String():             "the server's or gateway's tools",
	CodeMemberToolsMissing.String():          "the gateway members",

	// Fixed outside Speakeasy, where we see no change, so the fix stays unattributed.
	CodeAccessDenied.String():        "an assignment in the company sign-in service",
	CodeScopePolicyDenied.String():   "a policy in the company sign-in service",
	CodeTunnelUnavailable.String():   "restarting the customer's tunnel",
	CodeIneligibleIdentity.String():  "another account, which is another person",
	CodeNetworkDenied.String():       "not recorded yet; when it is, decide whether moving networks counts as a retry",
	CodeRefreshedUnverified.String(): "a connected result, never a failure to fix",
}

func TestAdminFixesAreAdminReasons(t *testing.T) {
	t.Parallel()
	for reason := range adminFixes {
		meaning, ok := MeaningOf(reason)
		require.True(t, ok, reason)
		require.Equal(t, WhoAdmin, meaning.WhoFixes, reason)
		require.NotContains(t, touchedOnlyFixes, reason, "listed twice")
	}
	for reason := range touchedOnlyFixes {
		meaning, ok := MeaningOf(reason)
		require.True(t, ok, reason)
		require.Equal(t, WhoAdmin, meaning.WhoFixes, reason)
	}
}

func TestEveryAdminReasonIsDecided(t *testing.T) {
	t.Parallel()
	for reason, meaning := range catalog {
		if meaning.WhoFixes != WhoAdmin {
			continue
		}
		_, fixed := adminFixes[reason]
		_, touched := touchedOnlyFixes[reason]
		require.True(t, fixed || touched, "%s is admin-fixable: add it to adminFixes or touchedOnlyFixes", reason)
	}
}
