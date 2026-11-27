package customdomains_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
)

func TestReconcileHealthState(t *testing.T) {
	t.Parallel()

	firstCheck := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	secondCheck := firstCheck.Add(24 * time.Hour)
	expiresAt := firstCheck.Add(60 * 24 * time.Hour)

	t.Run("healthy observation clears failures", func(t *testing.T) {
		t.Parallel()

		state := customdomains.ReconcileHealthState(customdomains.HealthState{
			Status:              customdomains.HealthStatusUnhealthy,
			Issue:               customdomains.HealthIssueCertificateExpired,
			UnhealthySince:      &firstCheck,
			ConsecutiveFailures: 2,
		}, customdomains.HealthObservation{
			Status:               customdomains.HealthStatusHealthy,
			CertificateExpiresAt: &expiresAt,
		}, secondCheck)

		require.Equal(t, customdomains.HealthStatusHealthy, state.Status)
		require.Empty(t, state.Issue)
		require.Equal(t, int32(0), state.ConsecutiveFailures)
		require.Nil(t, state.UnhealthySince)
		require.Equal(t, &secondCheck, state.CheckedAt)
		require.Equal(t, &expiresAt, state.CertificateExpiresAt)
	})

	t.Run("first failure starts unhealthy period", func(t *testing.T) {
		t.Parallel()

		state := customdomains.ReconcileHealthState(customdomains.HealthState{Status: customdomains.HealthStatusUnknown}, customdomains.HealthObservation{
			Status: customdomains.HealthStatusUnhealthy,
			Issue:  customdomains.HealthIssueDNSNotFound,
		}, firstCheck)

		require.Equal(t, customdomains.HealthStatusUnhealthy, state.Status)
		require.Equal(t, customdomains.HealthIssueDNSNotFound, state.Issue)
		require.Equal(t, int32(1), state.ConsecutiveFailures)
		require.Equal(t, &firstCheck, state.UnhealthySince)
	})

	t.Run("later failure preserves unhealthy period and increments count", func(t *testing.T) {
		t.Parallel()

		state := customdomains.ReconcileHealthState(customdomains.HealthState{
			Status:              customdomains.HealthStatusUnhealthy,
			Issue:               customdomains.HealthIssueDNSNotFound,
			CheckedAt:           &firstCheck,
			UnhealthySince:      &firstCheck,
			ConsecutiveFailures: 1,
		}, customdomains.HealthObservation{
			Status: customdomains.HealthStatusUnhealthy,
			Issue:  customdomains.HealthIssueCertificateNotReady,
		}, secondCheck)

		require.Equal(t, customdomains.HealthIssueCertificateNotReady, state.Issue)
		require.Equal(t, int32(2), state.ConsecutiveFailures)
		require.Equal(t, &firstCheck, state.UnhealthySince)
		require.Equal(t, &secondCheck, state.CheckedAt)
	})

	t.Run("retry with same check time is idempotent", func(t *testing.T) {
		t.Parallel()

		current := customdomains.HealthState{
			Status:              customdomains.HealthStatusUnhealthy,
			Issue:               customdomains.HealthIssueDNSNotFound,
			CheckedAt:           &firstCheck,
			UnhealthySince:      &firstCheck,
			ConsecutiveFailures: 1,
		}

		state := customdomains.ReconcileHealthState(current, customdomains.HealthObservation{
			Status: customdomains.HealthStatusUnhealthy,
			Issue:  customdomains.HealthIssueDNSNotFound,
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
	state := customdomains.ReconcileHealthState(customdomains.HealthState{
		Status:              customdomains.HealthStatusUnhealthy,
		Issue:               customdomains.HealthIssueCheckFailed,
		CheckedAt:           &firstCheck,
		UnhealthySince:      &firstCheck,
		ConsecutiveFailures: 1,
	}, customdomains.HealthObservation{
		Status: customdomains.HealthStatusUnhealthy,
		Issue:  customdomains.HealthIssueDNSNotFound,
	}, secondCheck)

	require.Equal(t, customdomains.HealthIssueDNSNotFound, state.Issue)
	require.Equal(t, int32(2), state.ConsecutiveFailures)
	require.Equal(t, &secondCheck, state.UnhealthySince)
	require.True(t, customdomains.IsRetryOfUnhealthyTransition(state, secondCheck),
		"a retried commit of this transition must be recognizable")

	// A continuing check_failed episode keeps its original start.
	state = customdomains.ReconcileHealthState(customdomains.HealthState{
		Status:              customdomains.HealthStatusUnhealthy,
		Issue:               customdomains.HealthIssueCheckFailed,
		CheckedAt:           &firstCheck,
		UnhealthySince:      &firstCheck,
		ConsecutiveFailures: 1,
	}, customdomains.HealthObservation{
		Status: customdomains.HealthStatusUnhealthy,
		Issue:  customdomains.HealthIssueCheckFailed,
	}, secondCheck)

	require.Equal(t, &firstCheck, state.UnhealthySince)
	require.False(t, customdomains.IsRetryOfUnhealthyTransition(state, secondCheck))
}

func TestReconcileHealthStateIgnoresOlderObservation(t *testing.T) {
	t.Parallel()

	checkedAt := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	current := customdomains.HealthState{
		Status:              customdomains.HealthStatusHealthy,
		CheckedAt:           &checkedAt,
		ConsecutiveFailures: 0,
	}

	state := customdomains.ReconcileHealthState(current, customdomains.HealthObservation{
		Status: customdomains.HealthStatusUnhealthy,
		Issue:  customdomains.HealthIssueDNSNotFound,
	}, checkedAt.Add(-time.Minute))

	require.Equal(t, current, state)
}

func TestShouldNotifyUnhealthyTransitionFreshFailure(t *testing.T) {
	t.Parallel()

	require.True(t, customdomains.ShouldNotifyUnhealthyTransition(
		customdomains.HealthState{Status: customdomains.HealthStatusHealthy},
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueCertificateExpired},
	))
	require.True(t, customdomains.ShouldNotifyUnhealthyTransition(
		customdomains.HealthState{Status: customdomains.HealthStatusUnknown},
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueDNSTargetMismatch},
	))
}

func TestShouldNotifyUnhealthyTransitionAlreadyUnhealthyStaysQuiet(t *testing.T) {
	t.Parallel()

	require.False(t, customdomains.ShouldNotifyUnhealthyTransition(
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueCertificateExpired},
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueCertificateExpired},
	))
}

