package customdomains

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReconcileHealthState(t *testing.T) {
	t.Parallel()

	firstCheck := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	secondCheck := firstCheck.Add(24 * time.Hour)
	expiresAt := firstCheck.Add(60 * 24 * time.Hour)

	t.Run("healthy observation clears failures", func(t *testing.T) {
		t.Parallel()

		state := ReconcileHealthState(HealthState{
			Status:              HealthStatusUnhealthy,
			Issue:               HealthIssueCertificateExpired,
			UnhealthySince:      &firstCheck,
			ConsecutiveFailures: 2,
		}, HealthObservation{
			Status:               HealthStatusHealthy,
			CertificateExpiresAt: &expiresAt,
		}, secondCheck)

		require.Equal(t, HealthStatusHealthy, state.Status)
		require.Empty(t, state.Issue)
		require.Equal(t, int32(0), state.ConsecutiveFailures)
		require.Nil(t, state.UnhealthySince)
		require.Equal(t, &secondCheck, state.CheckedAt)
		require.Equal(t, &expiresAt, state.CertificateExpiresAt)
	})

	t.Run("first failure starts unhealthy period", func(t *testing.T) {
		t.Parallel()

		state := ReconcileHealthState(HealthState{Status: HealthStatusUnknown}, HealthObservation{
			Status: HealthStatusUnhealthy,
			Issue:  HealthIssueDNSNotFound,
		}, firstCheck)

		require.Equal(t, HealthStatusUnhealthy, state.Status)
		require.Equal(t, HealthIssueDNSNotFound, state.Issue)
		require.Equal(t, int32(1), state.ConsecutiveFailures)
		require.Equal(t, &firstCheck, state.UnhealthySince)
	})

	t.Run("later failure preserves unhealthy period and increments count", func(t *testing.T) {
		t.Parallel()

		state := ReconcileHealthState(HealthState{
			Status:              HealthStatusUnhealthy,
			Issue:               HealthIssueDNSNotFound,
			CheckedAt:           &firstCheck,
			UnhealthySince:      &firstCheck,
			ConsecutiveFailures: 1,
		}, HealthObservation{
			Status: HealthStatusUnhealthy,
			Issue:  HealthIssueCertificateNotReady,
		}, secondCheck)

		require.Equal(t, HealthIssueCertificateNotReady, state.Issue)
		require.Equal(t, int32(2), state.ConsecutiveFailures)
		require.Equal(t, &firstCheck, state.UnhealthySince)
		require.Equal(t, &secondCheck, state.CheckedAt)
	})

	t.Run("retry with same check time is idempotent", func(t *testing.T) {
		t.Parallel()

		current := HealthState{
			Status:              HealthStatusUnhealthy,
			Issue:               HealthIssueDNSNotFound,
			CheckedAt:           &firstCheck,
			UnhealthySince:      &firstCheck,
			ConsecutiveFailures: 1,
		}

		state := ReconcileHealthState(current, HealthObservation{
			Status: HealthStatusUnhealthy,
			Issue:  HealthIssueDNSNotFound,
		}, firstCheck)

		require.Equal(t, current, state)
	})
}

func TestReconcileHealthStateIgnoresOlderObservation(t *testing.T) {
	t.Parallel()

	checkedAt := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	current := HealthState{
		Status:              HealthStatusHealthy,
		CheckedAt:           &checkedAt,
		ConsecutiveFailures: 0,
	}

	state := ReconcileHealthState(current, HealthObservation{
		Status: HealthStatusUnhealthy,
		Issue:  HealthIssueDNSNotFound,
	}, checkedAt.Add(-time.Minute))

	require.Equal(t, current, state)
}
