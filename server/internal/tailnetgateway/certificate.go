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

	"github.com/speakeasy-api/sc"
	"golang.org/x/sync/semaphore"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// CERTIFICATES.md in this directory explains the Let's Encrypt limits this
// file works around and how a connection's slots coordinate issuance and
// renewal; read it before changing when or how certificates are requested.
//
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

	// certificateRefreshInterval is how often, give or take a tenth, a node
	// holding the certificate re-reads the store, which picks up another
	// slot's renewal, and checks whether its own is due. It never asks tsnet
	// for the certificate then: tsnet would decide on a renewal itself.
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
// claims the issuance lease before requesting a missing one or renewing it.
type certificateIssuer interface {
	ReadTLSCertAndKey(domain string) ([]byte, []byte, error)
	ClaimCertificateIssuance(ctx context.Context) (func(), bool, error)
}

// certificateSource is where a node gets its certificate.
type certificateSource interface {
	// Stored returns the connection's stored certificate, or nil when none
	// is stored or the store does not share certificates.
	Stored(ctx context.Context) (*tls.Certificate, error)

	// Obtain returns a certificate for a node holding none: the stored one
	// when it is valid, otherwise a new one, ordered under the lease.
	Obtain(ctx context.Context, now time.Time) (*tls.Certificate, error)

	// Renew replaces held under the lease, or returns the certificate
	// another slot already renewed it into.
	Renew(ctx context.Context, held *tls.Certificate, now time.Time) (*tls.Certificate, error)
}

// errCertificateIssuing reports that another slot holds the connection's
// issuance lease. The slot retries shortly and then finds the certificate
// stored; it is not a failure.
var errCertificateIssuing = errors.New("another gateway slot is obtaining the certificate")

// errCertificateNotObtained fails a TLS handshake that arrives before the node
// holds its certificate.
var errCertificateNotObtained = errors.New("the gateway has not obtained its TLS certificate yet")

// errRenewalUnchanged is a renewal that returned the certificate it was
// asked to replace, or one that expires no later.
var errRenewalUnchanged = errors.New("the renewal returned no certificate later than the one it was asked to replace")

// backsOff reports whether a failure counts toward the backoff. A refusal
// naming its own retry time, control not yet listing the service name, and
// another slot holding the lease do not.
func backsOff(err error) bool {
	return !isCertificateRateLimited(err) && !isCertificateDomainPending(err) && !errors.Is(err, errCertificateIssuing)
}

