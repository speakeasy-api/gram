package mcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// workloadTenantEndpoint names the tenancy an admission resolves under, which
// is what the miss key is built from.
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
	issuer  *remotesessions_repo.RemoteSessionIssuer
	found   bool
	err     error
}

func (l *countingLookup) fn() workloadIssuerLookup {
	return func(_ context.Context, _ *ResolvedMcpEndpoint, _ string) (*remotesessions_repo.RemoteSessionIssuer, bool, error) {
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

	want := &remotesessions_repo.RemoteSessionIssuer{Slug: "gh-actions"}
	lookup := &countingLookup{issuer: want, found: true}
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)

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
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)

	row, err := admission.admit(t.Context(), workloadTestTenant(), "https://attacker.example.test")

	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	require.Nil(t, row, "a rejected issuer must yield no row, so no key source can be built from it")
}

// A repeated miss must not cost a repeated lookup: the grant is reachable
// without credentials, so the cheapest request must not be able to spend a
// query every time.
func TestWorkloadIssuerAdmission_RepeatedMissCostsOneLookup(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)
	endpoint := workloadTestTenant()

	for range 25 {
		_, err := admission.admit(t.Context(), endpoint, "https://attacker.example.test")
		require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	}

	require.EqualValues(t, 1, lookup.calls.Load(), "every rejection after the first must be served from the miss cache")
}

// A burst arriving together all passes the cache read before any of them
// records a miss, so the cache alone would not collapse it. Without
// coordination the cheapest possible flood still costs one query per request.
func TestWorkloadIssuerAdmission_ConcurrentMissesCollapseToOneLookup(t *testing.T) {
	t.Parallel()

	const callers = 32

	lookup := &countingLookup{found: false, release: make(chan struct{})}
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)
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
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)
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

	// The abandoned flight still lands its miss, so the next caller is served
	// from the cache instead of paying for the query again.
	close(lookup.release)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		reason, ok := admission.misses.seen(workloadIssuerMissKey(endpoint, issuerURL))
		if assert.True(c, ok, "the flight must record its miss even though its caller left") {
			assert.Equal(c, workloadIssuerMissUnknown, reason)
		}
	}, 5*time.Second, time.Millisecond)

	require.EqualValues(t, 1, lookup.calls.Load(), "abandoning a request must not cost the next one a second lookup")
}

// Two endpoints in different tenancies resolve independently, so one
// rejection must never answer for the other.
func TestWorkloadIssuerAdmission_MissIsNotSharedAcrossTenancies(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)

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
func TestWorkloadIssuerAdmission_MissIsNotSharedAcrossProjectsOnOneIssuer(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)

	organizationID := uuid.NewString()
	sharedIssuer := uuid.New()
	const issuerURL = "https://idp.example.test"

	_, err := admission.admit(t.Context(), workloadTenantEndpoint(organizationID, uuid.New(), sharedIssuer), issuerURL)
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	_, err = admission.admit(t.Context(), workloadTenantEndpoint(organizationID, uuid.New(), sharedIssuer), issuerURL)
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)

	require.EqualValues(t, 2, lookup.calls.Load(), "two projects sharing one issuer must not share a miss")
}

// A store failure says nothing about the issuer. Remembering it would keep
// rejecting a legitimate workload after the store recovered.
func TestWorkloadIssuerAdmission_LookupFailureIsNotRemembered(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{err: errors.New("connection refused")}
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)
	endpoint := workloadTestTenant()

	_, err := admission.admit(t.Context(), endpoint, "https://idp.example.test")
	require.Error(t, err)
	require.NotErrorIs(t, err, errWorkloadIssuerUntrusted, "an outage is not a trust decision")

	_, err = admission.admit(t.Context(), endpoint, "https://idp.example.test")
	require.Error(t, err)

	require.EqualValues(t, 2, lookup.calls.Load(), "a failed lookup must be retried, never cached")
}

