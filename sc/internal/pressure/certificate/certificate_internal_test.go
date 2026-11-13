package certificate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// The tracker tests of the original certificate_internal_test.go, run
// against the chart through an sc.Instance. Each keeps its name and
// assertions; only reads of the tracker's internals change, from its
// certState to the instance's snapshot.

// errRateLimited is how a Let's Encrypt refusal reached the gateway on a real
// tailnet.
var errRateLimited = errors.New(`obtain tailnet service certificate: 429 urn:ietf:params:acme:error:rateLimited: too many certificates (50) already issued for "tail7d394.ts.net" in the last 168h0m0s, retry after 2026-11-12 22:53:32 UTC: see https://letsencrypt.org/docs/rate-limits/`)

// serials makes every test certificate's serial distinct, which the original
// got from time.Now().UnixNano().
var serials atomic.Int64

// testCertificatePEM is a self-signed certificate and key for
// private.example.ts.net, valid until notAfter.
func testCertificatePEM(t *testing.T, notAfter time.Time) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + serials.Add(1)), Subject: pkix.Name{CommonName: "private.example.ts.net"},
		DNSNames:  []string{"private.example.ts.net"},
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// testCertificate is a parsed certificate valid until notAfter.
func testCertificate(t *testing.T, notAfter time.Time) *tls.Certificate {
	t.Helper()
	certPEM, keyPEM := testCertificatePEM(t, notAfter)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return &pair
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// scriptedFetch returns each error in turn, then cert, counting calls.
type scriptedFetch struct {
	mu     sync.Mutex
	errs   []error
	cert   *tls.Certificate
	calls  int
	called chan struct{}
}

func newScriptedFetch(t *testing.T, errs ...error) *scriptedFetch {
	t.Helper()
	return &scriptedFetch{mu: sync.Mutex{}, errs: errs, cert: testCertificate(t, time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)), calls: 0, called: make(chan struct{}, 8)}
}

func (f *scriptedFetch) fetch(context.Context) (*tls.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	var err error
	if len(f.errs) > 0 {
		err, f.errs = f.errs[0], f.errs[1:]
	}
	f.called <- struct{}{}
	if err != nil {
		return nil, err
	}
	return f.cert, nil
}

func (f *scriptedFetch) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fetchSource obtains certificates from fetch, stores none and never renews.
type fetchSource struct {
	fetch func(context.Context) (*tls.Certificate, error)
}

func (fetchSource) Stored(context.Context) (*tls.Certificate, error) { return nil, nil }

func (f fetchSource) Obtain(ctx context.Context, _ time.Time) (*tls.Certificate, error) {
	return f.fetch(ctx)
}

func (fetchSource) Renew(context.Context, *tls.Certificate, time.Time) (*tls.Certificate, error) {
	return nil, errors.New("this source does not renew")
}

// farWindows never asks for a renewal.
type farWindows struct{}

func (farWindows) RenewAt(_ context.Context, _ *x509.Certificate, now time.Time) (time.Time, time.Time, error) {
	return now.Add(1000 * time.Hour), now.Add(renewalCheckMax), nil
}

func newTestTracker(t *testing.T, fetch func(context.Context) (*tls.Certificate, error), now func() time.Time) *certificateTracker {
	t.Helper()
	return newCertificateTracker("private.example.ts.net", fetchSource{fetch: fetch}, farWindows{}, nil, testLogger(t), now)
}

// settle waits for the request in flight to finish recording its result.
// Await returns as soon as the snapshot leaves Requesting, which is before
// the step's actions, endRequest among them, have run; Advance takes the
// instance's turn, so it returns only once they have.
func settle(t *testing.T, tracker *certificateTracker) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := tracker.instance.Await(ctx, func(s sc.Snapshot[State, certState]) bool { return !s.In(Requesting) })
	require.NoError(t, err)
	_, err = tracker.instance.Advance(ctx)
	require.NoError(t, err)
}

// state is the tracker's chart context.
func state(tracker *certificateTracker) certState { return tracker.instance.Snapshot().Context }

// attempting is whether the tracker has a request in flight.
func attempting(tracker *certificateTracker) bool { return tracker.instance.Snapshot().In(Requesting) }

