package certificate

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/speakeasy-api/gram/sc"
)

// certificateTracker drives one node's certificate chart with an
// sc.Instance. The chart decides everything; the tracker turns polls into
// events, holds the pod-wide request slot for the request in flight, and
// reports each step to logs and the metrics callback from the instance's
// hooks.
type certificateTracker struct {
	domain string
	logger *slog.Logger
	now    func() time.Time

	// requests is a pod-wide semaphore bounding concurrent requests; nil
	// means unbounded. It is tried, never waited on: a node whose turn has
	// not come tries again at its next poll.
	requests chan struct{}

	// events, when set, is told the outcome of each obtain, renewal and
	// window request.
	events func(ctx context.Context, operation CertificateOperation, result CertificateResult)

	machine  *sc.Machine[State, certState]
	instance *sc.Instance[State, certState]

	// pollMu serializes polls, so two polls never both take a slot.
	pollMu sync.Mutex

	// held counts the request slots the tracker holds: one for the request
	// in flight, and briefly one for a poll the chart has not yet taken. It
	// is a count, not a flag, because a poll can see the last request's
	// Idle snapshot before that request's endRequest has run.
	held atomic.Int32

	closeOnce sync.Once
}

func newCertificateTracker(domain string, source certificateSource, windows renewalWindows, requests chan struct{}, logger *slog.Logger, now func() time.Time) *certificateTracker {
	t := &certificateTracker{
		domain: domain, logger: logger, now: now, requests: requests, events: nil,
		machine: nil, instance: nil, pollMu: sync.Mutex{}, held: atomic.Int32{}, closeOnce: sync.Once{},
	}
	chart, err := certificateChart()
	if err == nil {
		t.machine, err = bind(chart, source, windows, now, t.endRequest)
	}
	if err == nil {
		t.instance, err = t.machine.Instance(context.Background(), certState{
			Cert: nil, Err: nil, Failures: 0, RetryAt: time.Time{},
			RefreshAt: time.Time{}, WindowCheckAt: time.Time{}, RenewAt: time.Time{}, Steps: 0, RefreshIn: 0,
		}, sc.WithClock(now), sc.WithHooks(sc.JoinHooks(t.metricHooks(), t.logHooks())))
	}
	if err != nil {
		// The chart and its bindings are fixed; they fail only on a
		// declaration bug, which every test in this package would catch.
		panic(err)
	}
	return t
}

// Poll reports whether a valid certificate is held and, when it is not, why.
// It does not wait for a request: when one is due and a request slot is free,
// it hands the chart a poll, which starts the request in the background.
func (t *certificateTracker) Poll() (bool, string) {
	t.pollMu.Lock()
	defer t.pollMu.Unlock()

	now := t.now()
	snap := t.instance.Snapshot()
	ev := poll{RefreshIn: jittered(certificateRefreshInterval)}
	if t.machine.Can(snap, ev, now) && t.acquire() {
		t.held.Add(1)
		step, err := t.instance.Send(context.Background(), ev)
		if err != nil || !step.Accepted() {
			// Another poll or a step's result got in first.
			t.endRequest()
		}
	}
	return certStatus(snap, t.domain, now)
}

// endRequest gives back the request slot; the chart runs it whenever a
// request ends, including when the node stops during one.
func (t *certificateTracker) endRequest() {
	for {
		n := t.held.Load()
		if n == 0 {
			return
		}
		if t.held.CompareAndSwap(n, n-1) {
			t.release()
			return
		}
	}
}

// Certificate returns the certificate for a TLS handshake, failing the
// handshake until one is obtained. It never asks for one.
func (t *certificateTracker) Certificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := t.instance.Snapshot().Context.Cert
	if cert == nil {
		return nil, errCertificateNotObtained
	}
	return cert, nil
}

// ExpiresAt is when the certificate served expires, zero while none is held.
func (t *certificateTracker) ExpiresAt() time.Time {
	cert := t.instance.Snapshot().Context.Cert
	if cert == nil {
		return time.Time{}
	}
	return cert.Leaf.NotAfter
}

