package certificate

import (
	"context"
	"crypto/tls"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// The state machine tests of the original certstate_internal_test.go. The
// original builds a certState literal and calls decide or apply on it. Here
// the same literal is placed in the state that runs the step with StartIn,
// and the step's outcome is sent to the chart at stateNow.

var stateNow = time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)

const testRefresh = time.Hour

func held(t *testing.T, notAfter time.Time) *tls.Certificate {
	t.Helper()
	return testCertificate(t, notAfter)
}

func testChart(t *testing.T) *sc.Machine[State, certState] {
	t.Helper()
	m, err := certificateChart()
	require.NoError(t, err)
	return m
}

// servingState holds cert with its next refresh, window check and renewal in
// the future. RefreshIn is what the poll that started the request drew.
func servingState(cert *tls.Certificate) certState {
	return certState{
		Cert: cert, Err: nil, Failures: 0, RetryAt: time.Time{},
		RefreshAt: stateNow.Add(time.Hour), WindowCheckAt: stateNow.Add(6 * time.Hour), RenewAt: stateNow.Add(30 * 24 * time.Hour),
		Steps: 1, RefreshIn: testRefresh,
	}
}

// unheld is a node holding nothing, inside a request.
func unheld() certState { return certState{Steps: 1, RefreshIn: testRefresh} }

// driver steps the chart by hand, playing the services.
type driver struct {
	t    *testing.T
	m    *sc.Machine[State, certState]
	snap sc.Snapshot[State, certState]
}

// in places a chart in state with context c at stateNow.
func in(t *testing.T, c certState, state State) *driver {
	t.Helper()
	m := testChart(t)
	snap, _, err := m.StartIn(c, stateNow, state)
	require.NoError(t, err)
	return &driver{t: t, m: m, snap: snap}
}

func (d *driver) send(ev any, now time.Time) *driver {
	d.snap, _ = d.m.Transition(d.snap, ev, now)
	return d
}

// service is the one service running.
func (d *driver) service() sc.RunningService[State] {
	d.t.Helper()
	running := d.snap.Services()
	require.Len(d.t, running, 1, "one step is in flight")
	return running[0]
}

// done ends the step in flight with cert at stateNow.
func (d *driver) done(cert *tls.Certificate) *driver {
	d.t.Helper()
	return d.send(sc.Done[*tls.Certificate]{ID: d.service().ID, Result: cert}, stateNow)
}

// window ends the window step in flight with its answer at stateNow.
func (d *driver) window(renewAt, checkAgain time.Time) *driver {
	d.t.Helper()
	return d.send(sc.Done[renewalWindow]{ID: d.service().ID, Result: renewalWindow{RenewAt: renewAt, CheckAgain: checkAgain}}, stateNow)
}

// failWith ends the step in flight with err at stateNow.
func (d *driver) failWith(err error) *driver {
	d.t.Helper()
	return d.send(sc.Failed{ID: d.service().ID, Err: err}, stateNow)
}

func (d *driver) in(s State) bool { return d.snap.In(s) }

func (d *driver) state() certState { return d.snap.Context }

