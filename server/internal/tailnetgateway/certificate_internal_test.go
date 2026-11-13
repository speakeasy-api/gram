package tailnetgateway

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
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// errRateLimited is how a Let's Encrypt refusal reached the gateway on a real
// tailnet.
var errRateLimited = errors.New(`obtain tailnet service certificate: 429 urn:ietf:params:acme:error:rateLimited: too many certificates (50) already issued for "tail7d394.ts.net" in the last 168h0m0s, retry after 2026-11-12 22:53:32 UTC: see https://letsencrypt.org/docs/rate-limits/`)

// testCertificatePEM is a self-signed certificate and key for
// private.example.ts.net, valid until notAfter.
func testCertificatePEM(t *testing.T, notAfter time.Time) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "private.example.ts.net"},
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

// settle waits for the request goroutine to finish recording its result.
func settle(t *testing.T, tracker *certificateTracker) {
	t.Helper()
	require.Eventually(t, func() bool {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		return !tracker.attempting
	}, 5*time.Second, 5*time.Millisecond)
}

func TestCertificateTrackerWaitsForTheRetryTimeAfterARateLimit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	fetch := newScriptedFetch(t, errRateLimited)
	tracker := newCertificateTracker("private.example.ts.net", fetch.fetch, testenv.NewLogger(t), func() time.Time { return now })
	t.Cleanup(tracker.Close)

	ready, _ := tracker.Poll()
	require.False(t, ready)
	<-fetch.called
	settle(t, tracker)

	ready, reason := tracker.Poll()
	require.False(t, ready)
	require.Equal(t, "Let's Encrypt is rate limiting new certificates for this tailnet; retrying after 2026-11-12 22:53 UTC", reason)
	require.Equal(t, 1, fetch.count(), "no new request before Let's Encrypt's retry time")

	now = time.Date(2026, time.November, 12, 22, 53, 33, 0, time.UTC)
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
	tracker := newCertificateTracker("private.example.ts.net", fetch.fetch, testenv.NewLogger(t), time.Now)
	t.Cleanup(tracker.Close)

	tracker.Await(t.Context(), time.Minute)
	ready, _ := tracker.Poll()
	require.True(t, ready, "a normal start holds the certificate by its first heartbeat")

	failing := newScriptedFetch(t, errRateLimited)
	// A fixed clock an hour before the refusal's retry time, so the retry
	// falls after the wait whatever the time of day.
	before := time.Date(2026, time.November, 12, 21, 53, 0, 0, time.UTC)
	refused := newCertificateTracker("private.example.ts.net", failing.fetch, testenv.NewLogger(t), func() time.Time { return before })
	t.Cleanup(refused.Close)
	started := time.Now()
	refused.Await(t.Context(), time.Minute)
	require.Less(t, time.Since(started), 30*time.Second, "a refusal ends the wait instead of holding the start for the full minute")
	ready, _ = refused.Poll()
	require.False(t, ready)
}

func TestCertificateTrackerWaitsThroughTheShortRetryAfterApproval(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	pending := errors.New(`obtain tailnet service certificate: invalid domain "private.example.ts.net"; must be one of []`)
	fetch := newScriptedFetch(t, pending, errRateLimited)
	tracker := newCertificateTracker("private.example.ts.net", fetch.fetch, testenv.NewLogger(t), func() time.Time { return now })
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	require.False(t, tracker.nextRequestAfter(now.Add(certificateStartWait)),
		"control lists the service name within seconds of approval, so a start keeps waiting")

	now = now.Add(certificateDomainRetry)
	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	require.True(t, tracker.nextRequestAfter(now.Add(certificateStartWait)),
		"a rate limit retries long after the start would have ended, so the start stops waiting")
}

func TestCertificateTrackerBacksOffOnlyForRealFailures(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	reset := errors.New("acme: connection reset")
	fetch := newScriptedFetch(t, errRateLimited, reset)
	tracker := newCertificateTracker("private.example.ts.net", fetch.fetch, testenv.NewLogger(t), func() time.Time { return now })
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	now = time.Date(2026, time.November, 12, 22, 54, 0, 0, time.UTC)
	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	require.Equal(t, 1, tracker.failures, "the rate limit named its own retry time and does not inflate the backoff")
	require.Equal(t, now.Add(certificateRetryMin), tracker.retryAt, "the first real failure backs off by the minimum")
}

