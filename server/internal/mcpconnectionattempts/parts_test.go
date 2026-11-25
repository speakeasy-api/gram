package mcpconnectionattempts

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParts_AStepNamesThePartItBroke(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		step Step
		code Code
		want Part
	}{
		{StepInboundAuthorize, CodeRedirectURIMismatch, PartAppSignIn},
		{StepInboundToken, CodeRefreshTokenExpired, PartAppSignIn},
		{StepSignInRenewal, CodeRenewalRefused, PartAppSignIn},
		{StepProviderSignIn, CodeProviderOAuthInvalidClient, PartServiceSignIn},
		{StepProviderRefresh, CodeInvalidGrant, PartServiceSignIn},
		{StepProviderRefresh, CodeProviderUnavailable, PartServiceSignIn},
		{StepUpstreamEMA, CodeUpstreamEMARefused, PartServiceSignIn},
		{StepMCPRequest, CodeProviderRejected, PartServiceSignIn},
		{StepMCPRequest, CodeProviderUnreachable, PartServer},
		{StepDiscoveryMember, CodeTunnelUnavailable, PartServer},
		{StepMCPRequest, CodeInvalidBearerToken, PartAppSignIn},
		{StepInboundToken, CodeUpstreamReconnectRequired, PartServiceSignIn},
		{StepInboundToken, CodeUpstreamEMARefused, PartServiceSignIn},
		{StepInboundToken, CodeUpstreamEMAServiceRefused, PartServiceSignIn},
	} {
		require.Equal(t, c.want, PartOf(c.step.String(), c.code.String()), c.step.String()+" "+c.code.String())
	}
	require.Equal(t, PartAppSignIn, PartOf("a_step_added_later", CodeInternalError.String()))
}

func TestParts_ARequestReasonNamesThePartItBroke(t *testing.T) {
	t.Parallel()
	for _, step := range []Step{StepMCPRequest, StepDiscoveryListing, StepDiscoveryMember} {
		require.Equal(t, PartAppSignIn, PartOf(step.String(), CodeInvalidBearerToken.String()))
		for _, code := range []Code{CodeProviderUnreachable, CodeProviderUnavailable, CodeProviderServerError, CodeTunnelUnavailable, CodeTunnelAtCapacity} {
			require.Equal(t, PartServer, PartOf(step.String(), code.String()), code.String())
		}
		// Reasons that can be one caller's own, which another caller's success says nothing about.
		for _, code := range []Code{CodeKillswitchCheckUnavailable, CodeInvalidRemoteSession, CodeProviderRejected, CodeStoredSignInUnreadable, CodeCredentialMisconfigured, CodeCallerCredentialMissing, CodeProviderRateLimited, CodeInternalError, CodeStaticKeyRejected, CodeSharedCredentialRejected, CodeAddressNotFound, CodeMemberToolsMissing} {
			require.Equal(t, PartServiceSignIn, PartOf(step.String(), code.String()), code.String())
		}
		require.Equal(t, PartServiceSignIn, PartOf(step.String(), "a_reason_added_later"), "an unknown reason is ended only by the person's own success")
	}
}

// A reason only counts as the server not answering on a request or listing.
func TestParts_ASignInStepIsNeverTheServer(t *testing.T) {
	t.Parallel()
	for _, step := range []Step{StepInboundToken, StepProviderRefresh, StepProviderSignIn, StepSignInRenewal} {
		for _, reason := range serverDidNotAnswer {
			require.NotEqual(t, PartServer, PartOf(step.String(), reason), step.String()+" "+reason)
		}
	}
}

func TestParts_TheListedReasonsAreFailedCodes(t *testing.T) {
	t.Parallel()
	for _, reason := range slices.Concat(serverDidNotAnswer, didNotFinish) {
		meaning, ok := MeaningOf(reason)
		require.True(t, ok, reason)
		require.Equal(t, OutcomeFailed, meaning.Outcome, reason)
	}
	require.Empty(t, sharedReasons(serverDidNotAnswer, didNotFinish))
}

func sharedReasons(a, b []string) []string {
	return slices.DeleteFunc(slices.Clone(a), func(reason string) bool { return !slices.Contains(b, reason) })
}

func TestParts_OnlyABrokenConnectionCountsAsFailing(t *testing.T) {
	t.Parallel()
	ending := func(outcome Outcome, code Code) Ending {
		return Ending{Step: StepMCPRequest.String(), Outcome: outcome, Reason: code.String(), At: time.Time{}}
	}
	require.True(t, ending(OutcomeFailed, CodeRefreshTokenExpired).CountsAsFailing())
	require.True(t, ending(OutcomeFailed, CodeProviderUnreachable).CountsAsFailing())
	require.False(t, ending(OutcomeFailed, CodeExpired).CountsAsFailing(), "the person did not finish signing in")
	require.False(t, ending(OutcomeFailed, CodeLoginExpired).CountsAsFailing(), "the person did not finish signing in")
	require.False(t, ending(OutcomeBlocked, CodePolicyBlocked).CountsAsFailing())
	require.False(t, ending(OutcomeDeclined, CodeProviderDenied).CountsAsFailing())
	require.False(t, Ending{Step: StepInboundSignIn.String(), Outcome: OutcomeAbandoned, Reason: "", At: time.Time{}}.CountsAsFailing())
}

