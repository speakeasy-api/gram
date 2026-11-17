package tailnetgateway

import (
	"context"
	"crypto/tls"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/speakeasy-api/sc"
	"github.com/stretchr/testify/require"
)

// The certificate chart without goroutines, stores or clocks: each test puts
// the chart in a state, delivers what a request or the clock would, and
// checks where it went.

var stateNow = time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)

const testRefresh = time.Hour

type (
	certSnapshot = sc.Snapshot[certState, certContext]
	certStep     = sc.Step[certState, certContext]
)

func held(t *testing.T, notAfter time.Time) *tls.Certificate {
	t.Helper()
	return testCertificate(t, notAfter)
}

// servingContext holds cert with its next refresh, window check and renewal
// in the future.
func servingContext(cert *tls.Certificate) certContext {
	return certContext{
		cert: cert, err: nil, failures: 0, retryAt: time.Time{},
		refreshAt: stateNow.Add(time.Hour), windowCheckAt: stateNow.Add(6 * time.Hour), renewAt: stateNow.Add(30 * 24 * time.Hour),
		refreshEvery: testRefresh,
	}
}

// startIn puts the chart in state with c at stateNow, and returns the run ID
// of the request the state starts, if any.
func startIn(t *testing.T, c certContext, state certState) (certSnapshot, string) {
	t.Helper()
	snap, effects, err := certificateChart.StartIn(c, stateNow, state)
	require.NoError(t, err)
	return snap, requestRun(effects)
}

// requestRun is the run ID of the request effects start, or "".
func requestRun(effects []sc.Effect[certContext]) string {
	for _, e := range effects {
		if e.Kind == sc.EffectStartInvocation {
			return e.ID
		}
	}
	return ""
}

func done[R any](id string, result R) sc.Done[R] {
	return sc.Done[R]{ID: id, Result: result}
}

func TestChartPicksTheNextRequest(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(90*24*time.Hour))
	with := func(f func(*certContext)) certContext {
		c := servingContext(cert)
		f(&c)
		return c
	}

	for _, tc := range []struct {
		name string
		in   certState
		c    certContext
		at   time.Time
		want certState
	}{
		{name: "the first poll obtains", in: certWaiting, c: certContext{refreshEvery: testRefresh}, at: stateNow, want: certObtaining},
		{name: "a failed obtain waits for its retry", in: certRetrying, c: certContext{retryAt: stateNow.Add(time.Minute)}, at: stateNow, want: certRetrying},
		{name: "a failed obtain retries when due", in: certRetrying, c: certContext{retryAt: stateNow.Add(time.Minute)}, at: stateNow.Add(time.Minute), want: certObtaining},
		{name: "serving with nothing due does nothing", in: certServing, c: servingContext(cert), at: stateNow, want: certServing},
		{name: "refresh due reads the store first", in: certServing, c: with(func(c *certContext) { c.refreshAt, c.renewAt = stateNow, stateNow }), at: stateNow, want: certCheckingStore},
		{name: "a new certificate's window is read at once", in: certServing, c: with(func(c *certContext) { c.windowCheckAt = time.Time{} }), at: stateNow, want: certFetchingWindow},
		{name: "the window is read before deciding to renew", in: certServing, c: with(func(c *certContext) { c.windowCheckAt, c.renewAt = stateNow, stateNow }), at: stateNow, want: certFetchingWindow},
		{name: "an unread window never renews", in: certServing, c: with(func(c *certContext) { c.windowCheckAt, c.renewAt = stateNow.Add(time.Hour), time.Time{} }), at: stateNow, want: certServing},
		{name: "renewal due renews", in: certServing, c: with(func(c *certContext) { c.renewAt = stateNow }), at: stateNow, want: certRenewing},
		{name: "a failed renewal waits for its retry", in: certRenewBackoff, c: with(func(c *certContext) { c.renewAt, c.retryAt = stateNow, stateNow.Add(time.Minute) }), at: stateNow, want: certRenewBackoff},
		{name: "a failed renewal retries when due", in: certRenewBackoff, c: with(func(c *certContext) { c.renewAt, c.retryAt = stateNow, stateNow.Add(time.Minute) }), at: stateNow.Add(time.Minute), want: certRenewing},
		{name: "stopped does nothing", in: certStopped, c: with(func(c *certContext) { c.renewAt = stateNow }), at: stateNow, want: certStopped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snap, _ := startIn(t, tc.c, tc.in)
			snap, _ = certificateChart.Transition(snap, certPoll{}, tc.at)
			require.True(t, snap.In(tc.want), "in %v", snap.Active())
		})
	}
}