func TestShouldNotifyUnhealthyTransitionHealthyResultStaysQuiet(t *testing.T) {
	t.Parallel()

	require.False(t, customdomains.ShouldNotifyUnhealthyTransition(
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueDNSNotFound},
		customdomains.HealthState{Status: customdomains.HealthStatusHealthy},
	))
}

func TestShouldNotifyUnhealthyTransitionCheckFailedExcluded(t *testing.T) {
	t.Parallel()

	require.False(t, customdomains.ShouldNotifyUnhealthyTransition(
		customdomains.HealthState{Status: customdomains.HealthStatusHealthy},
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueCheckFailed},
	))
}

func TestShouldNotifyUnhealthyTransitionCheckFailedThenActionableIssueNotifies(t *testing.T) {
	t.Parallel()

	require.True(t, customdomains.ShouldNotifyUnhealthyTransition(
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueCheckFailed},
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueDNSNotFound},
	))
	// A repeated probe failure still stays quiet.
	require.False(t, customdomains.ShouldNotifyUnhealthyTransition(
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueCheckFailed},
		customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: customdomains.HealthIssueCheckFailed},
	))
}

func TestShouldNotifyUnhealthyTransitionHoldsCertificateIssueUntilDNSMatches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		issue  customdomains.HealthIssue
		dns    customdomains.DNSConfigurationStatus
		notify bool
	}{
		{issue: customdomains.HealthIssueCertificateNotReady, dns: customdomains.DNSConfigurationUnconfirmed, notify: false},
		{issue: customdomains.HealthIssueCertificateNotReady, dns: customdomains.DNSConfigurationNotFound, notify: false},
		{issue: customdomains.HealthIssueCertificateNotReady, dns: customdomains.DNSConfigurationWrongTarget, notify: false},
		{issue: customdomains.HealthIssueCertificateMissing, dns: customdomains.DNSConfigurationUnconfirmed, notify: false},
		{issue: customdomains.HealthIssueCertificateNotReady, dns: customdomains.DNSConfigurationMatched, notify: true},
		// Without usable DNS evidence the alert keeps its previous behavior.
		{issue: customdomains.HealthIssueCertificateNotReady, dns: customdomains.DNSConfigurationCheckFailed, notify: true},
		{issue: customdomains.HealthIssueCertificateNotReady, dns: customdomains.DNSConfigurationNotConfigured, notify: true},
		{issue: customdomains.HealthIssueCertificateNotReady, dns: "", notify: true},
		// Only issues a pending DNS cutover explains are held.
		{issue: customdomains.HealthIssueCertificateExpired, dns: customdomains.DNSConfigurationUnconfirmed, notify: true},
		{issue: customdomains.HealthIssueResourceMissing, dns: customdomains.DNSConfigurationNotFound, notify: true},
	} {
		t.Run(string(tc.issue)+"/"+string(tc.dns), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.notify, customdomains.ShouldNotifyUnhealthyTransition(
				customdomains.HealthState{Status: customdomains.HealthStatusHealthy},
				customdomains.HealthState{Status: customdomains.HealthStatusUnhealthy, Issue: tc.issue, DNSConfiguration: tc.dns},
			))
		})
	}
}

