package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	workloadidentity_repo "github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

// workloadTenantEndpoint names the tenancy an admission resolves under, which
// is what the flight key is built from.
func workloadTenantEndpoint(organizationID string, projectID, issuerID uuid.UUID) *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{
		OrganizationID:      organizationID,
		ProjectID:           projectID,
		UserSessionIssuerID: issuerID,
	}
}

// workloadTestTenant is a fresh, fully distinct tenancy.
func workloadTestTenant() *ResolvedMcpEndpoint {
	return workloadTenantEndpoint(uuid.NewString(), uuid.New(), uuid.New())
}

// newWorkloadTestAdmission builds admission with no store behind it. There is
// nothing to isolate between parallel tests: the only shared state is the
// in-process flight map, and each test builds its own.
func newWorkloadTestAdmission(t *testing.T, lookup workloadIssuerLookup, charge workloadIssuerBudget) *workloadIssuerAdmission {
	t.Helper()

	return newWorkloadIssuerAdmission(lookup, charge)
}

// countingLookup records every call so a test can assert what the miss path
// did and did not consult. The counter is atomic because the concurrency test
// calls it from many goroutines at once.
type countingLookup struct {
	calls atomic.Int64
	// running and peak track how many lookups are inside the function at once,
	// so a test can assert the slot bound rather than infer it from timing.
	running atomic.Int64
	peak    atomic.Int64
	// release, when non-nil, holds the lookup open until the test closes it,
	// so a test can guarantee callers pile up behind one in-flight call.
	release chan struct{}
	issuer  workloadidentity_repo.WorkloadIssuer
	found   bool
	err     error
}

func (l *countingLookup) fn() workloadIssuerLookup {
	return func(_ context.Context, _ *ResolvedMcpEndpoint, _ string) (workloadidentity_repo.WorkloadIssuer, bool, error) {
		l.calls.Add(1)
		l.enter()
		defer l.running.Add(-1)

		if l.release != nil {
			<-l.release
		}
		return l.issuer, l.found, l.err
	}
}

// enter records one more concurrent lookup, raising the high-water mark.
func (l *countingLookup) enter() {
	running := l.running.Add(1)
	for {
		peak := l.peak.Load()
		if running <= peak || l.peak.CompareAndSwap(peak, running) {
			return
		}
	}
}

func TestWorkloadIssuerAdmission_TrustedIssuerResolves(t *testing.T) {
	t.Parallel()

	want := workloadidentity_repo.WorkloadIssuer{ID: uuid.New(), Name: "gh-actions", JwksUri: "https://token.actions.example.test/jwks"}
	lookup := &countingLookup{issuer: want, found: true}
	admission := newWorkloadTestAdmission(t, lookup.fn(), allowAllWorkloadLookups)

	got, err := admission.admit(t.Context(), workloadTestTenant(), "https://token.actions.example.test")

	require.NoError(t, err)
	require.Equal(t, want, got)
}

// The core property of this ticket: an issuer nobody trusts is rejected
// without a key source ever being built, so nothing downstream can turn a
// request-supplied URL into an outbound fetch.
func TestWorkloadIssuerAdmission_UntrustedIssuerRejectedWithoutEgress(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadTestAdmission(t, lookup.fn(), allowAllWorkloadLookups)

	row, err := admission.admit(t.Context(), workloadTestTenant(), "https://attacker.example.test")

	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	require.Equal(t, workloadidentity_repo.WorkloadIssuer{}, row, "a rejected issuer must yield no row, so no key source can be built from it")
}

