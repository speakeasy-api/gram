package certificate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// The chart's share of the rules in CertLifecycle.tla, checked on every state
// the chart reaches. The model covers several slots sharing a store and a
// lease; one chart is one slot's tracker, so what can be checked here is
// what each slot must do for the connection-wide rules to hold:
//
//   - AtMostOneIssuance: a slot holding a certificate never orders one, a
//     slot runs one step at a time, only a poll starts a request (a client
//     handshake never does, which HandshakeIssuance models), and no request
//     starts before the retry time Let's Encrypt named.
//   - NoOrphanLeaseOnLiveSlot: every request that ends gives back the pod's
//     request slot. The lease itself is taken and released inside one step's
//     service, which the chart cannot see.
//   - OnlineHasCertificate: a slot reports ready only while it holds a
//     certificate, and it never drops one it holds.
//   - The sanity checks NeverIssued and NeverAllOnline, which show the
//     model reaches what the properties are about, become Report.Reached.
//
// EventuallyOnline is a liveness property under fairness. Report.Trapped
// only asks whether a top-level final state stays reachable, and Stopped
// always is through closeTracker, so it proves nothing here. Leaving
// closeTracker out makes every state trapped instead. What the chart owes
// EventuallyOnline, that a poll once the retry is due always starts an
// obtain while no certificate is held, is a reachability property from every
// state ("a held state stays reachable"), which Explore cannot state.
//
// The exploration runs on its own clock, which polls and service results
// share. Time passes only through Waits, so the certificates and windows it
// hands out last hours instead of months, which keeps the number of
// distinct distances to the context's times small.

// exploreStart is when the exploration starts: the tracker tests' clock.
var exploreStart = stateNow

// rateLimitRetryAt is the retry time errRateLimited names. nextCertificateRetry
// reads it against the clock, so the exploration's key must say which side of
// it a state is on.
var rateLimitRetryAt = time.Date(2026, time.November, 12, 22, 53, 32, 0, time.UTC)

// exploreCerts are the certificates an exploration's services hand out.
type exploreCerts struct {
	first, renewed, older *tls.Certificate
}

// certificateBetween is a self-signed certificate valid from notBefore to
// notAfter.
func certificateBetween(t *testing.T, notBefore, notAfter time.Time) *tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + serials.Add(1)), Subject: pkix.Name{CommonName: "private.example.ts.net"},
		DNSNames: []string{"private.example.ts.net"}, NotBefore: notBefore, NotAfter: notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func newExploreCerts(t *testing.T) exploreCerts {
	t.Helper()
	return exploreCerts{
		first:   certificateBetween(t, exploreStart, exploreStart.Add(9*time.Hour)),
		renewed: certificateBetween(t, exploreStart.Add(3*time.Hour), exploreStart.Add(18*time.Hour)),
		older:   certificateBetween(t, exploreStart.Add(-6*time.Hour), exploreStart.Add(3*time.Hour)),
	}
}

// name is how the exploration's key tells the certificates apart.
func (e exploreCerts) name(cert *tls.Certificate) string {
	switch cert {
	case nil:
		return ""
	case e.first:
		return "first"
	case e.renewed:
		return "renewed"
	case e.older:
		return "older"
	}
	return "unknown"
}

// errorClass is how the exploration's key tells the errors apart: by how the
// chart treats them.
func errorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case isCertificateRateLimited(err):
		return "rate_limited"
	case isCertificateDomainPending(err):
		return "domain_pending"
	case errors.Is(err, errCertificateIssuing):
		return "issuing"
	case errors.Is(err, errRenewalUnchanged):
		return "unchanged"
	}
	return "error"
}

