// Package certificate ports the tailnet gateway's certificate tracker to the
// sc statechart package, to find where sc falls short of hand-written state
// machines. The tracker requests a node's service certificate, holds it for
// the node's TLS handshakes, re-reads the shared store for another slot's
// renewal, asks Let's Encrypt for the renewal window, and renews under the
// issuance lease.
//
// The original keeps a certState and two pure functions: decide picks the
// next step from the state and the clock, and apply records a step's outcome.
// Here decide survives unchanged as the guard of the poll that starts a
// request and of the eventless transitions between its steps, which read the
// clock the driver passes in. apply is split into one assignment per
// outcome. The original's request loop is the Requesting state: one request
// of up to maxStepsPerRequest steps under one shared timeout.
//
// The tracker is poll-driven, as the original is: a request starts only
// when Poll finds a step due and wins a pod-wide request slot. The chart
// therefore has no timers; the times it compares against are guards on the
// driver's clock.
package certificate

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/sc"
)

// State identifies a state of the certificate chart.
type State string

const (
	// Running is every state but Stopped.
	Running State = "running"

	// Idle is a node with no request in flight. Poll starts one when decide
	// has a step due.
	Idle State = "idle"

	// Requesting is one request: the original's request goroutine. Its
	// steps share certificateFetchTimeout, and leaving it gives back the
	// pod's request slot.
	Requesting State = "requesting"

	// Deciding picks the request's next step, or ends the request. It is
	// left at once.
	Deciding State = "deciding"

	// Obtaining gets a certificate while none is held.
	Obtaining State = "obtaining"

	// CheckingStore re-reads the store for another slot's renewal.
	CheckingStore State = "checkingStore"

	// FetchingWindow asks for the held certificate's renewal window.
	FetchingWindow State = "fetchingWindow"

	// Renewing renews the held certificate under the lease.
	Renewing State = "renewing"

	// Stopped is a closed node, which starts no more requests.
	Stopped State = "stopped"
)

// Service and action names. The hooks map services to metrics operations and
// logs.
const (
	serviceObtain     = "obtain"
	serviceCheckStore = "checkStore"
	serviceWindow     = "fetchWindow"
	serviceRenew      = "renew"

	actionEndRequest = "endRequest"
)

// certState is the chart's context: a node's certificate state. The phase
// of the original follows from it and the active states; see phaseOf.
type certState struct {
	// Cert is the certificate served, nil until one is obtained.
	Cert *tls.Certificate

	// Err is why the last obtain or renewal failed; nil once one succeeds.
	Err error

	// Failures counts consecutive failures that back off: refusals naming
	// their own retry time do not count.
	Failures int

	// RetryAt holds every request back after a failure; zero otherwise.
	RetryAt time.Time

	// While a certificate is held: RefreshAt is when to re-read the store,
	// WindowCheckAt when to ask for the renewal window, RenewAt when to
	// renew. A zero WindowCheckAt means at once.
	RefreshAt     time.Time
	WindowCheckAt time.Time
	RenewAt       time.Time

	// Steps counts the steps the request in flight has started.
	Steps int

	// RefreshIn is how long after a step the store is read again: the
	// jittered interval the poll that started the request drew. The
	// original draws it after every step; a chart's assignments must be
	// pure, so the driver draws it and the poll carries it.
	RefreshIn time.Duration
}

// poll asks the chart to start a request if one is due at the driver's time.
type poll struct {
	// RefreshIn is the jittered refresh interval for the request's steps.
	RefreshIn time.Duration
}

// closeTracker stops the node.
type closeTracker struct{}

// renewalWindow is the fetchWindow service's result.
type renewalWindow struct {
	RenewAt, CheckAgain time.Time
}

// errCertificateIssuing reports that another slot holds the connection's
// issuance lease. The slot retries shortly and then finds the certificate
// stored; it is not a failure.
var errCertificateIssuing = errors.New("another gateway slot is obtaining the certificate")

// errCertificateNotObtained fails a TLS handshake that arrives before the node
// holds its certificate.
var errCertificateNotObtained = errors.New("the gateway has not obtained its TLS certificate yet")

// errRenewalUnchanged is a renewal that returned the certificate it was
// asked to replace.
var errRenewalUnchanged = errors.New("the renewal returned the certificate it was asked to replace")

// maxStepsPerRequest bounds one request's steps: obtain or re-read the
// store, read the window, renew, read the new certificate's window.
const maxStepsPerRequest = 4

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