// next is the step the chart takes for a poll at now, as decide names it.
func (d *driver) next(now time.Time) certAction {
	after, _ := d.m.Transition(d.snap, poll{RefreshIn: testRefresh}, now)
	switch {
	case after.In(Obtaining):
		return actionObtain
	case after.In(CheckingStore):
		return actionCheckStore
	case after.In(FetchingWindow):
		return actionFetchWindow
	case after.In(Renewing):
		return actionRenew
	default:
		return actionNone
	}
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
		in    State
		want  certAction
	}{
		{name: "nothing held obtains", state: certState{}, in: Idle, want: actionObtain},
		{name: "a failed obtain waits for its retry", state: certState{Err: errRateLimited, RetryAt: stateNow.Add(time.Minute)}, in: Idle, want: actionNone},
		{name: "a failed obtain retries when due", state: certState{Err: errRateLimited, RetryAt: stateNow}, in: Idle, want: actionObtain},
		{name: "serving with nothing due does nothing", state: servingState(cert), in: Idle, want: actionNone},
		{name: "refresh due reads the store first", state: with(func(st *certState) { st.RefreshAt, st.RenewAt = stateNow, stateNow }), in: Idle, want: actionCheckStore},
		{name: "a new certificate's window is read at once", state: with(func(st *certState) { st.WindowCheckAt = time.Time{} }), in: Idle, want: actionFetchWindow},
		{name: "the window is read before deciding to renew", state: with(func(st *certState) { st.WindowCheckAt, st.RenewAt = stateNow, stateNow }), in: Idle, want: actionFetchWindow},
		{name: "renewal due renews", state: with(func(st *certState) { st.RenewAt = stateNow }), in: Idle, want: actionRenew},
		{name: "a failed renewal waits for its retry", state: with(func(st *certState) {
			st.Err, st.RenewAt, st.RetryAt = errRateLimited, stateNow, stateNow.Add(time.Minute)
		}), in: Idle, want: actionNone},
		{name: "a failed renewal retries when due", state: with(func(st *certState) { st.Err, st.RenewAt, st.RetryAt = errRateLimited, stateNow, stateNow }), in: Idle, want: actionRenew},
		{name: "stopped does nothing", state: with(func(st *certState) { st.RenewAt = stateNow }), in: Stopped, want: actionNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.in != Stopped {
				require.Equal(t, tc.want, decide(tc.state, stateNow), "decide")
			}
			require.Equal(t, tc.want, in(t, tc.state, tc.in).next(stateNow), "the chart")
		})
	}
}

func TestApplyObtain(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(90*24*time.Hour))
	start := func() *driver { return in(t, unheld(), Obtaining) }

	d := start().done(cert)
	require.Equal(t, phaseServing, phaseOf(d.snap))
	require.Same(t, cert, d.state().Cert)
	require.Equal(t, stateNow.Add(testRefresh), d.state().RefreshAt)
	require.True(t, d.in(FetchingWindow), "the new certificate's window is read in the same request")

	reset := errors.New("acme: connection reset")
	d = start().failWith(reset)
	require.Equal(t, phaseRetrying, phaseOf(d.snap))
	require.Equal(t, 1, d.state().Failures)
	require.Equal(t, stateNow.Add(certificateRetryMin), d.state().RetryAt)

	d = start().failWith(errCertificateIssuing)
	require.Zero(t, d.state().Failures, "waiting for another slot does not back off")
	require.Equal(t, stateNow.Add(certificateIssuingRetry), d.state().RetryAt)
}

func TestApplyCheckStore(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(30*24*time.Hour))
	start := servingState(cert)
	start.RefreshAt = stateNow
	checking := func(c certState) *driver { return in(t, c, CheckingStore) }

	d := checking(start).done(nil)
	require.Same(t, cert, d.state().Cert, "an empty store changes nothing")
	require.Equal(t, stateNow.Add(testRefresh), d.state().RefreshAt)
	require.Equal(t, start.RenewAt, d.state().RenewAt)

	d = checking(start).done(cert)
	require.Same(t, cert, d.state().Cert, "the same certificate is not adopted again")
	require.Equal(t, start.WindowCheckAt, d.state().WindowCheckAt)

	older := held(t, stateNow.Add(time.Hour))
	require.Same(t, cert, checking(start).done(older).state().Cert, "an older stored certificate is ignored")

	renewed := held(t, stateNow.Add(90*24*time.Hour))
	failing := start
	failing.Err, failing.Failures, failing.RetryAt = errors.New("acme: connection reset"), 3, stateNow.Add(time.Hour)
	d = checking(failing).done(renewed)
	require.Equal(t, phaseServing, phaseOf(d.snap), "another slot's renewal clears this slot's failure")
	require.Same(t, renewed, d.state().Cert)
	require.Zero(t, d.state().Failures)
	require.True(t, d.state().RetryAt.IsZero())
	require.True(t, d.state().WindowCheckAt.IsZero(), "the adopted certificate's window is read next")

	d = checking(start).failWith(errors.New("decrypt failed"))
	require.Equal(t, phaseServing, phaseOf(d.snap), "an unreadable store is logged, not a failure")
	require.Equal(t, stateNow.Add(testRefresh), d.state().RefreshAt)
}

