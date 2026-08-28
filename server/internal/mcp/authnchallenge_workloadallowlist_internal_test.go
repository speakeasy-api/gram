package mcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// countingLookup records every call so a test can assert what the miss path
// did and did not consult.
type countingLookup struct {
	calls  int
	issuer *remotesessions_repo.RemoteSessionIssuer
	found  bool
	err    error
}

func (l *countingLookup) fn() workloadIssuerLookup {
	return func(_ context.Context, _ *ResolvedMcpEndpoint, _ string) (*remotesessions_repo.RemoteSessionIssuer, bool, error) {
		l.calls++
		return l.issuer, l.found, l.err
	}
}

func TestWorkloadIssuerAdmission_TrustedIssuerResolves(t *testing.T) {
	t.Parallel()

	want := &remotesessions_repo.RemoteSessionIssuer{Slug: "gh-actions"}
	lookup := &countingLookup{issuer: want, found: true}
	admission := newWorkloadIssuerAdmission(lookup.fn())

	got, err := admission.admit(t.Context(), workloadTestEndpoint(uuid.New()), "https://token.actions.example.test")

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

	row, err := admission.admit(t.Context(), workloadTestEndpoint(uuid.New()), "https://attacker.example.test")

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
	endpoint := workloadTestEndpoint(uuid.New())

	for range 25 {
		_, err := admission.admit(t.Context(), endpoint, "https://attacker.example.test")
		require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	}

	require.Equal(t, 1, lookup.calls, "every rejection after the first must be served from the miss cache")
}

// Trust is per endpoint, so one endpoint's rejection must never answer for
// another's. Caching across endpoints would deny a workload that its own
// endpoint legitimately trusts.
func TestWorkloadIssuerAdmission_MissIsNotSharedAcrossEndpoints(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{found: false}
	admission := newWorkloadIssuerAdmission(lookup.fn())

	const issuerURL = "https://idp.example.test"
	_, err := admission.admit(t.Context(), workloadTestEndpoint(uuid.New()), issuerURL)
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	_, err = admission.admit(t.Context(), workloadTestEndpoint(uuid.New()), issuerURL)
	require.ErrorIs(t, err, errWorkloadIssuerUntrusted)

	require.Equal(t, 2, lookup.calls, "a second endpoint must be resolved on its own, not from the first's miss")
}

// A store failure says nothing about the issuer. Remembering it would keep
// rejecting a legitimate workload after the store recovered.
func TestWorkloadIssuerAdmission_LookupFailureIsNotRemembered(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{err: errors.New("connection refused")}
	admission := newWorkloadIssuerAdmission(lookup.fn())
	endpoint := workloadTestEndpoint(uuid.New())

	_, err := admission.admit(t.Context(), endpoint, "https://idp.example.test")
	require.Error(t, err)
	require.NotErrorIs(t, err, errWorkloadIssuerUntrusted, "an outage is not a trust decision")

	_, err = admission.admit(t.Context(), endpoint, "https://idp.example.test")
	require.Error(t, err)

	require.Equal(t, 2, lookup.calls, "a failed lookup must be retried, never cached")
}

// A malformed iss can never match a row, and is the cheapest thing a flood can
// carry, so it is remembered like any other miss.
func TestWorkloadIssuerAdmission_MalformedIssuerIsRejectedAndRemembered(t *testing.T) {
	t.Parallel()

	lookup := &countingLookup{err: fmt.Errorf("%w: no host", remotesessions.ErrIssuerURLInvalid)}
	admission := newWorkloadIssuerAdmission(lookup.fn())
	endpoint := workloadTestEndpoint(uuid.New())

	for range 5 {
		_, err := admission.admit(t.Context(), endpoint, "not-a-url")
		require.ErrorIs(t, err, errWorkloadIssuerUntrusted)
	}

	require.Equal(t, 1, lookup.calls)
}

// The database-backed lookup must reject a value that is not an issuer
// identifier before it queries anything. Passing a nil handle is the
// assertion: if the parse check did not come first, this would panic rather
// than return.
func TestNewWorkloadIssuerLookup_MalformedIssuerNeverReachesTheDatabase(t *testing.T) {
	t.Parallel()

	lookup := newWorkloadIssuerLookup(nil)

	for _, issuerURL := range []string{"", "not-a-url", "ftp://idp.example.test", "https://idp.example.test?probe=1"} {
		row, found, err := lookup(t.Context(), workloadTestEndpoint(uuid.New()), issuerURL)

		require.ErrorIs(t, err, remotesessions.ErrIssuerURLInvalid, "%q is not an issuer identifier", issuerURL)
		require.False(t, found)
		require.Nil(t, row)
	}
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
// is looked up again rather than staying rejected.
func TestWorkloadIssuerMissCache_EntryExpires(t *testing.T) {
	t.Parallel()

	cache := newWorkloadIssuerMissCache(8, time.Millisecond)
	cache.remember("issuer")
	require.True(t, cache.seen("issuer"))

	require.Eventually(t, func() bool {
		return !cache.seen("issuer")
	}, time.Second, 5*time.Millisecond, "a miss must stop being remembered once its ttl lapses")

	require.Empty(t, cache.expiries, "a lapsed entry must be dropped, not merely ignored")
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