// key is everything the chart's guards read, with each stored time as its
// distance from now: a time already past is due, however long ago. Failures
// beyond seven back off the same as seven.
func (e exploreCerts) key(c certState, now time.Time) any {
	left := func(at time.Time) time.Duration { return max(at.Sub(now), 0) }
	return struct {
		Cert          string        `json:"cert"`
		Err           string        `json:"err"`
		Failures      int           `json:"failures"`
		RetryAt       time.Duration `json:"retry_at"`
		RefreshAt     time.Duration `json:"refresh_at"`
		WindowCheckAt time.Duration `json:"window_check_at"`
		RenewAt       time.Duration `json:"renew_at"`
		Steps         int           `json:"steps"`
		RefreshIn     time.Duration `json:"refresh_in"`
		BeforeLimit   bool          `json:"before_limit"`
	}{
		e.name(c.Cert), errorClass(c.Err), min(c.Failures, 7),
		left(c.RetryAt), left(c.RefreshAt), left(c.WindowCheckAt), left(c.RenewAt),
		c.Steps, c.RefreshIn, now.Before(rateLimitRetryAt),
	}
}

// startsService reports whether a step started a run of name.
func startsService(st sc.Step[State, certState], name string) bool {
	return slices.ContainsFunc(st.Effects, func(e sc.Effect[certState]) bool {
		return e.Kind == sc.EffectStartInvocation && e.Name == name
	})
}

func startsAnyService(st sc.Step[State, certState]) bool {
	return slices.ContainsFunc(st.Effects, func(e sc.Effect[certState]) bool { return e.Kind == sc.EffectStartInvocation })
}

func certificateExploration(e exploreCerts) sc.Exploration[State, certState] {
	window := func(id string, now time.Time) any {
		return sc.Done[renewalWindow]{ID: id, Result: renewalWindow{RenewAt: now.Add(3 * time.Hour), CheckAgain: now.Add(renewalCheckMax)}}
	}
	pending := errors.New(`obtain tailnet service certificate: invalid domain "private.example.ts.net"; must be one of []`)
	reset := errors.New("acme: connection reset")

	return sc.Exploration[State, certState]{
		Events: []any{closeTracker{}, poll{RefreshIn: certificateRefreshInterval}},
		Outcomes: map[string][]sc.Outcome{
			serviceObtain: {
				sc.Succeeds(e.first), sc.Fails(errRateLimited), sc.Fails(errCertificateIssuing), sc.Fails(pending), sc.Fails(reset),
			},
			serviceCheckStore: {
				sc.Succeeds[*tls.Certificate](nil), sc.Succeeds(e.first), sc.Succeeds(e.renewed),
				sc.Succeeds(e.older), sc.Fails(errors.New("decrypt failed")),
			},
			serviceWindow: {window, sc.Fails(errors.New("directory unreachable"))},
			serviceRenew: {
				sc.Succeeds(e.renewed), sc.Succeeds(e.first),
				sc.Fails(errRateLimited), sc.Fails(errCertificateIssuing), sc.Fails(reset),
			},
		},
		Key:              e.key,
		Start:            exploreStart,
		Waits:            []time.Duration{30 * time.Minute, time.Hour},
		CheckDeterminism: true,
		Invariants: []sc.Invariant[State, certState]{
			{Name: "one step in flight at most", Holds: func(s sc.Snapshot[State, certState]) bool {
				return len(s.Services()) <= 1
			}},
			{Name: "a step in flight belongs to a request within its step budget", Holds: func(s sc.Snapshot[State, certState]) bool {
				return len(s.Services()) == 0 || (s.In(Requesting) && s.Context.Steps >= 1 && s.Context.Steps <= maxStepsPerRequest)
			}},
			{Name: "a stopped node runs nothing", Holds: func(s sc.Snapshot[State, certState]) bool {
				return !s.In(Stopped) || len(s.Services()) == 0
			}},
		},
		Steps: []sc.StepInvariant[State, certState]{
			// An Invariant sees no clock, and readiness depends on it, so
			// this is checked on the step into each state, at its time.
			{Name: "OnlineHasCertificate: ready only with a certificate", Holds: func(st sc.Step[State, certState]) bool {
				ready, _ := certStatus(st.To, "private.example.ts.net", st.At)
				return !ready || st.To.Context.Cert != nil
			}},
			{Name: "AtMostOneIssuance: a slot holding a certificate never orders one", Holds: func(st sc.Step[State, certState]) bool {
				return !startsService(st, serviceObtain) || st.From.Context.Cert == nil
			}},
			{Name: "a held certificate is never dropped", Holds: func(st sc.Step[State, certState]) bool {
				return st.From.Context.Cert == nil || st.To.Context.Cert != nil
			}},
			{Name: "only a poll starts a request", Holds: func(st sc.Step[State, certState]) bool {
				_, isPoll := st.Event.(poll)
				return st.From.In(Requesting) || !st.To.In(Requesting) || isPoll
			}},
			{Name: "no request before the retry time", Holds: func(st sc.Step[State, certState]) bool {
				return !startsAnyService(st) || !st.At.Before(st.From.Context.RetryAt)
			}},
			{Name: "a certificate's window is read before it is renewed", Holds: func(st sc.Step[State, certState]) bool {
				return !startsService(st, serviceRenew) || !st.To.Context.WindowCheckAt.IsZero()
			}},
			{Name: "a renewal starts only at its renewal time", Holds: func(st sc.Step[State, certState]) bool {
				return !startsService(st, serviceRenew) || !st.At.Before(st.To.Context.RenewAt)
			}},
			{Name: "NoOrphanLeaseOnLiveSlot: a request that ends gives back its slot", Holds: func(st sc.Step[State, certState]) bool {
				ended := st.From.In(Requesting) && !st.To.In(Requesting)
				return !ended || slices.ContainsFunc(st.Effects, func(e sc.Effect[certState]) bool {
					return e.Kind == sc.EffectAction && e.Name == actionEndRequest
				})
			}},
		},
		// The request budget's DeadlineExceeded failures put the reachable
		// state space at about 160,000 states, past the default limit.
		MaxStates: 500_000,
	}
}

