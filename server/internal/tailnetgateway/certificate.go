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

// certPhase is where a node is with its certificate. Whether a held
// certificate has expired is not a phase: it follows from the clock, and
// certStatus works it out.
type certPhase int

const (
	// phaseObtaining: no certificate is held and no request has failed.
	phaseObtaining certPhase = iota
	// phaseRetrying: no certificate is held and the last request failed.
	phaseRetrying
	// phaseServing: a certificate is held and nothing is failing.
	phaseServing
	// phaseRenewFailing: a certificate is held and the last renewal failed;
	// the node keeps serving it.
	phaseRenewFailing
	// phaseStopped: the node is closing and starts no more requests.
	phaseStopped
)

// certState is a node's certificate state. Every transition goes through
// apply, and every choice of what to do next through decide, both pure.
type certState struct {
	phase certPhase

	// cert is the certificate served, nil in phaseObtaining and
	// phaseRetrying.
	cert *tls.Certificate

	// err is why the last request failed, in phaseRetrying and
	// phaseRenewFailing.
	err error

	// failures counts consecutive failures that back off: refusals naming
	// their own retry time do not count.
	failures int

	// retryAt holds every request back after a failure; zero otherwise.
	retryAt time.Time

	// While a certificate is held: refreshAt is when to re-read the store,
	// windowCheckAt when to ask for the renewal window, renewAt when to
	// renew. A zero windowCheckAt means at once.
	refreshAt     time.Time
	windowCheckAt time.Time
	renewAt       time.Time
}

// certAction is the request step a node takes next.
type certAction int

const (
	actionNone certAction = iota
	// actionObtain gets a certificate while none is held.
	actionObtain
	// actionCheckStore re-reads the store for another slot's renewal.
	actionCheckStore
	// actionFetchWindow asks for the held certificate's renewal window.
	actionFetchWindow
	// actionRenew renews the held certificate under the lease.
	actionRenew
)

// certOutcome is the result of running one action.
type certOutcome struct {
	action certAction

	// cert is the certificate obtained, found in the store (nil for none)
	// or renewed into.
	cert *tls.Certificate

	// renewAt and checkAgain are the window's answer, for actionFetchWindow.
	renewAt, checkAgain time.Time

	err error
}

// errRenewalUnchanged is a renewal that returned the certificate it was
// asked to replace.
var errRenewalUnchanged = errors.New("the renewal returned the certificate it was asked to replace")

// decide is the step a node in st takes at now, or actionNone.
func decide(st certState, now time.Time) certAction {
	switch {
	case st.phase == phaseStopped, now.Before(st.retryAt):
		return actionNone
	case st.cert == nil:
		return actionObtain
	case !now.Before(st.refreshAt):
		return actionCheckStore
	case !now.Before(st.windowCheckAt):
		return actionFetchWindow
	case !now.Before(st.renewAt):
		return actionRenew
	default:
		return actionNone
	}
}

// apply is the state after outcome at now. refreshIn is how long until the
// next store read, passed in so the jitter stays out of apply.
func apply(st certState, o certOutcome, now time.Time, refreshIn time.Duration) certState {
	if st.phase == phaseStopped {
		return st
	}
	switch o.action {
	case actionObtain:
		if o.err != nil {
			return failed(st, o.err, now)
		}
		return serving(st, o.cert, now, refreshIn)
	case actionCheckStore:
		if o.err == nil && replaces(o.cert, st.cert) {
			return serving(st, o.cert, now, refreshIn)
		}
		st.refreshAt = now.Add(refreshIn)
		return st
	case actionFetchWindow:
		if o.err != nil {
			st.renewAt, st.windowCheckAt = fallbackRenewAt(st.cert.Leaf), now.Add(renewalCheckMin)
			return st
		}
		st.renewAt, st.windowCheckAt = o.renewAt, o.checkAgain
		return st
	case actionRenew:
		if o.err == nil && o.cert.Leaf.Equal(st.cert.Leaf) {
			o.err = errRenewalUnchanged
		}
		if o.err != nil {
			return failed(st, o.err, now)
		}
		return serving(st, o.cert, now, refreshIn)
	case actionNone:
	}
	return st
}

// serving holds cert with nothing failing; its window is asked for at once.
func serving(st certState, cert *tls.Certificate, now time.Time, refreshIn time.Duration) certState {
	return certState{
		phase: phaseServing, cert: cert, err: nil, failures: 0, retryAt: time.Time{},
		refreshAt: now.Add(refreshIn), windowCheckAt: time.Time{}, renewAt: time.Time{},
	}
}