func TestChartObtains(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(90*24*time.Hour))
	start := certContext{refreshEvery: testRefresh}

	snap, run := startIn(t, start, certObtaining)
	snap, _ = certificateChart.Transition(snap, done(run, cert), stateNow)
	require.True(t, snap.In(certFetchingWindow), "the new certificate's window is read in the same step")
	require.Same(t, cert, snap.Context.cert)
	require.Equal(t, stateNow.Add(testRefresh), snap.Context.refreshAt)

	reset := errors.New("acme: connection reset")
	snap, run = startIn(t, start, certObtaining)
	snap, _ = certificateChart.Transition(snap, sc.Failed{ID: run, Err: reset}, stateNow)
	require.True(t, snap.In(certRetrying))
	require.Equal(t, 1, snap.Context.failures)
	require.Equal(t, stateNow.Add(certificateRetryMin), snap.Context.retryAt)

	snap, run = startIn(t, start, certObtaining)
	snap, _ = certificateChart.Transition(snap, sc.Failed{ID: run, Err: errCertificateIssuing}, stateNow)
	require.Zero(t, snap.Context.failures, "waiting for another slot does not back off")
	require.Equal(t, stateNow.Add(certificateIssuingRetry), snap.Context.retryAt)
}

func TestChartChecksTheStore(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(30*24*time.Hour))
	start := servingContext(cert)

	check := func(c certContext, ev func(run string) any) certSnapshot {
		snap, run := startIn(t, c, certCheckingStore)
		snap, _ = certificateChart.Transition(snap, ev(run), stateNow)
		return snap
	}

	snap := check(start, func(run string) any { return done[*tls.Certificate](run, nil) })
	require.True(t, snap.In(certServing))
	require.Same(t, cert, snap.Context.cert, "an empty store changes nothing")
	require.Equal(t, stateNow.Add(testRefresh), snap.Context.refreshAt)
	require.Equal(t, start.renewAt, snap.Context.renewAt)

	snap = check(start, func(run string) any { return done(run, cert) })
	require.Same(t, cert, snap.Context.cert, "the same certificate is not adopted again")
	require.Equal(t, start.windowCheckAt, snap.Context.windowCheckAt)

	older := held(t, stateNow.Add(time.Hour))
	snap = check(start, func(run string) any { return done(run, older) })
	require.Same(t, cert, snap.Context.cert, "an older stored certificate is ignored")

	renewed := held(t, stateNow.Add(90*24*time.Hour))
	failing := start
	failing.err, failing.failures, failing.retryAt = errors.New("acme: connection reset"), 3, stateNow.Add(time.Hour)
	snap = check(failing, func(run string) any { return done(run, renewed) })
	require.NoError(t, snap.Context.err, "another slot's renewal clears this slot's failure")
	require.Same(t, renewed, snap.Context.cert)
	require.Zero(t, snap.Context.failures)
	require.True(t, snap.Context.retryAt.IsZero())
	require.True(t, snap.In(certFetchingWindow), "the adopted certificate's window is read next")

	snap = check(start, func(run string) any { return sc.Failed{ID: run, Err: errors.New("decrypt failed")} })
	require.True(t, snap.In(certServing))
	require.NoError(t, snap.Context.err, "an unreadable store is logged, not a failure")
	require.Equal(t, stateNow.Add(testRefresh), snap.Context.refreshAt)
}

