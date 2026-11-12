package tailnetgateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// errRateLimited is how a Let's Encrypt refusal reached the gateway on a real
// tailnet.
var errRateLimited = errors.New(`obtain tailnet service certificate: 429 urn:ietf:params:acme:error:rateLimited: too many certificates (50) already issued for "tail7d394.ts.net" in the last 168h0m0s, retry after 2026-11-12 22:53:32 UTC: see https://letsencrypt.org/docs/rate-limits/`)

// scriptedFetch returns each error in turn, then nil, counting calls.
type scriptedFetch struct {
	mu     sync.Mutex
	errs   []error
	calls  int
	called chan struct{}
}

func (f *scriptedFetch) fetch(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	var err error
	if len(f.errs) > 0 {
		err, f.errs = f.errs[0], f.errs[1:]
	}
	f.called <- struct{}{}
	return err
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
	fetch := &scriptedFetch{mu: sync.Mutex{}, errs: []error{errRateLimited}, calls: 0, called: make(chan struct{}, 4)}
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
	require.Equal(t, 2, fetch.count(), "a held certificate is not requested again")
}

func TestCertificateTrackerAwaitReturnsOnceTheFirstRequestSettles(t *testing.T) {
	t.Parallel()
	fetch := &scriptedFetch{mu: sync.Mutex{}, errs: nil, calls: 0, called: make(chan struct{}, 4)}
	tracker := newCertificateTracker("private.example.ts.net", fetch.fetch, testenv.NewLogger(t), time.Now)
	t.Cleanup(tracker.Close)

	tracker.Await(t.Context(), time.Minute)
	ready, _ := tracker.Poll()
	require.True(t, ready, "a normal start holds the certificate by its first heartbeat")

	failing := &scriptedFetch{mu: sync.Mutex{}, errs: []error{errRateLimited}, calls: 0, called: make(chan struct{}, 4)}
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
	fetch := &scriptedFetch{mu: sync.Mutex{}, errs: []error{pending, errRateLimited}, calls: 0, called: make(chan struct{}, 4)}
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
	fetch := &scriptedFetch{mu: sync.Mutex{}, errs: []error{errRateLimited, reset}, calls: 0, called: make(chan struct{}, 4)}
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