// A malformed iss can never match a row, and is the cheapest thing a flood can
// carry, so it is remembered like any other miss.
func TestWorkloadIssuerAdmission_MalformedIssuerIsRejectedAndRemembered(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{err: fmt.Errorf("%w: no host", remotesessions.ErrIssuerURLInvalid)}
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)
	endpoint := workloadTestTenant()

	for range 5 {
		_, err := admission.admit(t.Context(), endpoint, "not-a-url")
		require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	}

	require.EqualValues(t, 1, lookup.calls.Load())
}

// The non-obvious half of the key, asserted where it matters rather than only
// at the key function. Lookup matches a closed candidate set that includes the
// caller's own spelling, so two inputs sharing a canonical form do not
// necessarily share a result: collapsing them would let the rejection of one
// answer for the spelling that would have matched.
func TestWorkloadIssuerAdmission_SpellingsSharingACanonicalFormResolveSeparately(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)
	endpoint := workloadTestTenant()

	_, err := admission.admit(t.Context(), endpoint, "https://idp.example.test")
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	_, err = admission.admit(t.Context(), endpoint, "https://IDP.example.test")
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)

	require.EqualValues(t, 2, lookup.calls.Load(), "a spelling the lookup may resolve differently must be resolved on its own")
}

// A cached rejection must answer the way the original did. Otherwise the same
// malformed iss would be a different error depending only on whether an entry
// happened to be live, and a caller mapping malformed to 400 and unknown to
// 401 would answer one request two ways.
func TestWorkloadIssuerAdmission_CachedMalformedMissKeepsItsTaxonomy(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{err: fmt.Errorf("%w: no host", remotesessions.ErrIssuerURLInvalid)}
	admission := newWorkloadIssuerAdmission(lookup.fn(), allowAllWorkloadLookups)
	endpoint := workloadTestTenant()

	_, first := admission.admit(t.Context(), endpoint, "not-a-url")
	require.ErrorIs(t, first, remotesessions.ErrIssuerURLInvalid)

	_, cached := admission.admit(t.Context(), endpoint, "not-a-url")
	require.ErrorIs(t, cached, errWorkloadIssuerUntrusted)
	require.ErrorIs(t, cached, remotesessions.ErrIssuerURLInvalid, "a repeat served from the cache must stay distinguishable from an unknown issuer")

	require.EqualValues(t, 1, lookup.calls.Load())
}

// An unknown issuer and a malformed one are both rejections, but they are not
// the same rejection, so one must never be served under the other's key.
func TestWorkloadIssuerMissCache_ReasonSurvivesTheEntry(t *testing.T) {
	t.Parallel()

	cache := newWorkloadIssuerMissCache(8, time.Minute)
	cache.remember("malformed", workloadIssuerMissMalformed)
	cache.remember("unknown", workloadIssuerMissUnknown)

	malformed, ok := cache.seen("malformed")
	require.True(t, ok)
	require.ErrorIs(t, malformed.err(), remotesessions.ErrIssuerURLInvalid)

	unknown, ok := cache.seen("unknown")
	require.True(t, ok)
	require.ErrorIs(t, unknown.err(), errWorkloadIssuerUntrusted)
	require.NotErrorIs(t, unknown.err(), remotesessions.ErrIssuerURLInvalid)
}

// A lapsed entry is a corpse, not a held entry: a fresh miss colliding with
// one must be recorded rather than silently dropped.
//
// admit reaches remember only through seen, which drops a lapsed entry on the
// way past, so this drives remember directly — the footgun is reachable by any
// second consumer that does not happen to read first, and asserting it through
// seen would assert nothing.
func TestWorkloadIssuerMissCache_LapsedEntryIsRecordedAgain(t *testing.T) {
	t.Parallel()

	cache := newWorkloadIssuerMissCache(8, time.Minute)
	cache.remember("issuer", workloadIssuerMissUnknown)

	// Lapse the entry in place, leaving it present but dead — the state a
	// read would have cleaned up.
	cache.entries["issuer"] = workloadIssuerMissEntry{
		expiry: time.Now().Add(-time.Minute),
		reason: workloadIssuerMissUnknown,
	}

	cache.remember("issuer", workloadIssuerMissMalformed)

	reason, ok := cache.seen("issuer")
	require.True(t, ok, "a miss recorded after the previous entry lapsed must be held")
	require.Equal(t, workloadIssuerMissMalformed, reason, "the new miss, not the lapsed one, must be what answers")
	require.Len(t, cache.order, 1, "the lapsed entry must not leave a second slot behind")
}