// Without coordination, callers arriving together each cost a query. The
// limiter bounds sustained load; this is what bounds a simultaneous burst.
func TestWorkloadIssuerAdmission_ConcurrentMissesCollapseToOneLookup(t *testing.T) {
	t.Parallel()

	const callers = 32

	lookup := &countingLookup{found: false, release: make(chan struct{})}
	admission := newWorkloadTestAdmission(t, lookup.fn(), allowAllWorkloadLookups)
	endpoint := workloadTestTenant()

	// Held inside admit: one caller occupies the lookup, the rest join it.
	var waiting atomic.Int64
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			waiting.Add(1)
			defer waiting.Add(-1)
			_, err := admission.admit(t.Context(), endpoint, "https://attacker.example.test")
			require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
		})
	}

	// Every caller is inside admit before any of them is allowed to finish,
	// so the burst is genuinely simultaneous rather than accidentally serial.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.EqualValues(c, callers, waiting.Load())
	}, 5*time.Second, time.Millisecond)

	close(lookup.release)
	wg.Wait()

	require.EqualValues(t, 1, lookup.calls.Load(), "concurrent rejections of one issuer must share a single lookup")
}

// Detaching the flight must not cost the caller its own cancellation. A client
// that disconnects has to stop waiting immediately, while the flight it opened
// carries on for anyone sharing it and still records what it found.
func TestWorkloadIssuerAdmission_AbandonedCallerStopsWaitingButFlightFinishes(t *testing.T) {
	t.Parallel()

	const issuerURL = "https://attacker.example.test"

	lookup := &countingLookup{found: false, release: make(chan struct{})}
	admission := newWorkloadTestAdmission(t, lookup.fn(), allowAllWorkloadLookups)
	endpoint := workloadTestTenant()

	ctx, cancel := context.WithCancel(t.Context())
	var abandoned error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, abandoned = admission.admit(ctx, endpoint, issuerURL)
	}()

	// The flight is inside the held-open lookup before the caller gives up, so
	// this abandons work that is genuinely still running.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.EqualValues(c, 1, lookup.running.Load())
	}, 5*time.Second, time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a caller that gave up was left waiting on the flight it opened")
	}

	require.ErrorIs(t, abandoned, context.Canceled)
	require.NotErrorIs(t, abandoned, errWorkloadIssuerUntrusted, "giving up decides nothing about the issuer, and must not be reported as a rejection")

	// The flight runs to completion on its own timeout rather than being
	// torn down with the caller that opened it. Releasing the lookup lets it
	// finish; nothing restarts it.
	close(lookup.release)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.EqualValues(c, 0, lookup.running.Load(), "the abandoned flight must finish rather than hang")
	}, 5*time.Second, time.Millisecond)

	require.EqualValues(t, 1, lookup.calls.Load(), "a caller giving up must not cause the lookup to run again")
}

// Two endpoints in different tenancies resolve independently, so one
// rejection must never answer for the other.
func TestWorkloadIssuerAdmission_FlightIsNotSharedAcrossTenancies(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadTestAdmission(t, lookup.fn(), allowAllWorkloadLookups)

	const issuerURL = "https://idp.example.test"
	_, err := admission.admit(t.Context(), workloadTestTenant(), issuerURL)
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	_, err = admission.admit(t.Context(), workloadTestTenant(), issuerURL)
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)

	require.EqualValues(t, 2, lookup.calls.Load(), "a second tenancy must be resolved on its own, not from the first's miss")
}

// One user session issuer can back endpoints in different projects: the
// mcp_servers foreign key to it carries no project pinning, unlike
// meta_mcp_servers' composite one. Since the lookup resolves under the
// project, a miss recorded for one must not deny a project-tier trusted
// issuer in the other.
func TestWorkloadIssuerAdmission_FlightIsNotSharedAcrossProjectsOnOneIssuer(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadTestAdmission(t, lookup.fn(), allowAllWorkloadLookups)

	organizationID := uuid.NewString()
	sharedIssuer := uuid.New()
	const issuerURL = "https://idp.example.test"

	_, err := admission.admit(t.Context(), workloadTenantEndpoint(organizationID, uuid.New(), sharedIssuer), issuerURL)
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	_, err = admission.admit(t.Context(), workloadTenantEndpoint(organizationID, uuid.New(), sharedIssuer), issuerURL)
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)

	require.EqualValues(t, 2, lookup.calls.Load(), "two projects sharing one issuer must not share a flight")
}

