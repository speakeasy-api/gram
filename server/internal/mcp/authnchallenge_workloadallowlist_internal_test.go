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

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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
		if l.release != nil {
			<-l.release
		}
		return l.issuer, l.found, l.err
	}
}

func TestWorkloadIssuerAdmission_TrustedIssuerResolves(t *testing.T) {
	t.Parallel()

	want := &remotesessions_repo.RemoteSessionIssuer{Slug: "gh-actions"}
	lookup := &countingLookup{issuer: want, found: true}
	admission := newWorkloadIssuerAdmission(lookup.fn())

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
	admission := newWorkloadIssuerAdmission(lookup.fn())

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
	admission := newWorkloadIssuerAdmission(lookup.fn())
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
	admission := newWorkloadIssuerAdmission(lookup.fn())
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

// Two endpoints in different tenancies resolve independently, so one
// rejection must never answer for the other.
func TestWorkloadIssuerAdmission_MissIsNotSharedAcrossTenancies(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadIssuerAdmission(lookup.fn())

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
	admission := newWorkloadIssuerAdmission(lookup.fn())

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
	admission := newWorkloadIssuerAdmission(lookup.fn())
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
	admission := newWorkloadIssuerAdmission(lookup.fn())
	endpoint := workloadTestTenant()

	for range 5 {
		_, err := admission.admit(t.Context(), endpoint, "not-a-url")
		require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	}

	require.EqualValues(t, 1, lookup.calls.Load())
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
		cache.remember("issuer-" + strconv.Itoa(i))
	}

	require.LessOrEqual(t, len(cache.expiries), maxEntries, "a flood of distinct issuers must evict, never grow")
	require.Len(t, cache.order, len(cache.expiries), "the insertion order must not outlive the entries it tracks")
	require.True(t, cache.seen("issuer-79"), "the most recent miss must still be held")
	require.False(t, cache.seen("issuer-0"), "the oldest miss must have been evicted")
}

// A lapsed entry must stop answering, so an issuer an operator has just added
// is looked up again rather than staying rejected. The live-entry half is
// asserted against a comfortable ttl so this cannot fail on a slow scheduler,
// and the lapse against a short one.
func TestWorkloadIssuerMissCache_EntryExpires(t *testing.T) {
	t.Parallel()

	held := newWorkloadIssuerMissCache(8, time.Minute)
	held.remember("issuer")
	require.True(t, held.seen("issuer"), "a fresh entry answers")

	lapsing := newWorkloadIssuerMissCache(8, time.Millisecond)
	lapsing.remember("issuer")
	require.Eventually(t, func() bool {
		return !lapsing.seen("issuer")
	}, time.Second, 5*time.Millisecond, "a miss must stop being remembered once its ttl lapses")

	require.Empty(t, lapsing.expiries, "a lapsed entry must be dropped, not merely ignored")
}

// Repeating a miss must not extend it. Otherwise a caller could pin an entry
// indefinitely and hold an issuer rejected past any configuration change.
func TestWorkloadIssuerMissCache_RepeatDoesNotExtendEntry(t *testing.T) {
	t.Parallel()

	cache := newWorkloadIssuerMissCache(8, time.Minute)
	cache.remember("issuer")
	first := cache.expiries["issuer"]

	cache.remember("issuer")

	require.Equal(t, first, cache.expiries["issuer"], "a repeated miss must not refresh the entry it hits")
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
