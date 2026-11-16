package tailnetgateway

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/speakeasy-api/sc"
)

// certificateChart is a node's certificate lifecycle. Certificate.tla in this
// directory models it together with the other slots of the connection, the
// shared store, the issuance lease and Let's Encrypt; when the chart changes,
// change the model and run TLC again (see CERTIFICATES.md).
//
// The chart declares its four requests by name only. Each tracker binds them
// to its own certificate source and renewal windows, so the chart is built
// once and explored without either.
//
// Close moves every state to stopped.
type certState string

const (
	// certWaiting: nothing has been asked yet; the node is not approved to
	// host the service, so a request would only be refused.
	certWaiting certState = "waiting"
	// certObtaining: no certificate is held and a request is running.
	certObtaining certState = "obtaining"
	// certRetrying: no certificate is held and the last request failed.
	certRetrying certState = "retrying"
	// certServing: a certificate is held and nothing is running. A failed
	// renewal leaves err set here until a later one succeeds.
	certServing certState = "serving"
	// certCheckingStore: re-reading the store for another slot's renewal.
	certCheckingStore certState = "checking_store"
	// certFetchingWindow: asking Let's Encrypt for the renewal window.
	certFetchingWindow certState = "fetching_window"
	// certRenewing: renewing the held certificate under the lease.
	certRenewing certState = "renewing"
	// certRenewBackoff: the last renewal failed; the node keeps serving the
	// held certificate until the retry.
	certRenewBackoff certState = "renew_backoff"
	// certStopped: the node is closing and starts no more requests.
	certStopped certState = "stopped"
)

// The services the chart runs. Each tracker binds them in bindCertificates.
const (
	serviceObtain      = "obtain"
	serviceCheckStore  = "checkStore"
	serviceFetchWindow = "fetchWindow"
	serviceRenew       = "renew"
)

// certPoll is the heartbeat's poll: it starts the first request, and like any
// event fires the timers that are due first.
type certPoll struct{}

// certStop closes the chart.
type certStop struct{}

// certContext is a node's certificate state.
type certContext struct {
	// cert is the certificate served, nil until one is obtained.
	cert *tls.Certificate

	// err is why the last request failed: in certRetrying, and while a held
	// certificate's last renewal failed.
	err error

	// failures counts consecutive failures that back off: refusals naming
	// their own retry time do not count.
	failures int

	// retryAt is when certRetrying and certRenewBackoff retry.
	retryAt time.Time

	// While a certificate is held: refreshAt is when to re-read the store,
	// windowCheckAt when to ask for the renewal window, renewAt when to
	// renew. A zero windowCheckAt means at once, and a zero renewAt that the
	// window has not been read yet.
	refreshAt     time.Time
	windowCheckAt time.Time
	renewAt       time.Time

	// refreshEvery is how long after each store read the next one is due:
	// certificateRefreshInterval give or take a tenth, drawn once per node
	// (see refreshInterval) so the nodes a pod started together do not all
	// read in the same poll.
	refreshEvery time.Duration
}

// refreshInterval draws a node's refreshEvery.
func refreshInterval() time.Duration {
	d := certificateRefreshInterval
	return d - d/10 + rand.N(d/5+1) //nolint:gosec // spreading load, not security
}

// renewalWindow is a window request's answer: when to renew and when to ask
// again.
type renewalWindow struct {
	renewAt, checkAgain time.Time
}

// certificateChart is built once; its services are bound per tracker.
var certificateChart = mustCertificateChart()

func mustCertificateChart() *sc.Machine[certState, certContext] {
	m, err := sc.New("tailnet-gateway-certificate", defineCertificateChart)
	if err != nil {
		panic(fmt.Sprintf("build certificate chart: %v", err))
	}
	return m
}