func TestChartFetchesTheWindow(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(90*24*time.Hour))
	start := servingContext(cert)

	window := renewalWindow{renewAt: stateNow.Add(60 * 24 * time.Hour), checkAgain: stateNow.Add(6 * time.Hour)}
	snap, run := startIn(t, start, certFetchingWindow)
	snap, _ = certificateChart.Transition(snap, done(run, window), stateNow)
	require.True(t, snap.In(certServing))
	require.Equal(t, window.renewAt, snap.Context.renewAt)
	require.Equal(t, window.checkAgain, snap.Context.windowCheckAt)

	snap, run = startIn(t, start, certFetchingWindow)
	snap, _ = certificateChart.Transition(snap, sc.Failed{ID: run, Err: errors.New("directory unreachable")}, stateNow)
	require.Equal(t, fallbackRenewAt(cert.Leaf), snap.Context.renewAt, "without a window, renew two thirds through")
	require.Equal(t, stateNow.Add(renewalCheckMin), snap.Context.windowCheckAt, "and ask again within the hour")
	require.True(t, snap.In(certServing), "an unreachable window is not a failure")
	require.NoError(t, snap.Context.err)
}

func TestChartRenews(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(10*24*time.Hour))
	start := servingContext(cert)
	start.renewAt = stateNow

	renewed := held(t, stateNow.Add(90*24*time.Hour))
	snap, run := startIn(t, start, certRenewing)
	snap, _ = certificateChart.Transition(snap, done(run, renewed), stateNow)
	require.Same(t, renewed, snap.Context.cert)
	require.True(t, snap.In(certFetchingWindow), "the renewal's window is read next, so it is not renewed again")

	reset := errors.New("acme: connection reset")
	snap, run = startIn(t, start, certRenewing)
	snap, _ = certificateChart.Transition(snap, sc.Failed{ID: run, Err: reset}, stateNow)
	require.True(t, snap.In(certRenewBackoff))
	require.Same(t, cert, snap.Context.cert, "a failed renewal keeps serving the held certificate")
	require.Equal(t, 1, snap.Context.failures)
	waiting, _ := certificateChart.Transition(snap, certPoll{}, stateNow)
	require.True(t, waiting.In(certRenewBackoff), "and backs off")
	retried, _ := certificateChart.Transition(snap, certPoll{}, snap.Context.retryAt)
	require.True(t, retried.In(certRenewing), "then retries")

	snap, run = startIn(t, start, certRenewing)
	snap, _ = certificateChart.Transition(snap, done(run, cert), stateNow)
	require.ErrorIs(t, snap.Context.err, errRenewalUnchanged, "a renewal returning the same certificate is a failure, not a loop")
	require.True(t, snap.In(certRenewBackoff))

	// Found by TestChartKeepsItsShareOfTheRules: a node that had adopted
	// another slot's renewal from the store, then renewed and got back an
	// older certificate, served the older one.
	older := held(t, stateNow.Add(5*24*time.Hour))
	snap, run = startIn(t, start, certRenewing)
	snap, _ = certificateChart.Transition(snap, done(run, older), stateNow)
	require.Same(t, cert, snap.Context.cert, "a renewal is adopted only if it is later than the held certificate")
	require.ErrorIs(t, snap.Context.err, errRenewalUnchanged)

	snap, run = startIn(t, start, certRenewing)
	stopped, effects := certificateChart.Transition(snap, certStop{}, stateNow)
	require.True(t, stopped.In(certStopped))
	require.Contains(t, kinds(effects), sc.EffectStopInvocation, "closing stops the request in flight")
	late, _ := certificateChart.Transition(stopped, done(run, renewed), stateNow)
	require.Equal(t, stopped.Seq(), late.Seq(), "a stopped node ignores late outcomes")
}