func TestCertificateTrackerWaitsForAnotherSlotWithoutBackingOff(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	fetch := newScriptedFetch(t, errCertificateIssuing)
	tracker := newCertificateTracker("private.example.ts.net", fetch.fetch, testenv.NewLogger(t), func() time.Time { return now })
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	<-fetch.called
	settle(t, tracker)
	ready, reason := tracker.Poll()
	require.False(t, ready)
	require.Equal(t, "another gateway slot is obtaining the TLS certificate for private.example.ts.net", reason)
	require.False(t, tracker.nextRequestAfter(now.Add(certificateStartWait)), "a start waits for the other slot to store the certificate")

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	require.Zero(t, tracker.failures, "waiting for another slot is not a failure")
	require.Equal(t, now.Add(certificateIssuingRetry), tracker.retryAt)
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

// TestCertificateLeaseOutlastsALiveRequest holds the invariant the issuance
// lease relies on: a slot claims the lease inside a request bounded by
// certificateFetchTimeout and then releases it within stateStoreTimeout. If
// the lease could lapse first, another slot could claim it and both would ask
// Let's Encrypt for a certificate, and the late release would delete the other
// slot's lease.
func TestCertificateLeaseOutlastsALiveRequest(t *testing.T) {
	t.Parallel()

	require.Less(t, certificateFetchTimeout+stateStoreTimeout, certificateLeaseDuration,
		"the issuance lease must outlast a request and its release")
}

func TestCertificateHandshakeNeverRequestsACertificate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	fetch := newScriptedFetch(t)
	tracker := newCertificateTracker("private.example.ts.net", fetch.fetch, testenv.NewLogger(t), func() time.Time { return now })
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

func TestCertificateTrackerRefreshesAHeldCertificate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	notAfter := now.Add(30 * 24 * time.Hour)
	first, renewed := testCertificate(t, notAfter), testCertificate(t, notAfter.Add(60*24*time.Hour))
	results := []struct {
		cert *tls.Certificate
		err  error
	}{{cert: first}, {err: errors.New("acme: connection reset")}, {cert: renewed}}
	var calls atomic.Int32
	called := make(chan struct{}, 4)
	fetch := func(context.Context) (*tls.Certificate, error) {
		result := results[calls.Add(1)-1]
		called <- struct{}{}
		return result.cert, result.err
	}
	tracker := newCertificateTracker("private.example.ts.net", fetch, testenv.NewLogger(t), func() time.Time { return now })
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	<-called
	settle(t, tracker)
	ready, _ := tracker.Poll()
	require.True(t, ready)

	now = now.Add(certificateRefreshInterval)
	ready, _ = tracker.Poll()
	require.True(t, ready, "a refresh in flight keeps serving the held certificate")
	<-called
	settle(t, tracker)
	ready, _ = tracker.Poll()
	require.True(t, ready, "a failed refresh keeps serving the held certificate")
	cert, err := tracker.Certificate(nil)
	require.NoError(t, err)
	require.Same(t, first, cert)

	now = now.Add(certificateRetryMin)
	_, _ = tracker.Poll()
	<-called
	settle(t, tracker)
	cert, err = tracker.Certificate(nil)
	require.NoError(t, err)
	require.Same(t, renewed, cert, "a refresh picks up the renewed certificate")
	require.Equal(t, int32(3), calls.Load())
}

func TestCertificateTrackerReportsAnExpiredCertificate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	expiring := testCertificate(t, now.Add(time.Minute))
	var calls atomic.Int32
	called := make(chan struct{}, 4)
	fetch := func(context.Context) (*tls.Certificate, error) {
		called <- struct{}{}
		if calls.Add(1) == 1 {
			return expiring, nil
		}
		return nil, errors.New("acme: connection reset")
	}
	tracker := newCertificateTracker("private.example.ts.net", fetch, testenv.NewLogger(t), func() time.Time { return now })
	t.Cleanup(tracker.Close)

	_, _ = tracker.Poll()
	<-called
	settle(t, tracker)
	now = now.Add(certificateRefreshInterval)
	_, _ = tracker.Poll()
	<-called
	settle(t, tracker)

	ready, reason := tracker.Poll()
	require.False(t, ready, "an expired certificate does not count as held")
	require.Contains(t, reason, "acme: connection reset")
}

// fakeIssuer is a connection's shared certificate store and issuance lease,
// as PostgresStateStore keeps them, in memory.
type fakeIssuer struct {
	ipn.StateStore

	mu              sync.Mutex
	certPEM, keyPEM []byte
	leased          bool
}

func (f *fakeIssuer) ReadTLSCertAndKey(string) ([]byte, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.certPEM == nil {
		return nil, nil, ipn.ErrStateNotExist
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

func (f *fakeIssuer) store(certPEM, keyPEM []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.certPEM, f.keyPEM = certPEM, keyPEM
}

func TestSlotsOfOneConnectionOrderOneCertificate(t *testing.T) {
	t.Parallel()
	issuer := &fakeIssuer{StateStore: nil, mu: sync.Mutex{}, certPEM: nil, keyPEM: nil, leased: false}
	certPEM, keyPEM := testCertificatePEM(t, time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC))
	var orders atomic.Int32
	release := make(chan struct{})
	// certPair stands in for tsnet: it returns the stored certificate, or
	// orders one, held open until release, and stores it.
	certPair := func(ctx context.Context, domain string) ([]byte, []byte, error) {
		if stored, key, err := issuer.ReadTLSCertAndKey(domain); err == nil {
			return stored, key, nil
		}
		orders.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
		issuer.store(certPEM, keyPEM)
		return certPEM, keyPEM, nil
	}

	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	var clock sync.Mutex
	nowFn := func() time.Time {
		clock.Lock()
		defer clock.Unlock()
		return now
	}
	const slots = 4
	trackers := make([]*certificateTracker, slots)
	for i := range trackers {
		trackers[i] = newCertificateTracker("private.example.ts.net", leasedCertificateFetch(issuer, "private.example.ts.net", certPair), testenv.NewLogger(t), nowFn)
		t.Cleanup(trackers[i].Close)
	}

	// Every slot is approved at once and every slot gets client handshakes
	// while the first order is held open.
	for _, tracker := range trackers {
		_, _ = tracker.Poll()
	}
	require.Eventually(t, func() bool { return orders.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	for _, tracker := range trackers {
		for range 4 {
			_, err := tracker.Certificate(&tls.ClientHelloInfo{ServerName: "private.example.ts.net"})
			require.ErrorIs(t, err, errCertificateNotObtained)
		}
	}
	close(release)

	clock.Lock()
	now = now.Add(certificateIssuingRetry)
	clock.Unlock()
	require.Eventually(t, func() bool {
		for _, tracker := range trackers {
			if ready, _ := tracker.Poll(); !ready {
				return false
			}
		}
		return true
	}, 5*time.Second, 5*time.Millisecond, "the other slots reuse the stored certificate")
	require.Equal(t, int32(1), orders.Load(), "the connection orders exactly one certificate")
}