// A malformed iss is reported as untrusted with the parse failure wrapped, so
// a caller can tell "could never name a row" from "names no row we hold"
// without the two answering differently on the wire.
func TestWorkloadIssuerAdmission_MalformedIssuerKeepsItsReason(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{err: fmt.Errorf("%w: no host", errWorkloadIssuerURLInvalid)}
	admission := newWorkloadTestAdmission(t, lookup.fn(), allowAllWorkloadLookups)

	_, err := admission.admit(t.Context(), workloadTestTenant(), "not-a-url")
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	require.ErrorIs(t, err, errWorkloadIssuerURLInvalid)
}

// The non-obvious half of the key, asserted where it matters rather than only
// at the key function. Lookup matches a closed candidate set that includes the
// caller's own spelling, so two inputs sharing a canonical form do not
// necessarily share a result: collapsing them would let the rejection of one
// answer for the spelling that would have matched.
func TestWorkloadIssuerAdmission_SpellingsSharingACanonicalFormResolveSeparately(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadTestAdmission(t, lookup.fn(), allowAllWorkloadLookups)
	endpoint := workloadTestTenant()

	_, err := admission.admit(t.Context(), endpoint, "https://idp.example.test")
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	_, err = admission.admit(t.Context(), endpoint, "https://IDP.example.test")
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)

	require.EqualValues(t, 2, lookup.calls.Load(), "a spelling the lookup may resolve differently must be resolved on its own")
}

// The key must distinguish exactly what the lookup distinguishes: same
// tenancy and same spelling share an answer, and any difference in either
// does not.
func TestWorkloadIssuerFlightKey_SeparatesTenancyAndSpelling(t *testing.T) {
	t.Parallel()

	organizationID := uuid.NewString()
	projectID, issuerID := uuid.New(), uuid.New()
	endpoint := workloadTenantEndpoint(organizationID, projectID, issuerID)
	const issuerURL = "https://idp.example.test"

	base := workloadIssuerFlightKey(endpoint, issuerURL)

	require.Equal(t, base, workloadIssuerFlightKey(workloadTenantEndpoint(organizationID, projectID, issuerID), issuerURL),
		"one tenancy and one spelling must produce one key")
	require.NotEqual(t, base, workloadIssuerFlightKey(workloadTenantEndpoint(organizationID, uuid.New(), issuerID), issuerURL),
		"a different project resolves differently and must not share a key")
	require.NotEqual(t, base, workloadIssuerFlightKey(workloadTenantEndpoint(uuid.NewString(), projectID, issuerID), issuerURL),
		"a different organization resolves differently and must not share a key")
	require.NotEqual(t, base, workloadIssuerFlightKey(endpoint, "https://IDP.example.test"),
		"a spelling the lookup may resolve differently must not share a key")
}

// The issuer spelling arrives unauthenticated under no length bound, so an
// entry must not grow with it: otherwise the entry cap would bound a count of
// unbounded strings rather than memory.
func TestWorkloadIssuerFlightKey_IsFixedSize(t *testing.T) {
	t.Parallel()

	endpoint := workloadTestTenant()

	short := workloadIssuerFlightKey(endpoint, "https://a.test")
	long := workloadIssuerFlightKey(endpoint, "https://a.test/"+strings.Repeat("x", 1<<20))

	require.Len(t, long, len(short), "a megabyte of issuer must occupy no more key than a short one")
}

// allowAllWorkloadLookups is the budget for tests about something other than
// the budget: every charge succeeds, so admission behaves as it does for an
// endpoint well inside its ceiling.
func allowAllWorkloadLookups(context.Context, string) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true, Remaining: 1, RetryAfter: 0}, nil
}

