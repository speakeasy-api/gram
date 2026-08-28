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

func TestReconcileHealthStateAnchorsUnhealthySinceWhenCheckFailedResolves(t *testing.T) {
	t.Parallel()

	firstCheck := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	secondCheck := firstCheck.Add(24 * time.Hour)

	// check_failed episode resolving into a real issue starts the confirmed
	// outage — UnhealthySince re-anchors to this check.
	state := ReconcileHealthState(HealthState{
		Status:              HealthStatusUnhealthy,
		Issue:               HealthIssueCheckFailed,
		CheckedAt:           &firstCheck,
		UnhealthySince:      &firstCheck,
		ConsecutiveFailures: 1,
	}, HealthObservation{
		Status: HealthStatusUnhealthy,
		Issue:  HealthIssueDNSNotFound,
	}, secondCheck)

	require.Equal(t, HealthIssueDNSNotFound, state.Issue)
	require.Equal(t, int32(2), state.ConsecutiveFailures)
	require.Equal(t, &secondCheck, state.UnhealthySince)
	require.True(t, IsRetryOfUnhealthyTransition(state, secondCheck),
		"a retried commit of this transition must be recognizable")

	// A continuing check_failed episode keeps its original start.
	state = ReconcileHealthState(HealthState{
		Status:              HealthStatusUnhealthy,
		Issue:               HealthIssueCheckFailed,
		CheckedAt:           &firstCheck,
		UnhealthySince:      &firstCheck,
		ConsecutiveFailures: 1,
	}, HealthObservation{
		Status: HealthStatusUnhealthy,
		Issue:  HealthIssueCheckFailed,
	}, secondCheck)

	require.Equal(t, &firstCheck, state.UnhealthySince)
	require.False(t, IsRetryOfUnhealthyTransition(state, secondCheck))
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

func TestShouldNotifyUnhealthyTransitionFreshFailure(t *testing.T) {
	t.Parallel()

	require.True(t, ShouldNotifyUnhealthyTransition(
		HealthState{Status: HealthStatusHealthy},
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueCertificateExpired},
	))
	require.True(t, ShouldNotifyUnhealthyTransition(
		HealthState{Status: HealthStatusUnknown},
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueDNSTargetMismatch},
	))
}

func TestShouldNotifyUnhealthyTransitionAlreadyUnhealthyStaysQuiet(t *testing.T) {
	t.Parallel()

	require.False(t, ShouldNotifyUnhealthyTransition(
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueCertificateExpired},
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueCertificateExpired},
	))
}

func TestShouldNotifyUnhealthyTransitionHealthyResultStaysQuiet(t *testing.T) {
	t.Parallel()

	require.False(t, ShouldNotifyUnhealthyTransition(
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueDNSNotFound},
		HealthState{Status: HealthStatusHealthy},
	))
}

func TestShouldNotifyUnhealthyTransitionCheckFailedExcluded(t *testing.T) {
	t.Parallel()

	require.False(t, ShouldNotifyUnhealthyTransition(
		HealthState{Status: HealthStatusHealthy},
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueCheckFailed},
	))
}

func TestShouldNotifyUnhealthyTransitionCheckFailedThenActionableIssueNotifies(t *testing.T) {
	t.Parallel()

	require.True(t, ShouldNotifyUnhealthyTransition(
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueCheckFailed},
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueDNSNotFound},
	))
	// A repeated probe failure still stays quiet.
	require.False(t, ShouldNotifyUnhealthyTransition(
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueCheckFailed},
		HealthState{Status: HealthStatusUnhealthy, Issue: HealthIssueCheckFailed},
	))
}

func TestIsRetryOfUnhealthyTransition(t *testing.T) {
	t.Parallel()

	checkedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	earlier := checkedAt.Add(-24 * time.Hour)

	// The check at checkedAt flipped the domain unhealthy: retry re-emits.
	require.True(t, IsRetryOfUnhealthyTransition(HealthState{
		Status:         HealthStatusUnhealthy,
		Issue:          HealthIssueDNSNotFound,
		CheckedAt:      &checkedAt,
		UnhealthySince: &checkedAt,
	}, checkedAt))

	// Long-running outage checked at checkedAt: transition predates this check.
	require.False(t, IsRetryOfUnhealthyTransition(HealthState{
		Status:         HealthStatusUnhealthy,
		Issue:          HealthIssueDNSNotFound,
		CheckedAt:      &checkedAt,
		UnhealthySince: &earlier,
	}, checkedAt))

	// A different (newer) check: not a retry.
	require.False(t, IsRetryOfUnhealthyTransition(HealthState{
		Status:         HealthStatusUnhealthy,
		Issue:          HealthIssueDNSNotFound,
		CheckedAt:      &earlier,
		UnhealthySince: &earlier,
	}, checkedAt))

	// Probe failures never notify, so retries of them re-emit nothing.
	require.False(t, IsRetryOfUnhealthyTransition(HealthState{
		Status:         HealthStatusUnhealthy,
		Issue:          HealthIssueCheckFailed,
		CheckedAt:      &checkedAt,
		UnhealthySince: &checkedAt,
	}, checkedAt))

	// Healthy domains have nothing to re-emit.
	require.False(t, IsRetryOfUnhealthyTransition(HealthState{
		Status:    HealthStatusHealthy,
		CheckedAt: &checkedAt,
	}, checkedAt))
}