// decide is the step a node in c takes at now, or actionNone. It is the
// original's decide without the stopped phase, which is a state here.
func decide(c certState, now time.Time) certAction {
	switch {
	case now.Before(c.RetryAt):
		return actionNone
	case c.Cert == nil:
		return actionObtain
	case !now.Before(c.RefreshAt):
		return actionCheckStore
	case !now.Before(c.WindowCheckAt):
		return actionFetchWindow
	case !now.Before(c.RenewAt):
		return actionRenew
	default:
		return actionNone
	}
}

// certificateChart is the certificate chart, declared once for the process.
// Its services and the action that gives back a request slot are bound per
// tracker with Machine.Bind.
var certificateChart = sync.OnceValues(func() (*sc.Machine[State, certState], error) {
	m, err := sc.New("certificate", declareChart)
	if err != nil {
		return nil, fmt.Errorf("build certificate chart: %w", err)
	}
	return m, nil
})

func declareChart(b *sc.Builder[State, certState]) {
	b.Initial(Running)
	b.RequiresAction(actionEndRequest)

	b.State(Running, func(b *sc.Builder[State, certState]) {
		b.Initial(Idle)

		b.State(Idle).On[poll]().WhenAt("requestDue", requestDue).Assign("beginRequest", beginRequest).To(Requesting)

		b.State(Requesting, func(b *sc.Builder[State, certState]) {
			b.Initial(Deciding)

			deciding := b.State(Deciding)
			deciding.Always().WhenAt("obtainDue", due(actionObtain)).Assign("countObtain", countStep).To(Obtaining)
			deciding.Always().WhenAt("refreshDue", due(actionCheckStore)).Assign("countRefresh", countStep).To(CheckingStore)
			deciding.Always().WhenAt("windowDue", due(actionFetchWindow)).Assign("countWindow", countStep).To(FetchingWindow)
			deciding.Always().WhenAt("renewalDue", due(actionRenew)).Assign("countRenewal", countStep).To(Renewing)
			deciding.Always().To(Idle)

			obtain := b.State(Obtaining).Requires[*tls.Certificate](serviceObtain)
			obtain.OnDone().AssignAt("hold", hold).To(Deciding)
			obtain.OnError().AssignAt("failObtain", fail).To(Deciding)

			store := b.State(CheckingStore).Requires[*tls.Certificate](serviceCheckStore)
			store.OnDone().When("storedIsNewer", storedIsNewer).AssignAt("adoptStored", hold).To(Deciding)
			store.OnDone().AssignAt("scheduleRefresh", func(c certState, _ sc.Done[*tls.Certificate], now time.Time) certState {
				c.RefreshAt = now.Add(c.RefreshIn)
				return c
			}).To(Deciding)
			store.OnError().AssignAt("scheduleRefreshAfterError", func(c certState, _ sc.Failed, now time.Time) certState {
				c.RefreshAt = now.Add(c.RefreshIn)
				return c
			}).To(Deciding)

			window := b.State(FetchingWindow).Requires[renewalWindow](serviceWindow)
			window.OnDone().Assign("scheduleRenewal", func(c certState, d sc.Done[renewalWindow]) certState {
				c.RenewAt, c.WindowCheckAt = d.Result.RenewAt, d.Result.CheckAgain
				return c
			}).To(Deciding)
			window.OnError().AssignAt("renewTwoThirdsThrough", func(c certState, _ sc.Failed, now time.Time) certState {
				c.RenewAt, c.WindowCheckAt = fallbackRenewAt(c.Cert.Leaf), now.Add(renewalCheckMin)
				return c
			}).To(Deciding)

			renew := b.State(Renewing).Requires[*tls.Certificate](serviceRenew)
			renew.OnDone().When("renewalUnchanged", func(c certState, d sc.Done[*tls.Certificate]) bool {
				return d.Result.Leaf.Equal(c.Cert.Leaf)
			}).AssignAt("failUnchanged", func(c certState, _ sc.Done[*tls.Certificate], now time.Time) certState {
				return failed(c, errRenewalUnchanged, now)
			}).To(Deciding)
			renew.OnDone().AssignAt("holdRenewal", hold).To(Deciding)
			renew.OnError().AssignAt("failRenewal", fail).To(Deciding)
		}).Within(certificateFetchTimeout).OnExit(actionEndRequest)
	}).On[closeTracker]().To(Stopped)

	b.Final(Stopped)
}

// requestDue is whether a poll at now has a step to take.
func requestDue(c certState, _ poll, now time.Time) bool { return decide(c, now) != actionNone }

func beginRequest(c certState, p poll) certState {
	c.Steps, c.RefreshIn = 0, p.RefreshIn
	return c
}

// due is the guard of the eventless transition into a's step: the request
// has steps left and decide picks a.
func due(a certAction) func(certState, struct{}, time.Time) bool {
	return func(c certState, _ struct{}, now time.Time) bool {
		return c.Steps < maxStepsPerRequest && decide(c, now) == a
	}
}

