package tailnetgateway

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// A node serves its Tailscale Service over HTTPS with a certificate Tailscale
// obtains from Let's Encrypt for the service name. Approval alone does not
// make a host usable: until the certificate is issued every TLS handshake
// fails. tsnet only asks for one when a client connects, or in an hourly
// refresh, and reports a refusal nowhere but in its logs. Let's Encrypt
// allows a tailnet 50 new certificates a week, shared with everything else on
// that tailnet, so a refusal is ordinary. The node therefore requests the
// certificate itself as soon as it is approved, keeps retrying, and reports a
// host without one as awaiting a certificate rather than online.
const (
	// certificateStartWait is how long Start waits for the certificate after
	// approval. First issuance usually takes a few seconds, so a normal start
	// reports online at its first heartbeat instead of briefly reporting a
	// certificate wait.
	certificateStartWait = 30 * time.Second

	// certificateFetchTimeout bounds one request, which runs the full ACME
	// exchange when no certificate is stored yet.
	certificateFetchTimeout = 2 * time.Minute

	// certificateAwaitPollInterval is how often Start checks for the
	// certificate while it waits.
	certificateAwaitPollInterval = 250 * time.Millisecond

	// certificateDomainRetry is the retry after control has not yet listed the
	// service name among the node's certificate domains, which it does shortly
	// after approval.
	certificateDomainRetry = 15 * time.Second

	// certificateRetryMin and certificateRetryMax bound the backoff after any
	// other failure. The maximum stays under tsnet's hourly refresh so the
	// node, not that refresh, is what notices a recovered issuer.
	certificateRetryMin = 30 * time.Second
	certificateRetryMax = 30 * time.Minute
)

// acmeRetryAfterPattern finds the time Let's Encrypt names in a rate-limit
// refusal, such as "retry after 2026-11-12 22:53:32 UTC".
var acmeRetryAfterPattern = regexp.MustCompile(`retry after (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`)

// certificateLocker is implemented by state stores that share a certificate
// across a connection's slots, so the slots can request it one at a time.
type certificateLocker interface {
	LockCertificate(ctx context.Context) (func(), error)
}

// certificateFetcher obtains the certificate for the node's service name.
type certificateFetcher func(ctx context.Context) error

// certificateTracker requests a node's service certificate in the background
// and remembers whether it is held. Once obtained it stays obtained: tsnet
// renews it on its own, from the account that issued it.
type certificateTracker struct {
	domain string
	fetch  certificateFetcher
	logger *slog.Logger
	now    func() time.Time

	// stop is closed by Close, which cancels any request in flight and
	// starts no more.
	stop     chan struct{}
	stopOnce sync.Once

	// attempts tracks the request goroutine so Close can wait for it.
	attempts sync.WaitGroup

	// mu guards every field below.
	mu sync.Mutex

	// ready is true once a certificate is held.
	ready bool

	// attempting is true while a request is in flight; at most one is.
	attempting bool

	// attempted is true once any request has finished.
	attempted bool

	// lastErr is why the latest request failed.
	lastErr error

	// retryAt is when the next request may start.
	retryAt time.Time

	// failures counts consecutive failed requests that back off: refusals
	// naming their own retry time do not count.
	failures int
}

func newCertificateTracker(domain string, fetch certificateFetcher, logger *slog.Logger, now func() time.Time) *certificateTracker {
	return &certificateTracker{
		domain: domain, fetch: fetch, logger: logger, now: now,
		stop: make(chan struct{}), stopOnce: sync.Once{}, attempts: sync.WaitGroup{},
		mu: sync.Mutex{}, ready: false, attempting: false, attempted: false,
		lastErr: nil, retryAt: time.Time{}, failures: 0,
	}
}

