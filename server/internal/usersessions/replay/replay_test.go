package replay

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The core property: an identifier is claimable exactly once.
func TestReserve_SecondPresentationRejected(t *testing.T) {
	t.Parallel()

	guard, _ := newTestGuard(t, time.Hour)
	key := testKey(t, "jti-once")

	claimed, err := guard.Reserve(t.Context(), key, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed, "the first presentation of an identifier must be accepted")

	claimed, err = guard.Reserve(t.Context(), key, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.False(t, claimed, "a replayed identifier must be rejected")
}

// The single-use guarantee has to hold under simultaneous presentation, not
// just serial: many callers racing on one identifier must see exactly one
// winner. SET NX is atomic in Redis, and this is what pins that the guard
// relies on it rather than on a read-then-write.
func TestReserve_ConcurrentPresentationsOneWinner(t *testing.T) {
	t.Parallel()

	guard, _ := newTestGuard(t, time.Hour)
	key := testKey(t, "jti-contended")
	const callers = 32

	start := make(chan struct{})
	results := make(chan bool, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			<-start
			claimed, err := guard.Reserve(t.Context(), key, time.Now().Add(time.Minute))
			require.NoError(t, err)
			results <- claimed
		})
	}
	close(start)
	wg.Wait()
	close(results)

	winners := 0
	for claimed := range results {
		if claimed {
			winners++
		}
	}
	require.Equal(t, 1, winners, "exactly one of %d simultaneous presentations may win", callers)
}

// Distinct identifiers do not interfere, so a client making concurrent
// requests is not rate limited by its own earlier assertions.
func TestReserve_DistinctIdentifiersIndependent(t *testing.T) {
	t.Parallel()

	guard, _ := newTestGuard(t, time.Hour)

	for _, id := range []string{"jti-a", "jti-b", "jti-c"} {
		claimed, err := guard.Reserve(t.Context(), testKey(t, id), time.Now().Add(time.Minute))
		require.NoError(t, err)
		require.True(t, claimed, "identifier %q must be claimable independently", id)
	}
}