func TestHeldCertificateIssueNotifiesOnceDNSMatches(t *testing.T) {
	t.Parallel()

	firstCheck := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	secondCheck := firstCheck.Add(24 * time.Hour)
	thirdCheck := secondCheck.Add(24 * time.Hour)

	held := customdomains.ReconcileHealthState(customdomains.HealthState{Status: customdomains.HealthStatusHealthy}, customdomains.HealthObservation{
		Status:           customdomains.HealthStatusUnhealthy,
		Issue:            customdomains.HealthIssueCertificateNotReady,
		DNSConfiguration: customdomains.DNSConfigurationUnconfirmed,
	}, firstCheck)
	require.False(t, customdomains.ShouldNotifyUnhealthyTransition(customdomains.HealthState{Status: customdomains.HealthStatusHealthy}, held))
	require.False(t, customdomains.IsRetryOfUnhealthyTransition(held, firstCheck), "a retried commit of a held issue must not alert")

	rearmed := customdomains.ReconcileHealthState(held, customdomains.HealthObservation{
		Status:           customdomains.HealthStatusUnhealthy,
		Issue:            customdomains.HealthIssueCertificateNotReady,
		DNSConfiguration: customdomains.DNSConfigurationMatched,
	}, secondCheck)
	require.True(t, customdomains.ShouldNotifyUnhealthyTransition(held, rearmed))
	require.Equal(t, &secondCheck, rearmed.UnhealthySince, "the alertable outage starts when DNS matches")
	require.True(t, customdomains.IsRetryOfUnhealthyTransition(rearmed, secondCheck), "a retried commit of the re-armed alert must re-emit it")

	continuing := customdomains.ReconcileHealthState(rearmed, customdomains.HealthObservation{
		Status:           customdomains.HealthStatusUnhealthy,
		Issue:            customdomains.HealthIssueCertificateNotReady,
		DNSConfiguration: customdomains.DNSConfigurationMatched,
	}, thirdCheck)
	require.False(t, customdomains.ShouldNotifyUnhealthyTransition(rearmed, continuing))
	require.Equal(t, &secondCheck, continuing.UnhealthySince)
}

func TestIsRetryOfUnhealthyTransition(t *testing.T) {
	t.Parallel()

	checkedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	earlier := checkedAt.Add(-24 * time.Hour)

	// The check at checkedAt flipped the domain unhealthy: retry re-emits.
	require.True(t, customdomains.IsRetryOfUnhealthyTransition(customdomains.HealthState{
		Status:         customdomains.HealthStatusUnhealthy,
		Issue:          customdomains.HealthIssueDNSNotFound,
		CheckedAt:      &checkedAt,
		UnhealthySince: &checkedAt,
	}, checkedAt))

	// Long-running outage checked at checkedAt: transition predates this check.
	require.False(t, customdomains.IsRetryOfUnhealthyTransition(customdomains.HealthState{
		Status:         customdomains.HealthStatusUnhealthy,
		Issue:          customdomains.HealthIssueDNSNotFound,
		CheckedAt:      &checkedAt,
		UnhealthySince: &earlier,
	}, checkedAt))

	// A different (newer) check: not a retry.
	require.False(t, customdomains.IsRetryOfUnhealthyTransition(customdomains.HealthState{
		Status:         customdomains.HealthStatusUnhealthy,
		Issue:          customdomains.HealthIssueDNSNotFound,
		CheckedAt:      &earlier,
		UnhealthySince: &earlier,
	}, checkedAt))

	// Probe failures never notify, so retries of them re-emit nothing.
	require.False(t, customdomains.IsRetryOfUnhealthyTransition(customdomains.HealthState{
		Status:         customdomains.HealthStatusUnhealthy,
		Issue:          customdomains.HealthIssueCheckFailed,
		CheckedAt:      &checkedAt,
		UnhealthySince: &checkedAt,
	}, checkedAt))

	// Healthy domains have nothing to re-emit.
	require.False(t, customdomains.IsRetryOfUnhealthyTransition(customdomains.HealthState{
		Status:    customdomains.HealthStatusHealthy,
		CheckedAt: &checkedAt,
	}, checkedAt))
}