func TestChartKeepsTheLifecycleRules(t *testing.T) {
	t.Parallel()
	e := newExploreCerts(t)
	m := testChart(t)

	report, err := m.Explore(certState{}, certificateExploration(e))
	require.NoError(t, err)

	for _, v := range report.Violations {
		t.Logf("%s:\n%s", v.Invariant, m.FormatTrace(v.Trace))
	}
	require.Empty(t, report.Violations)
	require.Empty(t, report.Stuck)
	require.Empty(t, report.Trapped)
	require.False(t, report.Truncated)
	t.Logf("%d states", report.States)

	// NeverIssued and NeverAllOnline: the exploration reaches holding a
	// certificate, renewing it, and every step in between.
	for _, s := range []State{Idle, Obtaining, CheckingStore, FetchingWindow, Renewing, Stopped} {
		require.True(t, report.Reached(s), "%s is reached", s)
	}
}

// TestChartTrustsTheRenewalItIsGiven records a property of the original that
// the exploration found: a renewal is adopted whenever it differs from the
// held certificate, even when it expires sooner. Re-reading the store adopts
// only a later certificate. tsnet is asked for one valid longer than the held
// one, so this does not happen in practice; the check documents that the
// tracker relies on it.
func TestChartTrustsTheRenewalItIsGiven(t *testing.T) {
	t.Parallel()
	e := newExploreCerts(t)
	m := testChart(t)
	x := certificateExploration(e)
	x.Invariants, x.Steps = nil, []sc.StepInvariant[State, certState]{{
		Name: "a certificate is only replaced by one that expires later",
		Holds: func(st sc.Step[State, certState]) bool {
			from, to := st.From.Context.Cert, st.To.Context.Cert
			return from == nil || to == from || to.Leaf.NotAfter.After(from.Leaf.NotAfter)
		},
	}}

	report, err := m.Explore(certState{}, x)
	require.NoError(t, err)
	require.Len(t, report.Violations, 1)
	trace := report.Violations[0].Trace
	t.Logf("\n%s", m.FormatTrace(trace))
	last, ok := trace.Events[len(trace.Events)-1].Event.(sc.Done[*tls.Certificate])
	require.True(t, ok, "the offending step is a service's result")
	require.Same(t, e.first, last.Result, "a renewal into the first certificate")
	require.Same(t, e.first, trace.Snapshot.Context.Cert)

	// The trace replays exactly from its start.
	snap, _ := m.Start(certState{}, trace.Start)
	for snap = range m.Fold(snap, slices.Values(trace.Events)) {
	}
	require.Same(t, e.first, snap.Context.Cert)
	require.Equal(t, trace.Snapshot.Active(), snap.Active())
}