// Await polls until the certificate is held, wait elapses, ctx is done, or
// the next request is due only after wait would have ended anyway. A retry
// due within the wait, such as the short one while control has not yet
// listed the service name right after approval, is waited for.
func (t *certificateTracker) Await(ctx context.Context, wait time.Duration) {
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	deadline := t.now().Add(wait)

	ticker := time.NewTicker(certificateAwaitPollInterval)
	defer ticker.Stop()
	for {
		if ready, _ := t.Poll(); ready {
			return
		}
		if t.nextRequestAfter(deadline) {
			return
		}

		select {
		case <-waitCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

// nextRequestAfter reports whether a request has failed and the retry is not
// due until after deadline.
func (t *certificateTracker) nextRequestAfter(deadline time.Time) bool {
	snap := t.instance.Snapshot()
	return !snap.In(Requesting) && phaseOf(snap) == phaseRetrying && snap.Context.RetryAt.After(deadline)
}

// Close stops any request in flight and waits for it.
func (t *certificateTracker) Close() {
	t.closeOnce.Do(func() {
		// Leaving Requesting stops the step in flight and waits for it,
		// then gives back the request slot.
		_, _ = t.instance.Send(context.Background(), closeTracker{})
		t.instance.Close()
	})
}

// acquire takes a pod-wide request slot without waiting.
func (t *certificateTracker) acquire() bool {
	if t.requests == nil {
		return true
	}
	select {
	case t.requests <- struct{}{}:
		return true
	default:
		return false
	}
}

func (t *certificateTracker) release() {
	if t.requests != nil {
		<-t.requests
	}
}

// stepOutcome is the service whose result step processed, and its error.
func stepOutcome(step sc.Step[State, certState]) (string, error, bool) {
	run, ok := step.Service()
	if !ok {
		return "", nil, false
	}
	var err error
	if failed, isFailed := step.Event.(sc.Failed); isFailed {
		err = failed.Err
	}
	return run.Name, err, true
}

// metricHooks tell events the outcome of each certificate request.
func (t *certificateTracker) metricHooks() *sc.Hooks[State, certState] {
	return &sc.Hooks[State, certState]{
		Transition: func(ctx context.Context, step sc.Step[State, certState]) {
			service, err, ok := stepOutcome(step)
			if !ok || step.To.In(Stopped) || t.events == nil {
				return
			}
			operation, ok := certificateOperation(service)
			if !ok {
				return
			}
			// A renewal returning the certificate it replaced fails in the
			// chart.
			if service == serviceRenew && err == nil && step.To.Context.Err != nil {
				err = step.To.Context.Err
			}
			t.events(ctx, operation, certificateResult(err))
		},
		Ignored: nil, EffectStarted: nil, EffectFinished: nil,
	}
}

// logHooks report what a step changed: the original's log.
func (t *certificateTracker) logHooks() *sc.Hooks[State, certState] {
	return &sc.Hooks[State, certState]{Transition: t.log, Ignored: nil, EffectStarted: nil, EffectFinished: nil}
}

func (t *certificateTracker) log(ctx context.Context, step sc.Step[State, certState]) {
	service, err, ok := stepOutcome(step)
	if !ok || step.To.In(Stopped) {
		return
	}
	before, after := step.From.Context, step.To.Context

	if after.Cert != nil && (before.Cert == nil || !before.Cert.Leaf.Equal(after.Cert.Leaf)) {
		t.logger.InfoContext(ctx, "obtained tailnet service certificate", slog.String("host.name", t.domain), slog.Time("certificate.expires_at", after.Cert.Leaf.NotAfter))
	}
	if err == nil {
		// As in the original, a renewal returning the certificate it
		// replaced is counted as an error but not logged.
		return
	}
	if errors.Is(err, errCertificateIssuing) {
		// Waiting for another slot's lease is expected, and retried every
		// few seconds; it is not worth a warning each time.
		t.logger.DebugContext(ctx, "waiting for another gateway slot to obtain the tailnet service certificate", slog.String("host.name", t.domain))
		return
	}
	switch service {
	case serviceCheckStore:
		t.logger.WarnContext(ctx, "could not read the stored tailnet service certificate", slog.String("host.name", t.domain), slog.Any("error", err))
	case serviceWindow:
		t.logger.WarnContext(ctx, "could not read the certificate renewal window; renewing two thirds through its lifetime",
			slog.String("host.name", t.domain), slog.Any("error", err))
	case serviceRenew:
		t.logger.WarnContext(ctx, "could not renew tailnet service certificate",
			slog.String("host.name", t.domain), slog.Any("error", after.Err), slog.Time("certificate.expires_at", after.Cert.Leaf.NotAfter),
			slog.Int("retry.attempt", after.Failures), slog.Duration("retry.wait", after.RetryAt.Sub(t.now())))
	case serviceObtain:
		t.logger.WarnContext(ctx, "could not obtain tailnet service certificate",
			slog.String("host.name", t.domain), slog.Any("error", after.Err),
			slog.Int("retry.attempt", after.Failures), slog.Duration("retry.wait", after.RetryAt.Sub(t.now())))
	}
}
