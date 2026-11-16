package tailnetgateway

import (
	"context"
	"crypto/tls"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The certificate state machine without goroutines, stores or clocks: decide
// picks the step, apply records its outcome, certStatus reports the result.

var stateNow = time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)

const testRefresh = time.Hour

func held(t *testing.T, notAfter time.Time) *tls.Certificate {
	t.Helper()
	return testCertificate(t, notAfter)
}

// servingState holds cert with its next refresh, window check and renewal in
// the future.
func servingState(cert *tls.Certificate) certState {
	return certState{
		phase: phaseServing, cert: cert, err: nil, failures: 0, retryAt: time.Time{},
		refreshAt: stateNow.Add(time.Hour), windowCheckAt: stateNow.Add(6 * time.Hour), renewAt: stateNow.Add(30 * 24 * time.Hour),
	}
}

func outcome(action certAction, cert *tls.Certificate, err error) certOutcome {
	return certOutcome{action: action, cert: cert, renewAt: time.Time{}, checkAgain: time.Time{}, err: err}
}

func TestDecide(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(90*24*time.Hour))
	with := func(f func(*certState)) certState {
		st := servingState(cert)
		f(&st)
		return st
	}

	for _, tc := range []struct {
		name  string
		state certState
		want  certAction
	}{
		{name: "nothing held obtains", state: certState{phase: phaseObtaining}, want: actionObtain},
		{name: "a failed obtain waits for its retry", state: certState{phase: phaseRetrying, retryAt: stateNow.Add(time.Minute)}, want: actionNone},
		{name: "a failed obtain retries when due", state: certState{phase: phaseRetrying, retryAt: stateNow}, want: actionObtain},
		{name: "serving with nothing due does nothing", state: servingState(cert), want: actionNone},
		{name: "refresh due reads the store first", state: with(func(st *certState) { st.refreshAt, st.renewAt = stateNow, stateNow }), want: actionCheckStore},
		{name: "a new certificate's window is read at once", state: with(func(st *certState) { st.windowCheckAt = time.Time{} }), want: actionFetchWindow},
		{name: "the window is read before deciding to renew", state: with(func(st *certState) { st.windowCheckAt, st.renewAt = stateNow, stateNow }), want: actionFetchWindow},
		{name: "renewal due renews", state: with(func(st *certState) { st.renewAt = stateNow }), want: actionRenew},
		{name: "a failed renewal waits for its retry", state: with(func(st *certState) {
			st.phase, st.renewAt, st.retryAt = phaseRenewFailing, stateNow, stateNow.Add(time.Minute)
		}), want: actionNone},
		{name: "a failed renewal retries when due", state: with(func(st *certState) { st.phase, st.renewAt, st.retryAt = phaseRenewFailing, stateNow, stateNow }), want: actionRenew},
		{name: "stopped does nothing", state: with(func(st *certState) { st.phase, st.renewAt = phaseStopped, stateNow }), want: actionNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, decide(tc.state, stateNow))
		})
	}
}

func TestApplyObtain(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(90*24*time.Hour))
	start := certState{phase: phaseObtaining}

	st := apply(start, outcome(actionObtain, cert, nil), stateNow, testRefresh)
	require.Equal(t, phaseServing, st.phase)
	require.Same(t, cert, st.cert)
	require.Equal(t, stateNow.Add(testRefresh), st.refreshAt)
	require.Equal(t, actionFetchWindow, decide(st, stateNow), "the new certificate's window is read in the same request")

	reset := errors.New("acme: connection reset")
	st = apply(start, outcome(actionObtain, nil, reset), stateNow, testRefresh)
	require.Equal(t, phaseRetrying, st.phase)
	require.Equal(t, 1, st.failures)
	require.Equal(t, stateNow.Add(certificateRetryMin), st.retryAt)

	st = apply(start, outcome(actionObtain, nil, errCertificateIssuing), stateNow, testRefresh)
	require.Zero(t, st.failures, "waiting for another slot does not back off")
	require.Equal(t, stateNow.Add(certificateIssuingRetry), st.retryAt)
}

func TestApplyCheckStore(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(30*24*time.Hour))
	start := servingState(cert)
	start.refreshAt = stateNow

	st := apply(start, outcome(actionCheckStore, nil, nil), stateNow, testRefresh)
	require.Same(t, cert, st.cert, "an empty store changes nothing")
	require.Equal(t, stateNow.Add(testRefresh), st.refreshAt)
	require.Equal(t, start.renewAt, st.renewAt)

	st = apply(start, outcome(actionCheckStore, cert, nil), stateNow, testRefresh)
	require.Same(t, cert, st.cert, "the same certificate is not adopted again")
	require.Equal(t, start.windowCheckAt, st.windowCheckAt)

	older := held(t, stateNow.Add(time.Hour))
	require.Same(t, cert, apply(start, outcome(actionCheckStore, older, nil), stateNow, testRefresh).cert, "an older stored certificate is ignored")

	renewed := held(t, stateNow.Add(90*24*time.Hour))
	failing := start
	failing.phase, failing.err, failing.failures, failing.retryAt = phaseRenewFailing, errors.New("acme: connection reset"), 3, stateNow.Add(time.Hour)
	st = apply(failing, outcome(actionCheckStore, renewed, nil), stateNow, testRefresh)
	require.Equal(t, phaseServing, st.phase, "another slot's renewal clears this slot's failure")
	require.Same(t, renewed, st.cert)
	require.Zero(t, st.failures)
	require.True(t, st.retryAt.IsZero())
	require.True(t, st.windowCheckAt.IsZero(), "the adopted certificate's window is read next")

	st = apply(start, outcome(actionCheckStore, nil, errors.New("decrypt failed")), stateNow, testRefresh)
	require.Equal(t, phaseServing, st.phase, "an unreadable store is logged, not a failure")
	require.Equal(t, stateNow.Add(testRefresh), st.refreshAt)
}

