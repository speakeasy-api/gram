package mcpconnectionattempts

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParts_AStepNamesThePartItBroke(t *testing.T) {
	t.Parallel()
	for _, step := range allSteps {
		want := PartAppSignIn
		switch step {
		case StepProviderSignIn, StepProviderToken, StepProviderRefresh, StepUpstreamEMA, StepMCPRequest, StepDiscoveryListing, StepDiscoveryMember:
			want = PartServiceSignIn
		case StepInboundAuthorize, StepInboundSignIn, StepInboundConsent, StepInboundToken, StepSignInRenewal:
		}
		require.Equal(t, want, PartOf(step.String(), CodeInternalError.String()), step.String())
		require.Contains(t, allParts, PartOf(step.String(), "a_reason_added_later"))
	}
}

func TestParts_ARequestReasonNamesThePartItBroke(t *testing.T) {
	t.Parallel()
	for _, step := range []Step{StepMCPRequest, StepDiscoveryListing, StepDiscoveryMember} {
		require.Equal(t, PartAppSignIn, PartOf(step.String(), CodeInvalidBearerToken.String()))
		for _, code := range []Code{CodeKillswitchCheckUnavailable, CodeProviderUnreachable, CodeProviderUnavailable, CodeProviderServerError, CodeTunnelUnavailable, CodeTunnelAtCapacity} {
			require.Equal(t, PartServer, PartOf(step.String(), code.String()), code.String())
		}
		// Reasons that can be one caller's own, which another caller's success says nothing about.
		for _, code := range []Code{CodeInvalidRemoteSession, CodeProviderRejected, CodeStoredSignInUnreadable, CodeCredentialMisconfigured, CodeCallerCredentialMissing, CodeProviderRateLimited, CodeInternalError, CodeStaticKeyRejected, CodeSharedCredentialRejected, CodeAddressNotFound, CodeAuthCheckUnavailable, CodeMemberToolsMissing} {
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
	for name, meaning := range catalog {
		e := Ending{Step: StepMCPRequest.String(), Outcome: meaning.Outcome, Reason: name, At: time.Time{}}
		want := meaning.Outcome == OutcomeFailed && name != CodeExpired.String() && name != CodeLoginExpired.String()
		require.Equal(t, want, e.CountsAsFailing(), name)
	}
	require.False(t, Ending{Step: StepInboundSignIn.String(), Outcome: OutcomeAbandoned, Reason: "", At: time.Time{}}.CountsAsFailing())
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