func TestCertificateTrackerWaitsForTheRetryTimeAfterARateLimit(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	fetch := newScriptedFetch(t, errRateLimited)
	tracker := newTestTracker(t, fetch.fetch, clock)
	t.Cleanup(tracker.Close)

	ready, _ := tracker.Poll()
	require.False(t, ready)
	<-fetch.called
	settle(t, tracker)

	ready, reason := tracker.Poll()
	require.False(t, ready)
	require.Equal(t, "Let's Encrypt is rate limiting new certificates for this tailnet; retrying after 2026-11-12 22:53 UTC", reason)
	require.Equal(t, 1, fetch.count(), "no new request before Let's Encrypt's retry time")

	mu.Lock()
	now = time.Date(2026, time.November, 12, 22, 53, 33, 0, time.UTC)
	mu.Unlock()
	ready, _ = tracker.Poll()
	require.False(t, ready)
	<-fetch.called
	settle(t, tracker)
	ready, reason = tracker.Poll()
	require.True(t, ready, "the retry after the window reopens obtains the certificate")
	require.Empty(t, reason)
	require.Equal(t, 2, fetch.count())

	_, _ = tracker.Poll()
	require.Equal(t, 2, fetch.count(), "a held certificate is not requested again before its refresh")
}

func TestCertificateTrackerAwaitReturnsOnceTheFirstRequestSettles(t *testing.T) {
	t.Parallel()
	fetch := newScriptedFetch(t)
	tracker := newTestTracker(t, fetch.fetch, time.Now)
	t.Cleanup(tracker.Close)

	tracker.Await(t.Context(), time.Minute)
	ready, _ := tracker.Poll()
	require.True(t, ready, "a normal start holds the certificate by its first heartbeat")

	failing := newScriptedFetch(t, errRateLimited)
	// A fixed clock an hour before the refusal's retry time, so the retry
	// falls after the wait whatever the time of day.
	before := time.Date(2026, time.November, 12, 21, 53, 0, 0, time.UTC)
	refused := newTestTracker(t, failing.fetch, func() time.Time { return before })
	t.Cleanup(refused.Close)
	started := time.Now()
	refused.Await(t.Context(), time.Minute)
	require.Less(t, time.Since(started), 30*time.Second, "a refusal ends the wait instead of holding the start for the full minute")
	ready, _ = refused.Poll()
	require.False(t, ready)
}

func TestCertificateTrackerWaitsThroughTheShortRetryAfterApproval(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	pending := errors.New(`obtain tailnet service certificate: invalid domain "private.example.ts.net"; must be one of []`)
	fetch := newScriptedFetch(t, pending, errRateLimited)
	tracker := newTestTracker(t, fetch.fetch, clock)
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	require.False(t, tracker.nextRequestAfter(clock().Add(certificateStartWait)),
		"control lists the service name within seconds of approval, so a start keeps waiting")

	mu.Lock()
	now = now.Add(certificateDomainRetry)
	mu.Unlock()
	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	require.True(t, tracker.nextRequestAfter(clock().Add(certificateStartWait)),
		"a rate limit retries long after the start would have ended, so the start stops waiting")
}

func TestCertificateTrackerBacksOffOnlyForRealFailures(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	reset := errors.New("acme: connection reset")
	fetch := newScriptedFetch(t, errRateLimited, reset)
	tracker := newTestTracker(t, fetch.fetch, clock)
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	mu.Lock()
	now = time.Date(2026, time.November, 12, 22, 54, 0, 0, time.UTC)
	mu.Unlock()
	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)

	st := state(tracker)
	require.Equal(t, 1, st.Failures, "the rate limit named its own retry time and does not inflate the backoff")
	require.Equal(t, clock().Add(certificateRetryMin), st.RetryAt, "the first real failure backs off by the minimum")
}

func TestCertificateTrackerWaitsForAnotherSlotWithoutBackingOff(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	fetch := newScriptedFetch(t, errCertificateIssuing)
	tracker := newTestTracker(t, fetch.fetch, func() time.Time { return now })
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	ready, reason := tracker.Poll()
	require.False(t, ready)
	require.Equal(t, "another gateway slot is obtaining the TLS certificate for private.example.ts.net", reason)
	require.False(t, tracker.nextRequestAfter(now.Add(certificateStartWait)), "a start waits for the other slot to store the certificate")

	st := state(tracker)
	require.Zero(t, st.Failures, "waiting for another slot is not a failure")
	require.Equal(t, now.Add(certificateIssuingRetry), st.RetryAt)
}

