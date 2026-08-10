package platformmcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLifecycleTelemetryAcceptsOnlyBoundedDimensions(t *testing.T) {
	t.Parallel()

	for _, event := range []LifecycleEvent{
		{Operation: "registration", Phase: "complete", Outcome: "succeeded"},
		{Operation: "provider_setup", Phase: "handoff", Outcome: "invalid"},
		{Operation: "readiness", Phase: "forced_probe", Outcome: "succeeded", State: ReadinessReady},
	} {
		require.True(t, validLifecycleEvent(event))
	}
	for _, event := range []LifecycleEvent{
		{Operation: "catalog", Phase: "search", Outcome: "succeeded"},
		{Operation: "readiness", Phase: "forced_probe", Outcome: "", State: ReadinessReady},
		{Operation: "readiness", Phase: "https://untrusted.example.test", Outcome: "succeeded"},
		{Operation: "readiness", Phase: "forced_probe", Outcome: "succeeded", State: "https://untrusted.example.test"},
	} {
		require.False(t, validLifecycleEvent(event))
	}

	telemetry := noopLifecycleTelemetry{}
	telemetry.Record(context.Background(), LifecycleEvent{Operation: "registration", Phase: "complete", Outcome: "succeeded"})
}

func TestLifecycleOutcomeIsBounded(t *testing.T) {
	t.Parallel()

	require.Equal(t, "succeeded", lifecycleOutcome(nil))
	require.Equal(t, "rate_limited", lifecycleOutcome(ErrOperationRateLimited))
	require.Equal(t, "denied", lifecycleOutcome(ErrForbidden))
	require.Equal(t, "invalid", lifecycleOutcome(ErrReadinessInvalid))
	require.Equal(t, "unavailable", lifecycleOutcome(assertionError{}))
}

type assertionError struct{}

func (assertionError) Error() string { return "untrusted provider body and URL" }