func TestHealthIssueMessageCertificateProblemsAreManagedByGram(t *testing.T) {
	t.Parallel()

	for _, issue := range []customdomains.HealthIssue{
		customdomains.HealthIssueCertificateMissing,
		customdomains.HealthIssueCertificateNotReady,
		customdomains.HealthIssueCertificateExpired,
		customdomains.HealthIssueCertificateInvalid,
	} {
		require.Equal(t,
			"There is a problem with the domain's TLS certificate. We're working to resolve it.",
			customdomains.HealthIssueMessage(issue, customdomains.DNSRemediation{Domain: "chat.example.com", ExpectedCNAME: "cname.example.com.", ExpectedARecords: nil}),
		)
	}
}

func TestHealthIssueMessageNamesExpectedCNAME(t *testing.T) {
	t.Parallel()

	cnameOnly := customdomains.DNSRemediation{Domain: "chat.example.com", ExpectedCNAME: "cname.example.com.", ExpectedARecords: nil}
	require.Equal(t,
		"The domain's DNS no longer resolves to the expected target. Create a CNAME record pointing the domain at cname.example.com.",
		customdomains.HealthIssueMessage(customdomains.HealthIssueDNSTargetMismatch, cnameOnly),
	)
	require.Equal(t,
		"DNS records for the domain could not be found. Create a CNAME record pointing the domain at cname.example.com.",
		customdomains.HealthIssueMessage(customdomains.HealthIssueDNSNotFound, cnameOnly),
	)

	// An unconfigured target must not leak an empty parenthetical or name the
	// platform.
	empty := customdomains.DNSRemediation{Domain: "chat.example.com", ExpectedCNAME: "", ExpectedARecords: nil}
	require.Equal(t,
		"The domain's DNS no longer resolves to the expected target.",
		customdomains.HealthIssueMessage(customdomains.HealthIssueDNSTargetMismatch, empty),
	)
	require.Equal(t,
		"DNS records for the domain could not be found.",
		customdomains.HealthIssueMessage(customdomains.HealthIssueDNSNotFound, empty),
	)
}

func TestHealthIssueMessageApexNamesARecords(t *testing.T) {
	t.Parallel()

	apex := customdomains.DNSRemediation{Domain: "example.com", ExpectedCNAME: "cname.example.net.", ExpectedARecords: []string{"34.127.46.134"}}
	require.Equal(t,
		"DNS records for the domain could not be found. Create an A record pointing the domain at 34.127.46.134.",
		customdomains.HealthIssueMessage(customdomains.HealthIssueDNSNotFound, apex),
	)
	require.Equal(t,
		"The domain's DNS no longer resolves to the expected target. Create an A record pointing the domain at 34.127.46.134.",
		customdomains.HealthIssueMessage(customdomains.HealthIssueDNSTargetMismatch, apex),
	)
}

func TestHealthIssueMessageSubdomainOffersBothRecordTypes(t *testing.T) {
	t.Parallel()

	both := customdomains.DNSRemediation{Domain: "chat.example.com", ExpectedCNAME: "cname.example.net.", ExpectedARecords: []string{"34.127.46.134"}}
	require.Equal(t,
		"DNS records for the domain could not be found. Create a CNAME record pointing the domain at cname.example.net (or, for an apex domain, an A record pointing at 34.127.46.134).",
		customdomains.HealthIssueMessage(customdomains.HealthIssueDNSNotFound, both),
	)
}

func TestHealthIssueMessageApexPluralizesMultipleARecords(t *testing.T) {
	t.Parallel()

	apex := customdomains.DNSRemediation{Domain: "example.com", ExpectedCNAME: "cname.example.net.", ExpectedARecords: []string{"34.127.46.134", "34.83.69.209"}}
	require.Equal(t,
		"DNS records for the domain could not be found. Create A records pointing the domain at 34.127.46.134, 34.83.69.209.",
		customdomains.HealthIssueMessage(customdomains.HealthIssueDNSNotFound, apex),
	)
}