// The ceiling is the bound this path relies on, so a refusal must be its own
// answer. Reported as errWorkloadIssuerUntrusted it would tell a caller the
// issuer was rejected, which is a 401 and a lie; reported as nothing in
// particular it would surface as a 5xx.
func TestWorkloadIssuerAdmission_SpentBudgetIsNotATrustDecision(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{issuer: workloadidentity_repo.WorkloadIssuer{ID: uuid.New()}, found: true}
	spent := func(context.Context, string) (ratelimit.Result, error) {
		return ratelimit.Result{Allowed: false, Remaining: 0, RetryAfter: 3 * time.Second}, nil
	}
	admission := newWorkloadTestAdmission(t, lookup.fn(), spent)

	_, err := admission.admit(t.Context(), workloadTestTenant(), "https://idp.example.test")

	require.ErrorIs(t, err, errWorkloadIssuerLookupRateLimited)
	require.NotErrorIs(t, err, errWorkloadIssuerUntrusted, "a spent budget decides nothing about the issuer")
	require.EqualValues(t, 0, lookup.calls.Load(), "a refused charge must cost no query")
}

// An unreachable bucket is not a throttle. Running the lookup anyway would
// spend exactly the budget the ceiling exists to protect, so it fails closed —
// and stays distinguishable from a refusal an operator could wait out.
func TestWorkloadIssuerAdmission_LimiterOutageFailsClosed(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{issuer: workloadidentity_repo.WorkloadIssuer{ID: uuid.New()}, found: true}
	outage := errors.New("redis unreachable")
	admission := newWorkloadTestAdmission(t, lookup.fn(), func(context.Context, string) (ratelimit.Result, error) {
		return ratelimit.Result{Allowed: false, Remaining: 0, RetryAfter: 0}, outage
	})

	_, err := admission.admit(t.Context(), workloadTestTenant(), "https://idp.example.test")

	require.ErrorIs(t, err, errWorkloadIssuerLimiterUnavailable)
	require.ErrorIs(t, err, outage)
	require.NotErrorIs(t, err, errWorkloadIssuerLookupRateLimited, "an outage must not read as a rate limit")
	require.EqualValues(t, 0, lookup.calls.Load(), "an unbounded lookup is exactly what the ceiling prevents")
}

// A ceiling that was never wired leaves this path with no bound at all. On a
// grant reachable without credentials that is the condition the bound exists
// for, so it refuses rather than running unprotected.
func TestWorkloadIssuerAdmission_AbsentBudgetRefuses(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{issuer: workloadidentity_repo.WorkloadIssuer{ID: uuid.New()}, found: true}
	admission := newWorkloadTestAdmission(t, lookup.fn(), nil)

	_, err := admission.admit(t.Context(), workloadTestTenant(), "https://idp.example.test")

	require.ErrorIs(t, err, errWorkloadIssuerLimiterUnavailable)
	require.EqualValues(t, 0, lookup.calls.Load())
}

// The budget is per endpoint, which is the property that keeps a mitigation
// from becoming a cross-tenant denial surface: one endpoint's spend must never
// be charged against another's.
func TestWorkloadIssuerLookupScope_SeparatesEndpoints(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	shared := workloadTenantEndpoint(uuid.NewString(), uuid.New(), issuerID)

	require.Equal(t, workloadIssuerLookupScope(shared),
		workloadIssuerLookupScope(workloadTenantEndpoint(uuid.NewString(), uuid.New(), issuerID)),
		"one authorization server is one budget, whatever project addresses it")
	require.NotEqual(t, workloadIssuerLookupScope(shared), workloadIssuerLookupScope(workloadTestTenant()),
		"a different endpoint must not spend this one's budget")
	require.NotEqual(t, workloadIssuerLookupScope(shared), workloadFetchScope(shared),
		"key fetches and admission lookups bound different resources and must not share a bucket")
}

// Without a store there are no buckets, and admission must not read that as
// permission to run unbounded.
func TestNewWorkloadIssuerLookupBudget_NilWithoutAStore(t *testing.T) {
	t.Parallel()

	require.Nil(t, newWorkloadIssuerLookupBudget(nil, testenv.NewMeterProvider(t)),
		"no store means no ceiling, which admission refuses rather than ignores")
}