// certStatus reports whether snap holds a certificate valid at now and, when
// it does not, why. Whether a held certificate has expired is not a state: it
// follows from the clock.
func certStatus(snap sc.Snapshot[certState, certContext], domain string, now time.Time) (bool, string) {
	c := snap.Context
	switch {
	case snap.In(certStopped):
		return false, "the gateway node is stopping"
	case c.cert == nil && c.err == nil:
		return false, "obtaining a TLS certificate for " + domain
	case c.cert == nil:
		return false, certificateFailureMessage(domain, c.err, c.retryAt)
	case now.Before(c.cert.Leaf.NotAfter):
		return true, ""
	case c.err == nil:
		return false, "the TLS certificate for " + domain + " has expired"
	default:
		return false, fmt.Sprintf("the TLS certificate for %s has expired; %v; retrying after %s",
			domain, c.err, c.retryAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
}

// replaces reports whether stored is a later certificate than held.
func replaces(stored, held *tls.Certificate) bool {
	return stored != nil && !stored.Leaf.Equal(held.Leaf) && stored.Leaf.NotAfter.After(held.Leaf.NotAfter)
}

// certificateTracker requests a node's service certificate in the background,
// holds it for the node's TLS handshakes, and renews it when its renewal
// window says so. certificateChart decides every step; the tracker only
// drives it from the node's heartbeat and reports what it did.
type certificateTracker struct {
	domain string
	logger *slog.Logger
	now    func() time.Time

	// events, when set, is told the outcome of each obtain, renewal and
	// window request.
	events func(ctx context.Context, operation CertificateOperation, result CertificateResult)

	// inst runs the chart. The chart decides only when the tracker polls,
	// so a node does no certificate work between heartbeats; its requests
	// run on goroutines of their own, one at a time since each has its own
	// state.
	inst *sc.Instance[certState, certContext]

	closeOnce sync.Once
}

// newCertificateTracker starts a node's tracker. requests is a pod-wide
// semaphore bounding concurrent requests, nil for unbounded; a request waits
// for its turn without holding up the node.
func newCertificateTracker(
	domain string,
	source certificateSource,
	windows renewalWindows,
	requests *semaphore.Weighted,
	events func(ctx context.Context, operation CertificateOperation, result CertificateResult),
	logger *slog.Logger,
	now func() time.Time,
) (*certificateTracker, error) {
	m, err := bindCertificates(source, windows, now)
	if err != nil {
		return nil, err
	}
	t := &certificateTracker{domain: domain, logger: logger, now: now, events: events, inst: nil, closeOnce: sync.Once{}}
	opts := []sc.RunOption{
		sc.WithClock(now),
		sc.WithHooks(&sc.Hooks[certState, certContext]{Transition: t.observe, Ignored: nil, EffectStarted: nil, EffectFinished: nil}),
	}
	if requests != nil {
		opts = append(opts, sc.LimitServices(requests))
	}
	start := certContext{
		cert: nil, err: nil, failures: 0, retryAt: time.Time{},
		refreshAt: time.Time{}, windowCheckAt: time.Time{}, renewAt: time.Time{},
		refreshEvery: refreshInterval(),
	}
	inst, err := m.Instance(context.Background(), start, opts...)
	if err != nil {
		return nil, fmt.Errorf("start certificate chart: %w", err)
	}
	t.inst = inst
	return t, nil
}

// Poll reports whether a valid certificate is held and, when it is not, why.
// It never waits for a request: when one is due it starts it in the
// background.
func (t *certificateTracker) Poll() (bool, string) {
	// The only error is a closed instance, whose snapshot reports stopped.
	_, _ = t.inst.Send(context.Background(), certPoll{})
	return certStatus(t.inst.Snapshot(), t.domain, t.now())
}

// Certificate returns the certificate for a TLS handshake, failing the
// handshake until one is obtained. It never asks for one: only the chart's
// own requests, which take the issuance lease, do.
func (t *certificateTracker) Certificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	if cert := t.inst.Snapshot().Context.cert; cert != nil {
		return cert, nil
	}
	return nil, errCertificateNotObtained
}

// ExpiresAt is when the certificate served expires, zero while none is held.
func (t *certificateTracker) ExpiresAt() time.Time {
	if cert := t.inst.Snapshot().Context.cert; cert != nil {
		return cert.Leaf.NotAfter
	}
	return time.Time{}
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
	snap := t.inst.Snapshot()
	return snap.In(certRetrying) && snap.Context.retryAt.After(deadline)
}

// busy reports whether a request is running or waiting for its turn.
func (t *certificateTracker) busy() bool {
	return len(t.inst.Snapshot().Services()) > 0
}

// Close stops any request in flight and waits for it.
func (t *certificateTracker) Close() {
	t.closeOnce.Do(func() {
		_, _ = t.inst.Send(context.Background(), certStop{})
		t.inst.Close()
	})
}

// certificateOperation names a request counted in metrics; re-reading the
// store is not a certificate request.
func certificateOperation(request certState) (CertificateOperation, bool) {
	switch request {
	case certObtaining:
		return CertificateOperationObtain, true
	case certRenewing:
		return CertificateOperationRenew, true
	case certFetchingWindow:
		return CertificateOperationWindow, true
	case certCheckingStore, certWaiting, certRetrying, certServing, certRenewBackoff, certStopped:
	}
	return "", false
}

// requestIn is the request state snap is in, if any.
func requestIn(snap sc.Snapshot[certState, certContext]) (certState, bool) {
	for _, s := range []certState{certObtaining, certCheckingStore, certFetchingWindow, certRenewing} {
		if snap.In(s) {
			return s, true
		}
	}
	return "", false
}

// observe counts and logs each request's outcome.
func (t *certificateTracker) observe(ctx context.Context, step sc.Step[certState, certContext]) {
	request, ok := requestIn(step.From)
	if !ok || step.To.In(certStopped) {
		return
	}
	var err error
	switch ev := step.Event.(type) {
	case sc.Failed:
		err = ev.Err
	case sc.Done[*tls.Certificate], sc.Done[renewalWindow]:
		if request == certRenewing {
			// A renewal returning the certificate it replaced fails in
			// the chart.
			err = step.To.Context.err
		}
	default:
		return
	}

	before, after := step.From.Context, step.To.Context
	if operation, ok := certificateOperation(request); ok && t.events != nil {
		t.events(ctx, operation, certificateResult(err))
	}
	if after.cert != nil && (before.cert == nil || !before.cert.Leaf.Equal(after.cert.Leaf)) {
		t.logger.InfoContext(ctx, "obtained tailnet service certificate", attr.SlogHostName(t.domain), attr.SlogTailnetGatewayCertificateExpiresAt(after.cert.Leaf.NotAfter))
	}
	if err == nil {
		return
	}
	if errors.Is(err, errCertificateIssuing) {
		// Waiting for another slot's lease is expected, and retried every
		// few seconds; it is not worth a warning each time.
		t.logger.DebugContext(ctx, "waiting for another gateway slot to obtain the tailnet service certificate", attr.SlogHostName(t.domain))
		return
	}
	switch request {
	case certCheckingStore:
		t.logger.WarnContext(ctx, "could not read the stored tailnet service certificate", attr.SlogHostName(t.domain), attr.SlogError(err))
	case certFetchingWindow:
		t.logger.WarnContext(ctx, "could not read the certificate renewal window; renewing two thirds through its lifetime",
			attr.SlogHostName(t.domain), attr.SlogError(err))
	case certRenewing:
		t.logger.WarnContext(ctx, "could not renew tailnet service certificate",
			attr.SlogHostName(t.domain), attr.SlogError(err), attr.SlogTailnetGatewayCertificateExpiresAt(after.cert.Leaf.NotAfter),
			attr.SlogRetryAttempt(after.failures), attr.SlogRetryWait(after.retryAt.Sub(step.At)))
	case certObtaining:
		t.logger.WarnContext(ctx, "could not obtain tailnet service certificate",
			attr.SlogHostName(t.domain), attr.SlogError(err),
			attr.SlogRetryAttempt(after.failures), attr.SlogRetryWait(after.retryAt.Sub(step.At)))
	case certWaiting, certRetrying, certServing, certRenewBackoff, certStopped:
	}
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
