package identitychaining

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// Only a transient failure that never reached the provider is repeated.
func TestOutcomeSafeToRepeat(t *testing.T) {
	t.Parallel()

	transient := Outcome{Stage: StageRedemption, Reason: ReasonTransientFailure, Confidence: ConfidenceInferred, Retryable: true, Cached: false, Provider: "", Unavailable: "", Submitted: false, Failures: nil, providerDescription: ""}
	submitted, cached, stale, rejected := transient, transient, transient, transient
	submitted.Submitted = true
	cached.Cached = true
	stale.Reason = ReasonStaleConfiguration
	rejected.Retryable = false

	require.True(t, transient.SafeToRepeat())
	require.False(t, submitted.SafeToRepeat(), "a submitted exchange may have minted a grant")
	require.False(t, cached.SafeToRepeat(), "a cached failure answers without the provider")
	require.False(t, stale.SafeToRepeat(), "stale configuration is retryable but not repeatable")
	require.False(t, rejected.SafeToRepeat())
}

func TestExhaustedOutcomeIsNotSafeToRepeat(t *testing.T) {
	t.Parallel()

	safe := newOutcome(StageRedemption, ReasonTransientFailure, ConfidenceInferred, true)
	submitted := safe
	submitted.Submitted = true
	for _, failures := range [][]Outcome{{safe, submitted}, {submitted, safe}, {safe, safe}} {
		outcome := exhausted(failures)
		require.False(t, outcome.SafeToRepeat(), "a combined outcome omits individual submission details")
	}
	require.True(t, exhausted([]Outcome{safe}).SafeToRepeat(), "a single provider retains its complete outcome")
}

// Only a kept sign-in paused after a refusal reads as retained.
func TestDelegationOutcomeRetained(t *testing.T) {
	t.Parallel()

	kept := delegationOutcome(remotesessions.ErrDelegationRetainedConfiguration)
	require.Equal(t, ReasonConfigurationRequired, kept.Reason)
	require.True(t, kept.Retained)

	noToken := delegationOutcome(remotesessions.ErrDelegationConfiguration)
	require.Equal(t, ReasonConfigurationRequired, noToken.Reason)
	require.False(t, noToken.Retained)
	require.False(t, delegationUnchecked.Retained)
}