func kinds(effects []sc.Effect[certContext]) []sc.EffectKind {
	out := make([]sc.EffectKind, 0, len(effects))
	for _, e := range effects {
		out = append(out, e.Kind)
	}
	return out
}

func TestCertStatus(t *testing.T) {
	t.Parallel()
	cert := held(t, stateNow.Add(time.Hour))
	failing := servingContext(cert)
	failing.err, failing.retryAt = errors.New("acme: connection reset"), stateNow.Add(2*time.Hour)

	for _, tc := range []struct {
		name   string
		in     certState
		c      certContext
		at     time.Time
		ready  bool
		failed bool
		reason string
	}{
		{name: "obtaining", in: certWaiting, c: certContext{}, at: stateNow, ready: false, failed: false, reason: "obtaining a TLS certificate for private.example.ts.net"},
		{name: "retrying", in: certRetrying, c: certContext{err: errRateLimited, retryAt: stateNow.Add(time.Hour)}, at: stateNow, ready: false, failed: true,
			reason: "Let's Encrypt is rate limiting new certificates for this tailnet; retrying after 2026-11-12 23:00 UTC"},
		{name: "serving", in: certServing, c: servingContext(cert), at: stateNow, ready: true, failed: false, reason: ""},
		{name: "renewal failing but still valid", in: certRenewBackoff, c: failing, at: stateNow, ready: true, failed: false, reason: ""},
		{name: "expired", in: certServing, c: servingContext(cert), at: stateNow.Add(time.Hour), ready: false, failed: true, reason: "the TLS certificate for private.example.ts.net has expired"},
		{name: "expired after failed renewals", in: certRenewBackoff, c: failing, at: stateNow.Add(time.Hour), ready: false, failed: true,
			reason: "the TLS certificate for private.example.ts.net has expired; acme: connection reset; retrying after 2026-11-13 00:00 UTC"},
		{name: "waiting for another slot", in: certRetrying, c: certContext{err: errCertificateIssuing, retryAt: stateNow.Add(5 * time.Second)}, at: stateNow, ready: false, failed: false,
			reason: "another gateway slot is obtaining the TLS certificate for private.example.ts.net"},
		{name: "service name not listed yet", in: certRetrying, c: certContext{err: errors.New(`invalid domain "private.example.ts.net"`), retryAt: stateNow.Add(5 * time.Second)}, at: stateNow, ready: false, failed: false,
			reason: "could not obtain a TLS certificate for private.example.ts.net: invalid domain \"private.example.ts.net\"; retrying after 2026-11-12 22:00 UTC"},
		{name: "stopped", in: certStopped, c: servingContext(cert), at: stateNow, ready: false, failed: false, reason: "the gateway node is stopping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snap, _ := startIn(t, tc.c, tc.in)
			ready, failed, reason := certStatus(snap, "private.example.ts.net", tc.at)
			require.Equal(t, tc.ready, ready)
			require.Equal(t, tc.failed, failed)
			require.Equal(t, tc.reason, reason)
		})
	}
}