func TestNextCertificateRetry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)

	require.Equal(t, time.Date(2026, time.November, 12, 22, 53, 32, 0, time.UTC), nextCertificateRetry(now, errRateLimited, 1),
		"a rate limit retries at the time Let's Encrypt names")
	require.Equal(t, now.Add(certificateRetryMax), nextCertificateRetry(now, errors.New("acme: rateLimited"), 1),
		"a rate limit without a time waits the longest backoff")
	require.Equal(t, now.Add(certificateDomainRetry), nextCertificateRetry(now, errors.New(`invalid domain "private.example.ts.net"; must be one of []`), 1),
		"control lists the service name shortly after approval")

	other := errors.New("acme: connection reset")
	require.Equal(t, now.Add(certificateRetryMin), nextCertificateRetry(now, other, 1))
	require.Equal(t, now.Add(2*certificateRetryMin), nextCertificateRetry(now, other, 2))
	require.Equal(t, now.Add(certificateRetryMax), nextCertificateRetry(now, other, 20), "the backoff is capped")
}

func TestCertificateFailureMessage(t *testing.T) {
	t.Parallel()
	retryAt := time.Date(2026, time.November, 12, 22, 53, 32, 0, time.UTC)

	require.Equal(t, "Let's Encrypt is rate limiting new certificates for this tailnet; retrying after 2026-11-12 22:53 UTC",
		certificateFailureMessage("private.example.ts.net", errRateLimited, retryAt))
	require.Equal(t, "could not obtain a TLS certificate for private.example.ts.net: acme: connection reset; retrying after 2026-11-12 22:53 UTC",
		certificateFailureMessage("private.example.ts.net", errors.New("acme: connection reset"), retryAt))
}

func TestCertificateLeaseOutlastsALiveRequest(t *testing.T) {
	t.Parallel()

	require.Less(t, certificateFetchTimeout+stateStoreTimeout, certificateLeaseDuration,
		"the issuance lease must outlast a request and its release")
}

func TestCertificateHandshakeNeverRequestsACertificate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	fetch := newScriptedFetch(t)
	tracker := newTestTracker(t, fetch.fetch, func() time.Time { return now })
	t.Cleanup(tracker.Close)

	handshakes := sync.WaitGroup{}
	for range 16 {
		handshakes.Go(func() {
			_, err := tracker.Certificate(&tls.ClientHelloInfo{ServerName: "private.example.ts.net"})
			require.ErrorIs(t, err, errCertificateNotObtained)
		})
	}
	handshakes.Wait()
	require.Zero(t, fetch.count(), "a client connecting before the certificate is held asks for nothing")

	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	cert, err := tracker.Certificate(&tls.ClientHelloInfo{ServerName: "private.example.ts.net"})
	require.NoError(t, err)
	require.Same(t, fetch.cert, cert, "a handshake serves the certificate the tracker obtained")
	require.Equal(t, 1, fetch.count())
}

// fakeIssuer is a connection's shared certificate store and issuance lease,
// as PostgresStateStore keeps them, in memory.
type fakeIssuer struct {
	mu              sync.Mutex
	certPEM, keyPEM []byte
	leased          bool
}

func (f *fakeIssuer) ReadTLSCertAndKey(string) ([]byte, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.certPEM == nil {
		return nil, nil, errStateNotExist
	}
	return f.certPEM, f.keyPEM, nil
}

func (f *fakeIssuer) ClaimCertificateIssuance(context.Context) (func(), bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leased {
		return nil, false, nil
	}
	f.leased = true
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.leased = false
	}, true, nil
}

func (f *fakeIssuer) isLeased() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leased
}

func (f *fakeIssuer) store(certPEM, keyPEM []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.certPEM, f.keyPEM = certPEM, keyPEM
}

// fakeTsnet stands in for tsnet's certificate handling over a fakeIssuer:
// CertPair returns the stored certificate or orders one; CertPairWithValidity
// renews. Each order is held open until hold is closed, and counted.
type fakeTsnet struct {
	t        *testing.T
	issuer   *fakeIssuer
	hold     chan struct{}
	notAfter func() time.Time

	orders, renewals atomic.Int32
}

func (f *fakeTsnet) order(ctx context.Context) ([]byte, []byte, error) {
	select {
	case <-f.hold:
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("order held open: %w", ctx.Err())
	}
	certPEM, keyPEM := testCertificatePEM(f.t, f.notAfter())
	f.issuer.store(certPEM, keyPEM)
	return certPEM, keyPEM, nil
}

