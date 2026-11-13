package tailnetgateway

import (
	"context"
	"crypto/tls"
	"errors"
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
// fails. Let's Encrypt allows a tailnet 50 new certificates a week, shared
// with everything else on that tailnet, so a refusal is ordinary. The node
// therefore requests the certificate itself as soon as it is approved, keeps
// retrying, and reports a host without one as awaiting a certificate rather
// than online.
//
// The node, not tsnet, terminates TLS, and serves only the certificate this
// tracker obtained. Left to terminate TLS, tsnet orders a certificate for any
// client that connects before one is stored, without the issuance lease, so
// every slot a client reached spent one from the tailnet's allowance.
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

	// certificateIssuingRetry is the retry while another slot holds the
	// issuance lease; first issuance usually takes a few seconds.
	certificateIssuingRetry = 5 * time.Second

	// certificateRefreshInterval is how often a node holding the certificate
	// asks tsnet for it again, which picks up a renewal, whichever slot made
	// it, and lets tsnet start one when it is due. It matches the hourly
	// refresh tsnet runs when it terminates TLS itself.
	certificateRefreshInterval = time.Hour

	// certificateRetryMin and certificateRetryMax bound the backoff after any
	// other failure. The maximum stays under an hour, so a slot notices a
	// recovered issuer within the hour.
	certificateRetryMin = 30 * time.Second
	certificateRetryMax = 30 * time.Minute
)

// acmeRetryAfterPattern finds the time Let's Encrypt names in a rate-limit
// refusal, such as "retry after 2026-11-12 22:53:32 UTC".
var acmeRetryAfterPattern = regexp.MustCompile(`retry after (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`)

// certificateIssuer is implemented by state stores that share a certificate
// across a connection's slots: a slot reads the stored certificate, and
// claims the issuance lease before requesting a missing one.
type certificateIssuer interface {
	ReadTLSCertAndKey(domain string) ([]byte, []byte, error)
	ClaimCertificateIssuance(ctx context.Context) (func(), bool, error)
}

// errCertificateIssuing reports that another slot holds the connection's
// issuance lease. The slot retries shortly and then finds the certificate
// stored; it is not a failure.
var errCertificateIssuing = errors.New("another gateway slot is obtaining the certificate")

// errCertificateNotObtained fails a TLS handshake that arrives before the node
// holds its certificate.
var errCertificateNotObtained = errors.New("the gateway has not obtained its TLS certificate yet")

// certificateFetcher obtains the certificate for the node's service name.
type certificateFetcher func(ctx context.Context) (*tls.Certificate, error)

// certificateTracker requests a node's service certificate in the background,
// holds it for the node's TLS handshakes, and asks again every
// certificateRefreshInterval so a renewal reaches the node.
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

	// cert is the certificate the node serves, nil until one is obtained.
	cert *tls.Certificate

	// attempting is true while a request is in flight; at most one is.
	attempting bool

	// attempted is true once any request has finished.
	attempted bool

	// lastErr is why the latest request failed.
	lastErr error

	// retryAt is when the next request may start: a retry while no
	// certificate is held, a refresh once one is.
	retryAt time.Time

	// failures counts consecutive failed requests that back off: refusals
	// naming their own retry time do not count.
	failures int
}

func newCertificateTracker(domain string, fetch certificateFetcher, logger *slog.Logger, now func() time.Time) *certificateTracker {
	return &certificateTracker{
		domain: domain, fetch: fetch, logger: logger, now: now,
		stop: make(chan struct{}), stopOnce: sync.Once{}, attempts: sync.WaitGroup{},
		mu: sync.Mutex{}, cert: nil, attempting: false, attempted: false,
		lastErr: nil, retryAt: time.Time{}, failures: 0,
	}
}

// Poll reports whether a valid certificate is held and, when it is not, why.
// It never blocks: when no request is in flight and the retry or refresh time
// has passed, it starts one in the background.
func (t *certificateTracker) Poll() (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	if !t.attempting && !now.Before(t.retryAt) && !t.stopped() {
		t.attempting = true
		t.attempts.Go(t.request)
	}
	if t.cert != nil && now.Before(t.cert.Leaf.NotAfter) {
		return true, ""
	}
	return false, t.reasonLocked()
}

// Certificate returns the certificate for a TLS handshake, failing the
// handshake until one is obtained. It never asks for one: only the tracker's
// own requests, which take the issuance lease, do.
func (t *certificateTracker) Certificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cert == nil {
		return nil, errCertificateNotObtained
	}
	return t.cert, nil
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
	cert, err := t.fetch(ctx)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.attempting = false
	t.attempted = true
	now := t.now()
	if err == nil {
		if t.cert == nil || !t.cert.Leaf.Equal(cert.Leaf) {
			t.logger.InfoContext(ctx, "obtained tailnet service certificate", attr.SlogHostName(t.domain))
		}
		t.cert, t.lastErr, t.failures = cert, nil, 0
		t.retryAt = now.Add(certificateRefreshInterval)
		return
	}
	if t.stopped() {
		return
	}

	if !isCertificateRateLimited(err) && !isCertificateDomainPending(err) && !errors.Is(err, errCertificateIssuing) {
		t.failures++
	}
	t.lastErr = err
	t.retryAt = nextCertificateRetry(now, err, t.failures)
	t.logger.WarnContext(ctx, "could not obtain tailnet service certificate",
		attr.SlogHostName(t.domain), attr.SlogError(err),
		attr.SlogRetryAttempt(t.failures), attr.SlogRetryWait(t.retryAt.Sub(now)))
}

func (t *certificateTracker) reasonLocked() string {
	if t.cert != nil && t.lastErr == nil {
		return "the TLS certificate for " + t.domain + " has expired"
	}
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
	if errors.Is(err, errCertificateIssuing) {
		return now.Add(certificateIssuingRetry)
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
	if errors.Is(err, errCertificateIssuing) {
		return "another gateway slot is obtaining the TLS certificate for " + domain
	}
	retry := retryAt.UTC().Format("2006-01-02 15:04 UTC")
	if isCertificateRateLimited(err) {
		return fmt.Sprintf("Let's Encrypt is rate limiting new certificates for this tailnet; retrying after %s", retry)
	}
	return fmt.Sprintf("could not obtain a TLS certificate for %s: %v; retrying after %s", domain, err, retry)
}
