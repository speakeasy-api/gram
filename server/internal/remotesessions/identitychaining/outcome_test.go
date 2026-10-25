package identitychaining

import (
	"testing"

	"github.com/stretchr/testify/require"
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