func TestHealthIssueMessageCertificateProblemsAreManagedByGram(t *testing.T) {
	t.Parallel()

	for _, issue := range []HealthIssue{
		HealthIssueCertificateMissing,
		HealthIssueCertificateNotReady,
		HealthIssueCertificateExpired,
		HealthIssueCertificateInvalid,
	} {
		require.Equal(t,
			"There is a problem with the domain's TLS certificate. We're working to resolve it.",
			HealthIssueMessage(issue, DNSRemediation{Domain: "chat.example.com", ExpectedCNAME: "cname.example.com.", ExpectedARecords: nil}),
		)
	}
}

func TestHealthIssueMessageNamesExpectedCNAME(t *testing.T) {
	t.Parallel()

	cnameOnly := DNSRemediation{Domain: "chat.example.com", ExpectedCNAME: "cname.example.com.", ExpectedARecords: nil}
	require.Equal(t,
		"The domain's DNS no longer resolves to the expected target. Create a CNAME record pointing the domain at cname.example.com.",
		HealthIssueMessage(HealthIssueDNSTargetMismatch, cnameOnly),
	)
	require.Equal(t,
		"DNS records for the domain could not be found. Create a CNAME record pointing the domain at cname.example.com.",
		HealthIssueMessage(HealthIssueDNSNotFound, cnameOnly),
	)

	// An unconfigured target must not leak an empty parenthetical or name the
	// platform.
	empty := DNSRemediation{Domain: "chat.example.com", ExpectedCNAME: "", ExpectedARecords: nil}
	require.Equal(t,
		"The domain's DNS no longer resolves to the expected target.",
		HealthIssueMessage(HealthIssueDNSTargetMismatch, empty),
	)
	require.Equal(t,
		"DNS records for the domain could not be found.",
		HealthIssueMessage(HealthIssueDNSNotFound, empty),
	)
}

func TestHealthIssueMessageApexNamesARecords(t *testing.T) {
	t.Parallel()

	apex := DNSRemediation{Domain: "example.com", ExpectedCNAME: "cname.example.net.", ExpectedARecords: []string{"34.127.46.134"}}
	require.Equal(t,
		"DNS records for the domain could not be found. Create an A record pointing the domain at 34.127.46.134.",
		HealthIssueMessage(HealthIssueDNSNotFound, apex),
	)
	require.Equal(t,
		"The domain's DNS no longer resolves to the expected target. Create an A record pointing the domain at 34.127.46.134.",
		HealthIssueMessage(HealthIssueDNSTargetMismatch, apex),
	)
}

func TestHealthIssueMessageSubdomainOffersBothRecordTypes(t *testing.T) {
	t.Parallel()

	both := DNSRemediation{Domain: "chat.example.com", ExpectedCNAME: "cname.example.net.", ExpectedARecords: []string{"34.127.46.134"}}
	require.Equal(t,
		"DNS records for the domain could not be found. Create a CNAME record pointing the domain at cname.example.net (or, for an apex domain, an A record pointing at 34.127.46.134).",
		HealthIssueMessage(HealthIssueDNSNotFound, both),
	)
}

func TestShouldAutoDisableRequiresAllThresholds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	eightDaysAgo := now.Add(-8 * 24 * time.Hour)
	sixDaysAgo := now.Add(-6 * 24 * time.Hour)

	eligible := HealthState{
		Status:               HealthStatusUnhealthy,
		Issue:                HealthIssueDNSTargetMismatch,
		CheckedAt:            &now,
		UnhealthySince:       &eightDaysAgo,
		CertificateExpiresAt: nil,
		ConsecutiveFailures:  AutoDisableConsecutiveFailures,
	}
	require.True(t, ShouldAutoDisable(eligible, now))

	belowFailures := eligible
	belowFailures.ConsecutiveFailures = AutoDisableConsecutiveFailures - 1
	require.False(t, ShouldAutoDisable(belowFailures, now), "needs the full failure count")

	tooRecent := eligible
	tooRecent.UnhealthySince = &sixDaysAgo
	require.False(t, ShouldAutoDisable(tooRecent, now), "needs a full week unhealthy")

	gramSide := eligible
	gramSide.Issue = HealthIssueCheckFailed
	require.False(t, ShouldAutoDisable(gramSide, now), "check_failed never disables")

	healthy := eligible
	healthy.Status = HealthStatusHealthy
	require.False(t, ShouldAutoDisable(healthy, now))

	noAnchor := eligible
	noAnchor.UnhealthySince = nil
	require.False(t, ShouldAutoDisable(noAnchor, now))
}

func TestHealthIssueMessageApexPluralizesMultipleARecords(t *testing.T) {
	t.Parallel()

	apex := DNSRemediation{Domain: "example.com", ExpectedCNAME: "cname.example.net.", ExpectedARecords: []string{"34.127.46.134", "34.83.69.209"}}
	require.Equal(t,
		"DNS records for the domain could not be found. Create A records pointing the domain at 34.127.46.134, 34.83.69.209.",
		HealthIssueMessage(HealthIssueDNSNotFound, apex),
	)
}