// The key must distinguish exactly what the lookup distinguishes: same
// tenancy and same spelling share an answer, and any difference in either
// does not.
func TestWorkloadIssuerMissKey_SeparatesTenancyAndSpelling(t *testing.T) {
	t.Parallel()

	organizationID := uuid.NewString()
	projectID, issuerID := uuid.New(), uuid.New()
	endpoint := workloadTenantEndpoint(organizationID, projectID, issuerID)
	const issuerURL = "https://idp.example.test"

	base := workloadIssuerMissKey(endpoint, issuerURL)

	require.Equal(t, base, workloadIssuerMissKey(workloadTenantEndpoint(organizationID, projectID, issuerID), issuerURL),
		"one tenancy and one spelling must produce one key")
	require.NotEqual(t, base, workloadIssuerMissKey(workloadTenantEndpoint(organizationID, uuid.New(), issuerID), issuerURL),
		"a different project resolves differently and must not share a key")
	require.NotEqual(t, base, workloadIssuerMissKey(workloadTenantEndpoint(uuid.NewString(), projectID, issuerID), issuerURL),
		"a different organization resolves differently and must not share a key")
	require.NotEqual(t, base, workloadIssuerMissKey(endpoint, "https://IDP.example.test"),
		"a spelling the lookup may resolve differently must not share a key")
}

// The issuer spelling arrives unauthenticated under no length bound, so an
// entry must not grow with it: otherwise the entry cap would bound a count of
// unbounded strings rather than memory.
func TestWorkloadIssuerMissKey_IsFixedSize(t *testing.T) {
	t.Parallel()

	endpoint := workloadTestTenant()

	short := workloadIssuerMissKey(endpoint, "https://a.test")
	long := workloadIssuerMissKey(endpoint, "https://a.test/"+strings.Repeat("x", 1<<20))

	require.Len(t, long, len(short), "a megabyte of issuer must occupy no more key than a short one")
}

// The cache is keyed partly by an unauthenticated request's own value, so it
// has to be bounded: without a cap, varying the issuer each time would make
// every rejection cost a permanent allocation.
func TestWorkloadIssuerMissCache_BoundedByEntries(t *testing.T) {
	t.Parallel()

	const maxEntries = 8
	cache := newWorkloadIssuerMissCache(maxEntries, time.Minute)

	for i := range maxEntries * 10 {
		cache.remember("issuer-"+strconv.Itoa(i), workloadIssuerMissUnknown)
	}

	require.LessOrEqual(t, len(cache.entries), maxEntries, "a flood of distinct issuers must evict, never grow")
	require.Len(t, cache.order, len(cache.entries), "the insertion order must not outlive the entries it tracks")
	_, held := cache.seen("issuer-79")
	require.True(t, held, "the most recent miss must still be held")
	_, evicted := cache.seen("issuer-0")
	require.False(t, evicted, "the oldest miss must have been evicted")
}

// A lapsed entry must stop answering, so an issuer an operator has just added
// is looked up again rather than staying rejected. The live-entry half is
// asserted against a comfortable ttl so this cannot fail on a slow scheduler,
// and the lapse against a short one.
func TestWorkloadIssuerMissCache_EntryExpires(t *testing.T) {
	t.Parallel()

	held := newWorkloadIssuerMissCache(8, time.Minute)
	held.remember("issuer", workloadIssuerMissUnknown)
	_, fresh := held.seen("issuer")
	require.True(t, fresh, "a fresh entry answers")

	lapsing := newWorkloadIssuerMissCache(8, time.Millisecond)
	lapsing.remember("issuer", workloadIssuerMissUnknown)
	require.Eventually(t, func() bool {
		_, still := lapsing.seen("issuer")
		return !still
	}, time.Second, 5*time.Millisecond, "a miss must stop being remembered once its ttl lapses")

	require.Empty(t, lapsing.entries, "a lapsed entry must be dropped, not merely ignored")
}