func countStep(c certState, _ struct{}) certState {
	c.Steps++
	return c
}

// hold records an obtained, adopted or renewed certificate.
func hold(c certState, d sc.Done[*tls.Certificate], now time.Time) certState {
	return serving(c, d.Result, now)
}

// fail records a failed obtain or renewal.
func fail(c certState, f sc.Failed, now time.Time) certState { return failed(c, f.Err, now) }

func storedIsNewer(c certState, d sc.Done[*tls.Certificate]) bool { return replaces(d.Result, c.Cert) }

// serving holds cert with nothing failing; its window is asked for at once.
func serving(c certState, cert *tls.Certificate, now time.Time) certState {
	return certState{
		Cert: cert, Err: nil, Failures: 0, RetryAt: time.Time{},
		RefreshAt: now.Add(c.RefreshIn), WindowCheckAt: time.Time{}, RenewAt: time.Time{},
		Steps: c.Steps, RefreshIn: c.RefreshIn,
	}
}

// failed records a failed obtain or renewal and when to retry.
func failed(c certState, err error, now time.Time) certState {
	if !isCertificateRateLimited(err) && !isCertificateDomainPending(err) && !errors.Is(err, errCertificateIssuing) {
		c.Failures++
	}
	c.Err = err
	c.RetryAt = nextCertificateRetry(now, err, c.Failures)
	return c
}

// replaces reports whether stored is a later certificate than held.
func replaces(stored, held *tls.Certificate) bool {
	return stored != nil && !stored.Leaf.Equal(held.Leaf) && stored.Leaf.NotAfter.After(held.Leaf.NotAfter)
}

// certPhase is where a node is with its certificate, as the original names
// it. Here it follows from the chart's state and context.
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

func phaseOf(snap sc.Snapshot[State, certState]) certPhase {
	c := snap.Context
	switch {
	case snap.In(Stopped):
		return phaseStopped
	case c.Cert == nil && c.Err == nil:
		return phaseObtaining
	case c.Cert == nil:
		return phaseRetrying
	case c.Err == nil:
		return phaseServing
	default:
		return phaseRenewFailing
	}
}

// certStatus reports whether snap holds a certificate valid at now and, when
// it does not, why.
func certStatus(snap sc.Snapshot[State, certState], domain string, now time.Time) (bool, string) {
	c := snap.Context
	switch phase := phaseOf(snap); phase {
	case phaseObtaining:
		return false, "obtaining a TLS certificate for " + domain
	case phaseRetrying:
		return false, certificateFailureMessage(domain, c.Err, c.RetryAt)
	case phaseServing, phaseRenewFailing:
		if now.Before(c.Cert.Leaf.NotAfter) {
			return true, ""
		}
		if phase == phaseServing {
			return false, "the TLS certificate for " + domain + " has expired"
		}
		return false, fmt.Sprintf("the TLS certificate for %s has expired; %v; retrying after %s",
			domain, c.Err, c.RetryAt.UTC().Format("2006-01-02 15:04 UTC"))
	case phaseStopped:
		return false, "the gateway node is stopping"
	}
	return false, ""
}

// bind gives the chart's services and its end-of-request action their
// implementations for one tracker. Each service reads the tracker's clock
// itself: a service sees only the context, not the time of the step that
// started it.
func bind(m *sc.Machine[State, certState], source certificateSource, windows renewalWindows, now func() time.Time, endRequest func()) (*sc.Machine[State, certState], error) {
	bound, err := m.Bind(func(b *sc.Binder[State, certState]) {
		b.Service[*tls.Certificate](serviceObtain, func(ctx context.Context, _ certState) (*tls.Certificate, error) {
			return source.Obtain(ctx, now())
		})
		b.Service[*tls.Certificate](serviceCheckStore, func(ctx context.Context, _ certState) (*tls.Certificate, error) {
			return source.Stored(ctx)
		})
		b.Service[renewalWindow](serviceWindow, func(ctx context.Context, c certState) (renewalWindow, error) {
			renewAt, checkAgain, err := windows.RenewAt(ctx, c.Cert.Leaf, now())
			return renewalWindow{RenewAt: renewAt, CheckAgain: checkAgain}, err //nolint:wrapcheck // the window's error is logged as it is, as in the original
		})
		b.Service[*tls.Certificate](serviceRenew, func(ctx context.Context, c certState) (*tls.Certificate, error) {
			return source.Renew(ctx, c.Cert, now())
		})
		b.Action(actionEndRequest, func(context.Context, certState, sc.Move[State]) { endRequest() })
	})
	if err != nil {
		return nil, fmt.Errorf("bind certificate chart: %w", err)
	}
	return bound, nil
}