// failed records a failed obtain or renewal and when to retry.
func failed(st certState, err error, now time.Time) certState {
	if !isCertificateRateLimited(err) && !isCertificateDomainPending(err) && !errors.Is(err, errCertificateIssuing) {
		st.failures++
	}
	st.err = err
	st.retryAt = nextCertificateRetry(now, err, st.failures)
	st.phase = phaseRetrying
	if st.cert != nil {
		st.phase = phaseRenewFailing
	}
	return st
}

// certStatus reports whether st holds a certificate valid at now and, when it
// does not, why.
func certStatus(st certState, domain string, now time.Time) (bool, string) {
	switch st.phase {
	case phaseObtaining:
		return false, "obtaining a TLS certificate for " + domain
	case phaseRetrying:
		return false, certificateFailureMessage(domain, st.err, st.retryAt)
	case phaseServing, phaseRenewFailing:
		if now.Before(st.cert.Leaf.NotAfter) {
			return true, ""
		}
		if st.phase == phaseServing {
			return false, "the TLS certificate for " + domain + " has expired"
		}
		return false, fmt.Sprintf("the TLS certificate for %s has expired; %v; retrying after %s",
			domain, st.err, st.retryAt.UTC().Format("2006-01-02 15:04 UTC"))
	case phaseStopped:
		return false, "the gateway node is stopping"
	}
	return false, ""
}

// replaces reports whether stored is a later certificate than held.
func replaces(stored, held *tls.Certificate) bool {
	return stored != nil && !stored.Leaf.Equal(held.Leaf) && stored.Leaf.NotAfter.After(held.Leaf.NotAfter)
}

// maxStepsPerRequest bounds one request's steps: obtain or re-read the
// store, read the window, renew, read the new certificate's window.
const maxStepsPerRequest = 4

// certificateTracker requests a node's service certificate in the background,
// holds it for the node's TLS handshakes, and renews it when its renewal
// window says so. It runs the actions decide picks and feeds their outcomes
// to apply; it decides nothing itself.
type certificateTracker struct {
	domain  string
	source  certificateSource
	windows renewalWindows
	logger  *slog.Logger
	now     func() time.Time

	// requests is a pod-wide semaphore bounding concurrent requests; nil
	// means unbounded.
	requests chan struct{}

	// events, when set, is told the outcome of each obtain, renewal and
	// window request.
	events func(ctx context.Context, operation CertificateOperation, result CertificateResult)

	// stop is closed by Close, which cancels any request in flight.
	stop     chan struct{}
	stopOnce sync.Once

	// attempts tracks the request goroutine so Close can wait for it.
	attempts sync.WaitGroup

	// mu guards state and attempting.
	mu    sync.Mutex
	state certState

	// attempting is true while a request is in flight; at most one is. It
	// applies on top of any phase.
	attempting bool
}

func newCertificateTracker(domain string, source certificateSource, windows renewalWindows, requests chan struct{}, logger *slog.Logger, now func() time.Time) *certificateTracker {
	return &certificateTracker{
		domain: domain, source: source, windows: windows, logger: logger, now: now, requests: requests, events: nil,
		stop: make(chan struct{}), stopOnce: sync.Once{}, attempts: sync.WaitGroup{},
		mu: sync.Mutex{}, attempting: false,
		state: certState{
			phase: phaseObtaining, cert: nil, err: nil, failures: 0, retryAt: time.Time{},
			refreshAt: time.Time{}, windowCheckAt: time.Time{}, renewAt: time.Time{},
		},
	}
}

// Poll reports whether a valid certificate is held and, when it is not, why.
// It never blocks: when decide has a step due and no request is in flight,
// it starts one in the background.
func (t *certificateTracker) Poll() (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	if !t.attempting && decide(t.state, now) != actionNone && t.acquire() {
		t.attempting = true
		t.attempts.Go(func() {
			defer t.release()
			t.request()
		})
	}
	return certStatus(t.state, t.domain, now)
}

// Certificate returns the certificate for a TLS handshake, failing the
// handshake until one is obtained. It never asks for one: only the tracker's
// own requests, which take the issuance lease, do.
func (t *certificateTracker) Certificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state.cert == nil {
		return nil, errCertificateNotObtained
	}
	return t.state.cert, nil
}