func (f *fakeTsnet) certPair(ctx context.Context, domain string) ([]byte, []byte, error) {
	if stored, key, err := f.issuer.ReadTLSCertAndKey(domain); err == nil {
		return stored, key, nil
	}
	f.orders.Add(1)
	return f.order(ctx)
}

func (f *fakeTsnet) certPairWithValidity(ctx context.Context, _ string, _ time.Duration) ([]byte, []byte, error) {
	f.renewals.Add(1)
	return f.order(ctx)
}

// testClock is a clock tests move by hand.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// scriptedWindows answers RenewAt, counting calls: with err when set, with
// renewAt when set, and otherwise afterIssue into the certificate's lifetime,
// as Let's Encrypt does for a new certificate.
type scriptedWindows struct {
	mu         sync.Mutex
	renewAt    time.Time
	movedFor   string
	afterIssue time.Duration
	err        error
	calls      int
}

func (w *scriptedWindows) RenewAt(_ context.Context, leaf *x509.Certificate, now time.Time) (time.Time, time.Time, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.err != nil {
		return time.Time{}, time.Time{}, w.err
	}
	if !w.renewAt.IsZero() && (w.movedFor == "" || w.movedFor == leaf.SerialNumber.String()) {
		return w.renewAt, now.Add(renewalCheckMax), nil
	}
	return leaf.NotBefore.Add(w.afterIssue), now.Add(renewalCheckMax), nil
}

// moveTo moves the window of the certificate with serial to at.
func (w *scriptedWindows) moveTo(serial string, at time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.renewAt, w.movedFor = at, serial
}

func (w *scriptedWindows) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

// connection is the slots of one connection sharing a store, a fake tsnet
// and renewal windows.
type connection struct {
	clock    *testClock
	issuer   *fakeIssuer
	tsnet    *fakeTsnet
	windows  *scriptedWindows
	trackers []*certificateTracker
}

func newConnection(t *testing.T, slots int) *connection {
	t.Helper()
	// The original starts the clock on 12 November 2026. Here it starts at
	// the wall clock's time, and only ever moves forward from it: the
	// Within budget of a request is a time on this clock, which the driver
	// applies as a context deadline on the wall clock, so a clock behind
	// the wall clock fails every step at once with DeadlineExceeded.
	clock := &testClock{mu: sync.Mutex{}, now: time.Now().UTC()}
	issuer := &fakeIssuer{mu: sync.Mutex{}, certPEM: nil, keyPEM: nil, leased: false}
	c := &connection{
		clock: clock, issuer: issuer,
		tsnet:   &fakeTsnet{t: t, issuer: issuer, hold: make(chan struct{}), notAfter: func() time.Time { return clock.Now().Add(90 * 24 * time.Hour) }},
		windows: &scriptedWindows{mu: sync.Mutex{}, renewAt: time.Time{}, movedFor: "", afterIssue: 60 * 24 * time.Hour, err: nil, calls: 0},
	}
	for range slots {
		source := &sharedSource{store: issuer, fqdn: "private.example.ts.net", certPair: c.tsnet.certPair, certPairWithValidity: c.tsnet.certPairWithValidity}
		tracker := newCertificateTracker("private.example.ts.net", source, c.windows, nil, testLogger(t), clock.Now)
		t.Cleanup(tracker.Close)
		c.trackers = append(c.trackers, tracker)
	}
	return c
}

// pollUntil polls every slot until each satisfies done. A slot waiting for
// another to finish under the lease retries after certificateIssuingRetry, so
// once the lease is free the clock moves on by that much.
func (c *connection) pollUntil(t *testing.T, done func(certState) bool, msg string) {
	t.Helper()
	require.Eventually(t, func() bool {
		all, waiting := true, false
		for _, tracker := range c.trackers {
			_, _ = tracker.Poll()
			idle := !attempting(tracker)
			st := state(tracker)
			ok := idle && done(st)
			waiting = waiting || (idle && errors.Is(st.Err, errCertificateIssuing))
			all = all && ok
		}
		if !all && waiting && !c.issuer.isLeased() {
			c.clock.Advance(certificateIssuingRetry)
		}
		return all
	}, 5*time.Second, 5*time.Millisecond, msg)
}

// served is the serial of the certificate a slot serves, or "" for none.
func served(st certState) string {
	if st.Cert == nil {
		return ""
	}
	return st.Cert.Leaf.SerialNumber.String()
}