// TestChartKeepsItsShareOfTheRules explores every state the chart can reach
// and checks its share of the rules in Certificate.tla. The chart cannot see
// the store, the lease or Let's Encrypt, so the rules about them become rules
// about what the chart asks for: a node holding a certificate never orders a
// new one, it renews only once the window says so, and it never stops serving
// a certificate it holds.
func TestChartKeepsItsShareOfTheRules(t *testing.T) {
	t.Parallel()
	// Short lifetimes keep the explored times few: the clock moves an
	// hour at a time between store reads.
	first := shortLived(t, stateNow, stateNow.Add(6*time.Hour))
	renewed := shortLived(t, stateNow.Add(3*time.Hour), stateNow.Add(12*time.Hour))
	serial := func(cert *tls.Certificate) string {
		switch cert {
		case nil:
			return ""
		case first:
			return "first"
		case renewed:
			return "renewed"
		}
		return "other"
	}
	errClass := func(err error) string {
		switch {
		case err == nil:
			return ""
		case errors.Is(err, errRenewalUnchanged):
			return "unchanged"
		case errors.Is(err, errCertificateIssuing):
			return "issuing"
		case isCertificateRateLimited(err):
			return "rate_limited"
		}
		return "error"
	}
	// relative keeps how far a stored time is from now, so the explored
	// states stay finite as the exploration clock moves on. Every time
	// already past behaves the same: its deadline fires at once.
	relative := func(at, now time.Time) time.Duration {
		if at.IsZero() {
			return -1
		}
		return max(at.Sub(now), 0)
	}
	window := func(id string, now time.Time) any {
		// Let's Encrypt's window is a fixed time, asked about again
		// after the longest wait.
		return done(id, renewalWindow{renewAt: stateNow.Add(4 * time.Hour), checkAgain: now.Add(renewalCheckMax)})
	}
	reset := errors.New("acme: connection reset")
	refused := errors.New("acme: urn:ietf:params:acme:error:rateLimited: too many certificates")

	requests := func(st certStep, name string) []sc.Effect[certContext] {
		var out []sc.Effect[certContext]
		for _, e := range st.Effects {
			if e.Kind == sc.EffectStartInvocation && e.Name == name {
				out = append(out, e)
			}
		}
		return out
	}

	// The store is read every two hours rather than every hour: each retry
	// interleaves with the refresh, and the offsets between them are what
	// the explored states multiply by.
	report, err := certificateChart.Explore(certContext{refreshEvery: 2 * time.Hour}, sc.Exploration[certState, certContext]{
		Start:  stateNow,
		Events: []any{certPoll{}, certStop{}},
		// The chart has no timers: time passes between polls. Once a
		// certificate is held, every stored time is on a half-hour grid
		// (the refresh and window intervals, the renewal window, the
		// certificates' lifetimes, the half-hour refusals), so waits on
		// the same grid keep the explored states few. Retries before
		// then are 5 to 30 seconds, and half an hour is past each.
		Waits: []time.Duration{30 * time.Minute, 2 * time.Hour},
		Outcomes: map[string][]sc.Outcome{
			serviceObtain: {sc.Succeeds(first), sc.Fails(errCertificateIssuing), sc.Fails(refused), sc.Fails(reset)},
			// Once a certificate is held, failures are refusals that
			// wait half an hour: the store, window and renewal timers
			// are then all on a half-hour grid, which keeps the
			// explored states few. The unit tests above cover the
			// other retry times.
			serviceCheckStore:  {sc.Succeeds[*tls.Certificate](nil), sc.Succeeds(renewed), sc.Fails(refused)},
			serviceFetchWindow: {window, sc.Fails(refused)},
			serviceRenew:       {sc.Succeeds(renewed), sc.Succeeds(first), sc.Fails(refused)},
		},
		Key: func(c certContext, now time.Time) any {
			return []any{
				serial(c.cert), errClass(c.err),
				// The backoff is capped from the seventh failure on.
				min(c.failures, 7),
				relative(c.retryAt, now), relative(c.refreshAt, now), relative(c.windowCheckAt, now), relative(c.renewAt, now),
				c.refreshEvery,
			}
		},
		CheckKey:         true,
		CheckDeterminism: true,
		Invariants: []sc.Invariant[certState, certContext]{
			{
				// OneRequestAtATime: a node asks for one thing at a time,
				// so it never races itself for the lease.
				Name:  "at most one request runs",
				Holds: func(s certSnapshot) bool { return len(s.Services()) <= 1 },
			},
			{
				Name: "only a held certificate is checked, windowed or renewed",
				Holds: func(s certSnapshot) bool {
					return s.Context.cert != nil || s.In(certWaiting) || s.In(certObtaining) || s.In(certRetrying) || s.In(certStopped)
				},
			},
		},
		Steps: []sc.StepInvariant[certState, certContext]{
			{
				// OneNewOrder: only a node holding nothing obtains; every
				// later certificate is a renewal.
				Name: "a node holding a certificate never obtains",
				Holds: func(st certStep) bool {
					for _, e := range requests(st, serviceObtain) {
						if e.Context.cert != nil {
							return false
						}
					}
					return true
				},
			},
			{
				// OneRenewalPerCycle: a renewal waits for the window every
				// slot agrees on.
				Name: "a renewal starts only once the window says so",
				Holds: func(st certStep) bool {
					for _, e := range requests(st, serviceRenew) {
						if e.Context.renewAt.IsZero() || st.At.Before(e.Context.renewAt) {
							return false
						}
					}
					return true
				},
			},
			{
				// Read before acting: a renewal waits for every read
				// that is due, as the gateway did before the chart.
				Name: "a renewal never starts while a store read or window read is due",
				Holds: func(st certStep) bool {
					for _, e := range requests(st, serviceRenew) {
						if !st.At.Before(e.Context.refreshAt) || !st.At.Before(e.Context.windowCheckAt) {
							return false
						}
					}
					return true
				},
			},
			{
				// NoOutage: a failed renewal keeps serving.
				Name: "a held certificate is only ever replaced",
				Holds: func(st certStep) bool {
					return st.From.Context.cert == nil || st.To.Context.cert != nil
				},
			},
			{
				Name: "a certificate is replaced only by a later one",
				Holds: func(st certStep) bool {
					from, to := st.From.Context.cert, st.To.Context.cert
					return from == nil || from == to || to.Leaf.NotAfter.After(from.Leaf.NotAfter)
				},
			},
		},
		Goals: []sc.Goal[certState, certContext]{
			{
				Name:     "can always get back to serving a certificate",
				Holds:    func(s certSnapshot) bool { return s.Context.cert != nil },
				Ignoring: []any{certStop{}},
			},
		},
	})
	require.NoError(t, err)
	t.Logf("explored %d states", report.States)
	for _, v := range report.Violations {
		t.Errorf("%s:\n%s", v.Invariant, certificateChart.FormatTrace(v.Trace))
	}
	require.False(t, report.Truncated, "explored %d states", report.States)
	require.Empty(t, report.Stuck)
	require.Empty(t, report.Trapped)
	for _, s := range []certState{certWaiting, certObtaining, certRetrying, certServing, certCheckingStore, certFetchingWindow, certRenewing, certRenewBackoff, certStopped} {
		require.Truef(t, report.Reached(s), "state %q is never reached", s)
	}

	// Every path the exploration found replays to the same snapshot.
	for path := range report.Paths() {
		snap, _ := certificateChart.Start(path.Initial, path.Start)
		for next := range certificateChart.Fold(snap, slices.Values(path.Events)) {
			snap = next
		}
		require.ElementsMatch(t, path.Snapshot.Active(), snap.Active())
	}
}

// shortLived is a certificate valid from notBefore to notAfter.
func shortLived(t *testing.T, notBefore, notAfter time.Time) *tls.Certificate {
	t.Helper()
	cert := testCertificate(t, notAfter)
	leaf := *cert.Leaf
	leaf.NotBefore = notBefore
	cert.Leaf = &leaf
	return cert
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
	clock := newTestClock(stateNow)
	var mu sync.Mutex
	var seen []string
	events := func(_ context.Context, operation CertificateOperation, result CertificateResult) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, string(operation)+":"+string(result))
	}
	tracker := startTestTracker(t, fetchSource{fetch: fetch.fetch}, farWindows{}, nil, events, clock.Now)

	_, _, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	clock.Advance(time.Hour)
	_, _, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"obtain:rate_limited", "obtain:ok", "window:ok"}, seen,
		"each obtain and window request is counted; the window is read right after obtaining")
}