// ExpiresAt is when the certificate served expires, zero while none is held.
func (t *certificateTracker) ExpiresAt() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state.cert == nil {
		return time.Time{}
	}
	return t.state.cert.Leaf.NotAfter
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
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.attempting && t.state.phase == phaseRetrying && t.state.retryAt.After(deadline)
}

// Close stops any request in flight and waits for it.
func (t *certificateTracker) Close() {
	t.mu.Lock()
	t.state.phase = phaseStopped
	t.mu.Unlock()
	t.stopOnce.Do(func() { close(t.stop) })
	t.attempts.Wait()
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

// request runs the steps decide picks until none is due, applying each
// outcome before deciding again.
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
	defer func() {
		t.mu.Lock()
		t.attempting = false
		t.mu.Unlock()
	}()

	for range maxStepsPerRequest {
		t.mu.Lock()
		st, now := t.state, t.now()
		t.mu.Unlock()
		action := decide(st, now)
		if action == actionNone {
			return
		}
		outcome := t.run(ctx, action, st, now)

		t.mu.Lock()
		before := t.state
		t.state = apply(t.state, outcome, t.now(), jittered(certificateRefreshInterval))
		after := t.state
		t.mu.Unlock()
		t.log(ctx, before, after, outcome)
	}
}

// run performs action; it is the only place the tracker does I/O.
func (t *certificateTracker) run(ctx context.Context, action certAction, st certState, now time.Time) certOutcome {
	outcome := certOutcome{action: action, cert: nil, renewAt: time.Time{}, checkAgain: time.Time{}, err: nil}
	switch action {
	case actionObtain:
		outcome.cert, outcome.err = t.source.Obtain(ctx, now)
	case actionCheckStore:
		outcome.cert, outcome.err = t.source.Stored(ctx)
	case actionFetchWindow:
		outcome.renewAt, outcome.checkAgain, outcome.err = t.windows.RenewAt(ctx, st.cert.Leaf, now)
	case actionRenew:
		outcome.cert, outcome.err = t.source.Renew(ctx, st.cert, now)
	case actionNone:
	}
	return outcome
}

// certificateOperation names a step counted in metrics; re-reading the store
// is not a certificate request.
func certificateOperation(action certAction) (CertificateOperation, bool) {
	switch action {
	case actionObtain:
		return CertificateOperationObtain, true
	case actionRenew:
		return CertificateOperationRenew, true
	case actionFetchWindow:
		return CertificateOperationWindow, true
	case actionNone, actionCheckStore:
	}
	return "", false
}

// log reports what a step changed.
func (t *certificateTracker) log(ctx context.Context, before, after certState, o certOutcome) {
	if after.phase == phaseStopped {
		return
	}
	if operation, ok := certificateOperation(o.action); ok && t.events != nil {
		// A renewal returning the certificate it replaced fails in apply.
		err := o.err
		if o.action == actionRenew && err == nil && after.phase == phaseRenewFailing {
			err = after.err
		}
		t.events(ctx, operation, certificateResult(err))
	}
	if after.cert != nil && (before.cert == nil || !before.cert.Leaf.Equal(after.cert.Leaf)) {
		t.logger.InfoContext(ctx, "obtained tailnet service certificate", attr.SlogHostName(t.domain), attr.SlogTailnetGatewayCertificateExpiresAt(after.cert.Leaf.NotAfter))
	}
	if o.err == nil {
		return
	}
	switch o.action {
	case actionCheckStore:
		t.logger.WarnContext(ctx, "could not read the stored tailnet service certificate", attr.SlogHostName(t.domain), attr.SlogError(o.err))
	case actionFetchWindow:
		t.logger.WarnContext(ctx, "could not read the certificate renewal window; renewing two thirds through its lifetime",
			attr.SlogHostName(t.domain), attr.SlogError(o.err))
	case actionRenew:
		t.logger.WarnContext(ctx, "could not renew tailnet service certificate",
			attr.SlogHostName(t.domain), attr.SlogError(after.err), attr.SlogTailnetGatewayCertificateExpiresAt(after.cert.Leaf.NotAfter),
			attr.SlogRetryAttempt(after.failures), attr.SlogRetryWait(after.retryAt.Sub(t.now())))
	case actionObtain:
		t.logger.WarnContext(ctx, "could not obtain tailnet service certificate",
			attr.SlogHostName(t.domain), attr.SlogError(after.err),
			attr.SlogRetryAttempt(after.failures), attr.SlogRetryWait(after.retryAt.Sub(t.now())))
	case actionNone:
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