func TestApplyFetchWindow(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(90*24*time.Hour))
	start := servingState(cert)

	o := outcome(actionFetchWindow, nil, nil)
	o.renewAt, o.checkAgain = stateNow.Add(60*24*time.Hour), stateNow.Add(6*time.Hour)
	st := apply(start, o, stateNow, testRefresh)
	require.Equal(t, o.renewAt, st.renewAt)
	require.Equal(t, o.checkAgain, st.windowCheckAt)

	st = apply(start, outcome(actionFetchWindow, nil, errors.New("directory unreachable")), stateNow, testRefresh)
	require.Equal(t, fallbackRenewAt(cert.Leaf), st.renewAt, "without a window, renew two thirds through")
	require.Equal(t, stateNow.Add(renewalCheckMin), st.windowCheckAt, "and ask again within the hour")
	require.Equal(t, phaseServing, st.phase, "an unreachable window is not a failure")
}

func TestApplyRenew(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(10*24*time.Hour))
	start := servingState(cert)
	start.renewAt = stateNow

	renewed := held(t, stateNow.Add(90*24*time.Hour))
	st := apply(start, outcome(actionRenew, renewed, nil), stateNow, testRefresh)
	require.Equal(t, phaseServing, st.phase)
	require.Same(t, renewed, st.cert)
	require.Equal(t, actionFetchWindow, decide(st, stateNow), "the renewal's window is read next, so it is not renewed again")

	reset := errors.New("acme: connection reset")
	st = apply(start, outcome(actionRenew, nil, reset), stateNow, testRefresh)
	require.Equal(t, phaseRenewFailing, st.phase)
	require.Same(t, cert, st.cert, "a failed renewal keeps serving the held certificate")
	require.Equal(t, 1, st.failures)
	require.Equal(t, actionNone, decide(st, stateNow), "and backs off")
	require.Equal(t, actionRenew, decide(st, st.retryAt), "then retries")

	st = apply(start, outcome(actionRenew, cert, nil), stateNow, testRefresh)
	require.ErrorIs(t, st.err, errRenewalUnchanged, "a renewal returning the same certificate is a failure, not a loop")
	require.Equal(t, phaseRenewFailing, st.phase)

	stopped := start
	stopped.phase = phaseStopped
	require.Equal(t, stopped, apply(stopped, outcome(actionRenew, renewed, nil), stateNow, testRefresh), "a stopped node ignores late outcomes")
}

func TestCertStatus(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(time.Hour))
	failing := servingState(cert)
	failing.phase, failing.err, failing.retryAt = phaseRenewFailing, errors.New("acme: connection reset"), stateNow.Add(2*time.Hour)

	for _, tc := range []struct {
		name   string
		state  certState
		at     time.Time
		ready  bool
		reason string
	}{
		{name: "obtaining", state: certState{phase: phaseObtaining}, at: stateNow, ready: false, reason: "obtaining a TLS certificate for private.example.ts.net"},
		{name: "retrying", state: certState{phase: phaseRetrying, err: errRateLimited, retryAt: stateNow.Add(time.Hour)}, at: stateNow, ready: false,
			reason: "Let's Encrypt is rate limiting new certificates for this tailnet; retrying after 2026-11-12 23:00 UTC"},
		{name: "serving", state: servingState(cert), at: stateNow, ready: true, reason: ""},
		{name: "renewal failing but still valid", state: failing, at: stateNow, ready: true, reason: ""},
		{name: "expired", state: servingState(cert), at: stateNow.Add(time.Hour), ready: false, reason: "the TLS certificate for private.example.ts.net has expired"},
		{name: "expired after failed renewals", state: failing, at: stateNow.Add(time.Hour), ready: false,
			reason: "the TLS certificate for private.example.ts.net has expired; acme: connection reset; retrying after 2026-11-13 00:00 UTC"},
		{name: "stopped", state: certState{phase: phaseStopped}, at: stateNow, ready: false, reason: "the gateway node is stopping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ready, reason := certStatus(tc.state, "private.example.ts.net", tc.at)
			require.Equal(t, tc.ready, ready)
			require.Equal(t, tc.reason, reason)
		})
	}
}

func TestCertificateResult(t *testing.T) {
	t.Parallel()
	require.Equal(t, CertificateResultOK, certificateResult(nil))
	require.Equal(t, CertificateResultRateLimited, certificateResult(errRateLimited))
	require.Equal(t, CertificateResultWaitingForSlot, certificateResult(errCertificateIssuing))
	require.Equal(t, CertificateResultError, certificateResult(errRenewalUnchanged))
}

func TestCertificateTrackerReportsEachRequest(t *testing.T) {
	t.Parallel()
	fetch := newScriptedFetch(t, errRateLimited)
	now := stateNow
	tracker := newTestTracker(t, fetch.fetch, func() time.Time { return now })
	var mu sync.Mutex
	var seen []string
	tracker.events = func(_ context.Context, operation CertificateOperation, result CertificateResult) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, string(operation)+":"+string(result))
	}
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	now = now.Add(time.Hour)
	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"obtain:rate_limited", "obtain:ok", "window:ok"}, seen,
		"each obtain and window request is counted; the window is read right after obtaining")
}