// callerOwnOnRequest are the failed reasons a request or listing can carry for one
// caller alone, so only that caller's own success ends them.
var callerOwnOnRequest = []Code{
	CodeCustomDomainNotReady, CodeIDPUnavailable, CodeAIToolCheckUnavailable, CodeCIMDFetchFailed, CodeCIMDMetadataInvalid,
	CodeRegistrationInvalid, CodeUnknownClientID, CodeRedirectURIMismatch, CodeResourceMismatch, CodePKCEMismatch,
	CodeCodeNotFoundOrExpired, CodeServerNotFound, CodeSessionChanged, CodeClientAuthFailed, CodeSignInProviderError,
	CodeConnectionCatalogExpired, CodeInvalidRequest, CodeResourceNotAvailable, CodeAssertionInvalid, CodeAssertionOther,
	CodeUpstreamCredentialChoice, CodeUpstreamSignInNotSetUp, CodeIssuerNotTrusted, CodeAssertionVerifierMisconfigured,
	CodeAssertionCheckUnavailable, CodeProviderOAuthInvalidClient, CodeProviderOAuthInvalidScope, CodeProviderOAuthInvalidGrant,
	CodeIssuerMismatch, CodeConnectionChanged, CodeUnrenewableToken, CodeRejectedByMember, CodeProviderRateLimited,
	CodeSharedCredentialRejected, CodeStaticKeyRejected, CodeConfigurationRequired, CodeInvalidTarget, CodeMalformedAssertion,
	CodeInsufficientScope, CodeMalformedResponse, CodeEMAProviderRejected, CodeTransientFailure, CodeIDPTransientFailure,
	CodeProviderTransientFailure, CodeReauthenticationRequired, CodeInvalidRemoteSession, CodeCredentialMisconfigured,
	CodeRemoteSessionUnavailable, CodeProviderRejected, CodeCallerCredentialMissing, CodeMemberToolsMissing,
	CodeRefreshTokenExpired, CodeSignInRevoked, CodeRefreshTokenRevoked, CodeRefreshTokenUnknownOrAlreadyUsed,
	CodeUpstreamReconnectRequired, CodeUpstreamEMARefused, CodeInvalidGrant, CodeRefreshInvalidClient, CodeNoGrant,
	CodeRefreshRejected, CodeInternalError, CodeOfflineNotRequested, CodeOfflineUnsupported, CodeRenewalRefused,
	CodeRenewalMisconfigured, CodeInvalidIdentity, CodeRenewalRetryable, CodeRenewalAmbiguous, CodeCatalogTooLarge,
	CodeApprovalNeedsAdmin, CodeRefreshTokenClientMismatch, CodeRefreshTokenUsedByOtherApp, CodeStoredSessionInvalid,
	CodeStoredSignInUnreadable, CodeUpstreamCredentialNotChosen, CodeSignInMethodNotAccepted, CodeCredentialChoiceInvalid,
	CodeAddressNotAllowed, CodeAddressNotFound, CodeKillswitchCheckUnavailable,
	CodeUpstreamEMAIdentityProviderRefused, CodeUpstreamEMAServiceRefused, CodeUpstreamEMANotRenewed,
}

// A new failed reason must be placed: the server not answering anyone, or one caller's own.
func TestParts_EveryFailedReasonIsClassifiedForRequests(t *testing.T) {
	t.Parallel()
	classified := slices.Concat(serverDidNotAnswer, didNotFinish, []string{CodeInvalidBearerToken.String()})
	for _, code := range callerOwnOnRequest {
		require.NotContains(t, classified, code.String(), "classified twice")
		classified = append(classified, code.String())
	}
	for name, meaning := range catalog {
		if meaning.Outcome == OutcomeFailed {
			require.Contains(t, classified, name, "add it to serverDidNotAnswer, or to callerOwnOnRequest")
		}
	}
	for _, name := range classified {
		meaning, ok := MeaningOf(name)
		require.True(t, ok, name)
		require.Equal(t, OutcomeFailed, meaning.Outcome, name)
	}
}

func TestParts_GraceFollowsWhoFixesTheReason(t *testing.T) {
	t.Parallel()
	require.Equal(t, GraceRetry, GraceOf(CodeRefreshTokenExpired.String()))
	require.Equal(t, GraceRetry, GraceOf(CodeRedirectURIMismatch.String()))
	require.Equal(t, GraceOutage, GraceOf(CodeProviderUnavailable.String()))
	require.Equal(t, GraceOutage, GraceOf(CodeAuthCheckUnavailable.String()))
	require.Equal(t, GraceNone, GraceOf(CodeStaticKeyRejected.String()))
	require.Equal(t, GraceRetry, GraceOf("a_reason_added_later"))
}