// Repeating a miss must not extend it. Otherwise a caller could pin an entry
// indefinitely and hold an issuer rejected past any configuration change.
func TestWorkloadIssuerMissCache_RepeatDoesNotExtendEntry(t *testing.T) {
	t.Parallel()

	cache := newWorkloadIssuerMissCache(8, time.Minute)
	cache.remember("issuer", workloadIssuerMissUnknown)
	first := cache.entries["issuer"]

	cache.remember("issuer", workloadIssuerMissUnknown)

	require.Equal(t, first, cache.entries["issuer"], "a repeated miss must not refresh the entry it hits")
	require.Len(t, cache.order, 1, "a repeated miss must not take a second slot")
}

// The database-backed lookup must reject a value that is not an issuer
// identifier before it queries anything. Passing a nil handle is the
// assertion: if the parse check did not come first, this would panic rather
// than return.
func TestNewWorkloadIssuerLookup_MalformedIssuerNeverReachesTheDatabase(t *testing.T) {
	t.Parallel()

	lookup := newWorkloadIssuerLookup(nil)

	for _, issuerURL := range []string{"", "not-a-url", "ftp://idp.example.test", "https://idp.example.test?probe=1"} {
		row, found, err := lookup(t.Context(), workloadTestTenant(), issuerURL)

		require.ErrorIs(t, err, remotesessions.ErrIssuerURLInvalid, "%q is not an issuer identifier", issuerURL)
		require.False(t, found)
		require.Nil(t, row)
	}
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

	lookup := &countingLookup{issuer: &remotesessions_repo.RemoteSessionIssuer{Slug: "gh"}, found: true}
	spent := func(context.Context, string) (ratelimit.Result, error) {
		return ratelimit.Result{Allowed: false, Remaining: 0, RetryAfter: 3 * time.Second}, nil
	}
	admission := newWorkloadIssuerAdmission(lookup.fn(), spent)

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

	lookup := &countingLookup{issuer: &remotesessions_repo.RemoteSessionIssuer{Slug: "gh"}, found: true}
	outage := errors.New("redis unreachable")
	admission := newWorkloadIssuerAdmission(lookup.fn(), func(context.Context, string) (ratelimit.Result, error) {
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

	lookup := &countingLookup{issuer: &remotesessions_repo.RemoteSessionIssuer{Slug: "gh"}, found: true}
	admission := newWorkloadIssuerAdmission(lookup.fn(), nil)

	_, err := admission.admit(t.Context(), workloadTestTenant(), "https://idp.example.test")

	require.ErrorIs(t, err, errWorkloadIssuerLimiterUnavailable)
	require.EqualValues(t, 0, lookup.calls.Load())
}

// Neither a refusal nor an outage says anything about the issuer, so neither
// may be remembered: caching one would keep rejecting a legitimate workload
// after the pressure passed.
func TestWorkloadIssuerAdmission_BudgetOutcomesAreNeverRemembered(t *testing.T) {
	t.Parallel()

	endpoint := workloadTestTenant()
	const issuerURL = "https://idp.example.test"

	for name, charge := range map[string]workloadIssuerBudget{
		"refused": func(context.Context, string) (ratelimit.Result, error) {
			return ratelimit.Result{Allowed: false, Remaining: 0, RetryAfter: time.Second}, nil
		},
		"store outage": func(context.Context, string) (ratelimit.Result, error) {
			return ratelimit.Result{Allowed: false, Remaining: 0, RetryAfter: 0}, errors.New("redis unreachable")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			admission := newWorkloadIssuerAdmission((&countingLookup{found: false}).fn(), charge)
			_, err := admission.admit(t.Context(), endpoint, issuerURL)
			require.Error(t, err)

			_, held := admission.misses.seen(workloadIssuerMissKey(endpoint, issuerURL))
			require.False(t, held, "%s is a statement about load, not about the issuer", name)
		})
	}
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