// The same jti presented for two different clients is two different
// reservations: identifiers are only unique within their issuing party, and
// nothing stops two clients from choosing the same value.
func TestReserve_ScopedByClient(t *testing.T) {
	t.Parallel()

	guard, _ := newTestGuard(t, time.Hour)

	first := Key{Issuer: t.Name(), Party: "client-one", ID: "shared-jti"}
	second := Key{Issuer: t.Name(), Party: "client-two", ID: "shared-jti"}

	claimed, err := guard.Reserve(t.Context(), first, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = guard.Reserve(t.Context(), second, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed, "another client's identical jti must not be treated as a replay")
}

// The same jti presented to two different authorization servers is likewise
// two reservations, so one tenant's assertion volume cannot evict another's.
func TestReserve_ScopedByIssuer(t *testing.T) {
	t.Parallel()

	guard, _ := newTestGuard(t, time.Hour)

	first := Key{Issuer: t.Name() + "-issuer-one", Party: "client", ID: "shared-jti"}
	second := Key{Issuer: t.Name() + "-issuer-two", Party: "client", ID: "shared-jti"}

	claimed, err := guard.Reserve(t.Context(), first, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = guard.Reserve(t.Context(), second, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed, "another issuer's identical jti must not be treated as a replay")
}

// Length-prefixed hashing keeps the parts distinguishable. Without it these
// two keys would encode identically and one client could burn the other's
// identifiers.
func TestKey_PunctuationCannotCollide(t *testing.T) {
	t.Parallel()

	split := Key{Issuer: "iss", Party: "a:b", ID: "c"}
	shifted := Key{Issuer: "iss", Party: "a", ID: "b:c"}

	require.NotEqual(t, split.storageKey(), shifted.storageKey())
}

// The same property across the Party/Subject boundary. This is why Subject is
// a part of its own rather than something a caller folds into Party: a
// workload's external subject is attacker-influenced and colon-heavy — a
// GitHub Actions sub reads `repo:org/name:ref:refs/heads/main` — so a caller
// concatenating the two would reintroduce exactly the collision the length
// prefixes exist to close.
func TestKey_SubjectCannotCollideWithParty(t *testing.T) {
	t.Parallel()

	split := Key{Issuer: "iss", Party: "a:b", Subject: "c", ID: "jti"}
	shifted := Key{Issuer: "iss", Party: "a", Subject: "b:c", ID: "jti"}

	require.NotEqual(t, split.storageKey(), shifted.storageKey())
}

// A key with no Subject must hash exactly as it did before Subject existed.
// Replicas running different builds share this keyspace during a rolling
// deploy, so moving client-assertion keys would orphan every hold already
// taken and let a spent assertion be spent again against a newer replica.
// The legacy encoding is spelled out here rather than referenced, so a change
// to storageKey has to confront it.
func TestKey_EmptySubjectPreservesLegacyEncoding(t *testing.T) {
	t.Parallel()

	sum := sha256.New()
	for _, part := range []string{"iss", "client", "jti"} {
		sum.Write([]byte(strconv.Itoa(len(part))))
		sum.Write([]byte(":"))
		sum.Write([]byte(part))
	}
	legacy := base64.RawURLEncoding.EncodeToString(sum.Sum(nil))

	got := Key{Issuer: "iss", Party: "client", Subject: "", ID: "jti"}.storageKey()
	require.Equal(t, legacy, got, "client assertion keys must not move")
}

// Two workloads vouched for by one issuer must not share a keyspace. A jti is
// unique per issuer, never per workload, so without Subject in the key the
// first workload to present an identifier would burn it for the second.
func TestReserve_ScopedBySubject(t *testing.T) {
	t.Parallel()

	guard, _ := newTestGuard(t, time.Hour)

	first := Key{Issuer: t.Name(), Party: "issuer-ref", Subject: "workload-one", ID: "shared-jti"}
	second := Key{Issuer: t.Name(), Party: "issuer-ref", Subject: "workload-two", ID: "shared-jti"}

	claimed, err := guard.Reserve(t.Context(), first, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = guard.Reserve(t.Context(), second, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed, "another workload's identical jti must not be treated as a replay")
}

// An empty Subject is legitimate — a client assertion has no second identity
// to name — so it must not be refused the way the required parts are.
func TestReserve_EmptySubjectAccepted(t *testing.T) {
	t.Parallel()

	guard, _ := newTestGuard(t, time.Hour)

	claimed, err := guard.Reserve(t.Context(), Key{Issuer: t.Name(), Party: "client", Subject: "", ID: "jti"}, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)
}

// A hold window that has already passed must still produce a real expiry.
// Redis reads a zero expiration as "no expiry", so an unclamped lifetime here
// would leave a permanent key named after an attacker-supplied identifier.
func TestReserve_ElapsedHoldStillExpires(t *testing.T) {
	t.Parallel()

	guard, client := newTestGuard(t, time.Hour)
	key := testKey(t, "jti-already-elapsed")

	claimed, err := guard.Reserve(t.Context(), key, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.True(t, claimed)

	ttl, err := client.TTL(t.Context(), guard.namespace+":"+key.storageKey()).Result()
	require.NoError(t, err)
	require.Positive(t, ttl, "an elapsed hold window must still set an expiry, never persist forever")
	require.LessOrEqual(t, ttl, minHoldTTL)
}

// A hold window beyond the guard's cap is truncated to it, so a client cannot
// pin keyspace for longer than the consumer's policy allows.
func TestReserve_HoldClampedToMax(t *testing.T) {
	t.Parallel()

	const maxHold = 2 * time.Minute
	guard, client := newTestGuard(t, maxHold)
	key := testKey(t, "jti-overlong-hold")

	claimed, err := guard.Reserve(t.Context(), key, time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	require.True(t, claimed)

	ttl, err := client.TTL(t.Context(), guard.namespace+":"+key.storageKey()).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, maxHold, "a hold longer than the cap must be truncated to it")
}

// A guard with no store cannot answer truthfully, so it is refused at
// construction rather than degrading into one that reports every caller as
// the first.
func TestNewRedisGuard_RequiresStore(t *testing.T) {
	t.Parallel()

	_, err := NewRedisGuard(nil, "ns", time.Hour)
	require.Error(t, err)
}

func TestNewRedisGuard_RejectsUnusableConfiguration(t *testing.T) {
	t.Parallel()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	_, err = NewRedisGuard(client, "", time.Hour)
	require.Error(t, err, "an empty namespace would share a keyspace with every other consumer")

	_, err = NewRedisGuard(client, "ns", 0)
	require.Error(t, err, "a zero cap would clamp every reservation below its floor")
}

// An incomplete Key is a caller bug, and accepting one would put every
// affected assertion on a single shared storage key.
func TestReserve_RejectsIncompleteKey(t *testing.T) {
	t.Parallel()

	guard, _ := newTestGuard(t, time.Hour)

	_, err := guard.Reserve(t.Context(), Key{Issuer: "iss", Party: "client", ID: ""}, time.Now().Add(time.Minute))
	require.Error(t, err, "an assertion with no jti must not be reservable")

	_, err = guard.Reserve(t.Context(), Key{Issuer: "", Party: "client", ID: "jti"}, time.Now().Add(time.Minute))
	require.Error(t, err)

	_, err = guard.Reserve(t.Context(), Key{Issuer: "iss", Party: "", ID: "jti"}, time.Now().Add(time.Minute))
	require.Error(t, err)
}