// Poll reports whether the certificate is held and, when it is not, why. It
// never blocks: when no request is in flight and the retry time has passed,
// it starts one in the background.
func (t *certificateTracker) Poll() (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ready {
		return true, ""
	}
	if !t.attempting && !t.now().Before(t.retryAt) && !t.stopped() {
		t.attempting = true
		t.attempts.Go(t.request)
	}
	return false, t.reasonLocked()
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

// nextRequestAfter reports whether a request has settled without a
// certificate and the next one is not due until after deadline.
func (t *certificateTracker) nextRequestAfter(deadline time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.attempted && !t.attempting && t.retryAt.After(deadline)
}

// Close stops any request in flight and waits for it.
func (t *certificateTracker) Close() {
	t.stopOnce.Do(func() { close(t.stop) })
	t.attempts.Wait()
}

func (t *certificateTracker) stopped() bool {
	select {
	case <-t.stop:
		return true
	default:
		return false
	}
}

func (t *certificateTracker) request() {
	ctx, cancel := context.WithTimeout(context.Background(), certificateFetchTimeout)
	defer cancel()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-t.stop:
			cancel()
		case <-stopWatch:
		}
	}()
	err := t.fetch(ctx)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.attempting = false
	t.attempted = true
	if err == nil {
		t.ready, t.lastErr, t.failures = true, nil, 0
		t.logger.InfoContext(ctx, "obtained tailnet service certificate", attr.SlogHostName(t.domain))
		return
	}
	if t.stopped() {
		return
	}

	now := t.now()
	if !isCertificateRateLimited(err) && !isCertificateDomainPending(err) {
		t.failures++
	}
	t.lastErr = err
	t.retryAt = nextCertificateRetry(now, err, t.failures)
	t.logger.WarnContext(ctx, "could not obtain tailnet service certificate",
		attr.SlogHostName(t.domain), attr.SlogError(err),
		attr.SlogRetryAttempt(t.failures), attr.SlogRetryWait(t.retryAt.Sub(now)))
}

func (t *certificateTracker) reasonLocked() string {
	if t.lastErr == nil {
		return "obtaining a TLS certificate for " + t.domain
	}
	return certificateFailureMessage(t.domain, t.lastErr, t.retryAt)
}

// isCertificateRateLimited reports whether Let's Encrypt refused the request
// under one of its rate limits.
func isCertificateRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "rateLimited")
}

// isCertificateDomainPending reports whether control has not yet listed the
// service name among the node's certificate domains, as just after approval.
func isCertificateDomainPending(err error) bool {
	return err != nil && strings.Contains(err.Error(), "invalid domain")
}

// nextCertificateRetry is when to request again after the failures-th failure
// in a row. A rate-limit refusal names its own retry time; control not yet
// listing the service name clears within seconds; anything else backs off.
func nextCertificateRetry(now time.Time, err error, failures int) time.Time {
	if isCertificateRateLimited(err) {
		if match := acmeRetryAfterPattern.FindStringSubmatch(err.Error()); match != nil {
			if at, parseErr := time.ParseInLocation(time.DateTime, match[1], time.UTC); parseErr == nil && at.After(now) {
				return at
			}
		}
		return now.Add(certificateRetryMax)
	}
	if isCertificateDomainPending(err) {
		return now.Add(certificateDomainRetry)
	}

	backoff := certificateRetryMin
	for i := 1; i < max(failures, 1) && backoff < certificateRetryMax; i++ {
		backoff *= 2
	}
	return now.Add(min(backoff, certificateRetryMax))
}

// certificateFailureMessage is the slot's last_error while it waits for a
// certificate.
func certificateFailureMessage(domain string, err error, retryAt time.Time) string {
	retry := retryAt.UTC().Format("2006-01-02 15:04 UTC")
	if isCertificateRateLimited(err) {
		return fmt.Sprintf("Let's Encrypt is rate limiting new certificates for this tailnet; retrying after %s", retry)
	}
	return fmt.Sprintf("could not obtain a TLS certificate for %s: %v; retrying after %s", domain, err, retry)
}