func defineCertificateChart(b *sc.Builder[certState, certContext]) {
	b.Initial(certWaiting)
	b.On[certStop]().To(certStopped)

	b.State(certWaiting).On[certPoll]().To(certObtaining)

	obtaining := b.State(certObtaining)
	obtain := obtaining.Requires[*tls.Certificate](serviceObtain)
	obtain.OnDone().AssignAt("serve", serve).To(certServing)
	obtain.OnError().AssignAt("recordFailure", recordFailure).To(certRetrying)

	b.State(certRetrying).Deadline("retryAt", retryAt).To(certObtaining)

	// When several are due at once the earliest fires first, and a tie
	// fires in declaration order. A renewal due with a refresh is safe
	// either way: it re-reads the store under the lease.
	serving := b.State(certServing)
	serving.Deadline("refreshAt", func(c certContext) time.Time { return c.refreshAt }).To(certCheckingStore)
	serving.Deadline("windowCheckAt", func(c certContext) time.Time { return c.windowCheckAt }).To(certFetchingWindow)
	serving.Deadline("renewAt", func(c certContext) time.Time { return c.renewAt }).
		When("windowRead", func(c certContext, _ struct{}) bool { return !c.renewAt.IsZero() }).
		To(certRenewing)

	checking := b.State(certCheckingStore)
	check := checking.Requires[*tls.Certificate](serviceCheckStore)
	check.OnDone().
		When("replacesHeld", func(c certContext, d sc.Done[*tls.Certificate]) bool { return replaces(d.Result, c.cert) }).
		AssignAt("serve", serve).
		To(certServing)
	// Nothing newer, or a store that cannot be read: logged, not a failure.
	check.OnDone().AssignAt("scheduleRefresh", scheduleRefresh[sc.Done[*tls.Certificate]]).To(certServing)
	check.OnError().AssignAt("scheduleRefresh", scheduleRefresh[sc.Failed]).To(certServing)

	fetching := b.State(certFetchingWindow)
	window := fetching.Requires[renewalWindow](serviceFetchWindow)
	window.OnDone().Assign("followWindow", func(c certContext, d sc.Done[renewalWindow]) certContext {
		c.renewAt, c.windowCheckAt = d.Result.renewAt, d.Result.checkAgain
		return c
	}).To(certServing)
	// An unreadable window is not a failure: renew two thirds through the
	// lifetime and ask again soon.
	window.OnError().AssignAt("fallBack", func(c certContext, _ sc.Failed, now time.Time) certContext {
		c.renewAt, c.windowCheckAt = fallbackRenewAt(c.cert.Leaf), now.Add(renewalCheckMin)
		return c
	}).To(certServing)

	renewing := b.State(certRenewing)
	renew := renewing.Requires[*tls.Certificate](serviceRenew)
	renew.OnDone().
		When("renewed", func(c certContext, d sc.Done[*tls.Certificate]) bool { return replaces(d.Result, c.cert) }).
		AssignAt("serve", serve).
		To(certServing)
	renew.OnDone().AssignAt("recordUnchanged", func(c certContext, _ sc.Done[*tls.Certificate], now time.Time) certContext {
		return failed(c, errRenewalUnchanged, now)
	}).To(certRenewBackoff)
	renew.OnError().AssignAt("recordFailure", recordFailure).To(certRenewBackoff)

	b.State(certRenewBackoff).Deadline("retryAt", retryAt).To(certServing)

	b.Final(certStopped)
}

func retryAt(c certContext) time.Time { return c.retryAt }

// serve holds the certificate a request returned with nothing failing; its
// window is asked for at once.
func serve(c certContext, d sc.Done[*tls.Certificate], now time.Time) certContext {
	return certContext{
		cert: d.Result, err: nil, failures: 0, retryAt: time.Time{},
		refreshAt: now.Add(c.refreshEvery), windowCheckAt: time.Time{}, renewAt: time.Time{},
		refreshEvery: c.refreshEvery,
	}
}

func scheduleRefresh[Ev any](c certContext, _ Ev, now time.Time) certContext {
	c.refreshAt = now.Add(c.refreshEvery)
	return c
}

func recordFailure(c certContext, f sc.Failed, now time.Time) certContext {
	return failed(c, f.Err, now)
}

// failed records a failed obtain or renewal and when to retry.
func failed(c certContext, err error, now time.Time) certContext {
	if backsOff(err) {
		c.failures++
	}
	c.err = err
	c.retryAt = nextCertificateRetry(now, err, c.failures)
	return c
}

// bindCertificates gives the chart's services their implementations.
//
// Each request bounds itself in wall time. A Within budget on the chart's
// states would not do: the chart's timers fire only when the node polls, and
// a state entered by a timer that fell due before the poll counts its budget
// from the due time, so a request started late would find it spent.
func bindCertificates(source certificateSource, windows renewalWindows, now func() time.Time) (*sc.Machine[certState, certContext], error) {
	m, err := certificateChart.Bind(func(b *sc.Binder[certState, certContext]) {
		b.Service(serviceObtain, func(ctx context.Context, _ *sc.Run, _ certContext) (*tls.Certificate, error) {
			ctx, cancel := context.WithTimeout(ctx, certificateFetchTimeout)
			defer cancel()
			return source.Obtain(ctx, now())
		})
		b.Service(serviceCheckStore, func(ctx context.Context, _ *sc.Run, _ certContext) (*tls.Certificate, error) {
			ctx, cancel := context.WithTimeout(ctx, certificateFetchTimeout)
			defer cancel()
			return source.Stored(ctx)
		})
		b.Service(serviceFetchWindow, func(ctx context.Context, _ *sc.Run, c certContext) (renewalWindow, error) {
			ctx, cancel := context.WithTimeout(ctx, renewalFetchTimeout)
			defer cancel()
			renewAt, checkAgain, err := windows.RenewAt(ctx, c.cert.Leaf, now())
			if err != nil {
				return renewalWindow{}, fmt.Errorf("read certificate renewal window: %w", err)
			}
			return renewalWindow{renewAt: renewAt, checkAgain: checkAgain}, nil
		})
		b.Service(serviceRenew, func(ctx context.Context, _ *sc.Run, c certContext) (*tls.Certificate, error) {
			ctx, cancel := context.WithTimeout(ctx, certificateFetchTimeout)
			defer cancel()
			return source.Renew(ctx, c.cert, now())
		})
	})
	if err != nil {
		return nil, fmt.Errorf("bind certificate chart: %w", err)
	}
	return m, nil
}