func TestApplyFetchWindow(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(90*24*time.Hour))
	start := func() *driver { return in(t, servingState(cert), FetchingWindow) }

	renewAt, checkAgain := stateNow.Add(60*24*time.Hour), stateNow.Add(6*time.Hour)
	d := start().window(renewAt, checkAgain)
	require.Equal(t, renewAt, d.state().RenewAt)
	require.Equal(t, checkAgain, d.state().WindowCheckAt)

	d = start().failWith(errors.New("directory unreachable"))
	require.Equal(t, fallbackRenewAt(cert.Leaf), d.state().RenewAt, "without a window, renew two thirds through")
	require.Equal(t, stateNow.Add(renewalCheckMin), d.state().WindowCheckAt, "and ask again within the hour")
	require.Equal(t, phaseServing, phaseOf(d.snap), "an unreachable window is not a failure")
}

func TestApplyRenew(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(10*24*time.Hour))
	start := servingState(cert)
	start.RenewAt = stateNow
	renewing := func() *driver { return in(t, start, Renewing) }

	renewed := held(t, stateNow.Add(90*24*time.Hour))
	d := renewing().done(renewed)
	require.Equal(t, phaseServing, phaseOf(d.snap))
	require.Same(t, renewed, d.state().Cert)
	require.True(t, d.in(FetchingWindow), "the renewal's window is read next, so it is not renewed again")

	reset := errors.New("acme: connection reset")
	d = renewing().failWith(reset)
	require.Equal(t, phaseRenewFailing, phaseOf(d.snap))
	require.Same(t, cert, d.state().Cert, "a failed renewal keeps serving the held certificate")
	require.Equal(t, 1, d.state().Failures)
	require.Equal(t, actionNone, d.next(stateNow), "and backs off")
	require.Equal(t, actionRenew, d.next(d.state().RetryAt), "then retries")

	d = renewing().done(cert)
	require.ErrorIs(t, d.state().Err, errRenewalUnchanged, "a renewal returning the same certificate is a failure, not a loop")
	require.Equal(t, phaseRenewFailing, phaseOf(d.snap))

	d = renewing()
	id := d.service().ID
	stopped := d.send(closeTracker{}, stateNow).snap
	require.Equal(t, stopped, d.send(sc.Done[*tls.Certificate]{ID: id, Result: renewed}, stateNow).snap, "a stopped node ignores late outcomes")
}

func TestCertStatus(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(time.Hour))
	failing := servingState(cert)
	failing.Err, failing.RetryAt = errors.New("acme: connection reset"), stateNow.Add(2*time.Hour)
	idle := func(c certState) sc.Snapshot[State, certState] { return in(t, c, Idle).snap }

	for _, tc := range []struct {
		name   string
		state  sc.Snapshot[State, certState]
		at     time.Time
		ready  bool
		reason string
	}{
		{name: "obtaining", state: idle(certState{}), at: stateNow, ready: false, reason: "obtaining a TLS certificate for private.example.ts.net"},
		{name: "retrying", state: idle(certState{Err: errRateLimited, RetryAt: stateNow.Add(time.Hour)}), at: stateNow, ready: false,
			reason: "Let's Encrypt is rate limiting new certificates for this tailnet; retrying after 2026-11-12 23:00 UTC"},
		{name: "serving", state: idle(servingState(cert)), at: stateNow, ready: true, reason: ""},
		{name: "renewal failing but still valid", state: idle(failing), at: stateNow, ready: true, reason: ""},
		{name: "expired", state: idle(servingState(cert)), at: stateNow.Add(time.Hour), ready: false, reason: "the TLS certificate for private.example.ts.net has expired"},
		{name: "expired after failed renewals", state: idle(failing), at: stateNow.Add(time.Hour), ready: false,
			reason: "the TLS certificate for private.example.ts.net has expired; acme: connection reset; retrying after 2026-11-13 00:00 UTC"},
		{name: "stopped", state: in(t, certState{}, Stopped).snap, at: stateNow, ready: false, reason: "the gateway node is stopping"},
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
	var mu sync.Mutex
	now := stateNow
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	tracker := newTestTracker(t, fetch.fetch, clock)
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
	mu.Lock()
	now = now.Add(time.Hour)
	mu.Unlock()
	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"obtain:rate_limited", "obtain:ok", "window:ok"}, seen,
		"each obtain and window request is counted; the window is read right after obtaining")
}