func storedSerial(t *testing.T, issuer *fakeIssuer) string {
	t.Helper()
	certPEM, keyPEM, err := issuer.ReadTLSCertAndKey("private.example.ts.net")
	require.NoError(t, err)
	pair, err := parseCertificatePair(certPEM, keyPEM)
	require.NoError(t, err)
	return pair.Leaf.SerialNumber.String()
}

func TestSlotsOfOneConnectionOrderOneCertificate(t *testing.T) {
	t.Parallel()
	c := newConnection(t, 4)

	// Every slot is approved at once and every slot gets client handshakes
	// while the first order is held open.
	for _, tracker := range c.trackers {
		_, _ = tracker.Poll()
	}
	require.Eventually(t, func() bool { return c.tsnet.orders.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	for _, tracker := range c.trackers {
		for range 4 {
			_, err := tracker.Certificate(&tls.ClientHelloInfo{ServerName: "private.example.ts.net"})
			require.ErrorIs(t, err, errCertificateNotObtained)
		}
	}
	close(c.tsnet.hold)

	c.pollUntil(t, func(st certState) bool { return st.Cert != nil }, "the other slots reuse the stored certificate")
	require.Equal(t, int32(1), c.tsnet.orders.Load(), "the connection orders exactly one certificate")
	require.Zero(t, c.tsnet.renewals.Load())
}

func TestSlotsOfOneConnectionRenewOncePerCycle(t *testing.T) {
	t.Parallel()
	c := newConnection(t, 3)
	close(c.tsnet.hold)
	c.pollUntil(t, func(st certState) bool { return st.Cert != nil }, "every slot holds the first certificate")
	first := storedSerial(t, c.issuer)

	// Hourly refreshes before the renewal point read the window once and
	// renew nothing.
	for range 3 {
		c.clock.Advance(pastRefresh)
		c.pollUntil(t, func(st certState) bool { return served(st) == first }, "slots keep the first certificate")
	}
	require.Zero(t, c.tsnet.renewals.Load())
	require.Equal(t, 3, c.windows.count(), "each slot reads the window once on obtaining it, then waits for Retry-After")

	// The renewal point arrives: all three slots fall due in the same hour.
	c.clock.Advance(60 * 24 * time.Hour)
	c.pollUntil(t, func(st certState) bool {
		return (served(st) != first && st.Err == nil) || errors.Is(st.Err, errCertificateIssuing)
	}, "each slot has renewed or is waiting for the one that is")
	renewed := storedSerial(t, c.issuer)
	c.pollUntil(t, func(st certState) bool { return served(st) == renewed }, "every slot serves the renewal")
	require.Equal(t, int32(1), c.tsnet.renewals.Load(), "the connection renews once per cycle, not once per slot")
	require.Equal(t, int32(1), c.tsnet.orders.Load(), "a renewal is never a new order")
}

func TestSlotRenewsWhenTheWindowMovesEarlier(t *testing.T) {
	t.Parallel()
	c := newConnection(t, 1)
	close(c.tsnet.hold)
	c.pollUntil(t, func(st certState) bool { return st.Cert != nil }, "the slot holds the first certificate")
	first := storedSerial(t, c.issuer)
	c.clock.Advance(pastRefresh)
	c.pollUntil(t, func(certState) bool { return true }, "the slot refreshes")
	require.Zero(t, c.tsnet.renewals.Load())

	// Let's Encrypt moves the window to now, as before a mass revocation.
	c.windows.moveTo(first, c.clock.Now())
	c.clock.Advance(renewalCheckMax)
	c.pollUntil(t, func(st certState) bool { return served(st) != first }, "the slot renews within one window check")
	require.Equal(t, int32(1), c.tsnet.renewals.Load())
}

func TestCertificateRequestsShareAPodWideLimit(t *testing.T) {
	t.Parallel()
	requests := make(chan struct{}, 1)
	requests <- struct{}{}
	fetch := newScriptedFetch(t)
	tracker := newCertificateTracker("private.example.ts.net", fetchSource{fetch: fetch.fetch}, farWindows{}, requests, testLogger(t), time.Now)
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	require.Zero(t, fetch.count(), "a node whose turn has not come does not start a request")
	<-requests
	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	require.Empty(t, requests, "the request gives its slot back")
}

// pastRefresh is long enough for a held certificate's jittered refresh to
// fall due.
const pastRefresh = certificateRefreshInterval + certificateRefreshInterval/10 + time.Second
