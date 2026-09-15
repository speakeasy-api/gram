package replay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// minHoldTTL is the floor applied to every reservation's lifetime.
//
// It exists because the underlying Redis SET NX treats a zero expiration as
// "no expiry at all": a hold window that rounded down to zero, or that a clock
// skew allowance pushed into the past, would otherwise create a permanent key
// named after an attacker-supplied identifier. One second is long enough to be
// a real hold and short enough to be harmless when it is reached.
const minHoldTTL = time.Second

// Guard records assertion identifiers and reports whether each is new.
// Safe for concurrent use; construct one per logical keyspace at wiring time.
type Guard struct {
	// cache is the store reservations are made in. Only its set-if-absent
	// operation is used, and only against a backend whose answer is
	// authoritative.
	cache cache.Cache

	// namespace prefixes every stored key, so consumers of this package
	// never share an entry.
	namespace string

	// maxHold caps the lifetime of any single reservation, bounding the
	// keyspace one assertion-issuing party can occupy.
	maxHold time.Duration
}

// NewRedisGuard binds a Redis-backed Guard to a keyspace.
//
// namespace prefixes every stored key so unrelated consumers of this package
// never share an entry. maxHold caps how long any single reservation is kept,
// bounding the keyspace an assertion-issuing client can occupy; pass the
// caller's own maximum assertion lifetime plus whatever clock skew it
// tolerates, since a reservation released while the assertion is still
// acceptable would let it be replayed.
//
// A nil client is refused rather than defaulted. Reserve cannot report a
// truthful verdict without a store, and the shared no-op cache answers
// set-if-absent by telling every caller it won, which for a replay guard is
// the one wrong answer.
func NewRedisGuard(client *redis.Client, namespace string, maxHold time.Duration) (*Guard, error) {
	if client == nil {
		return nil, errors.New("replay: Guard requires a Redis client")
	}
	if namespace == "" {
		return nil, errors.New("replay: Guard requires a namespace")
	}
	if maxHold < minHoldTTL {
		return nil, fmt.Errorf("replay: maxHold must be at least %s", minHoldTTL)
	}
	return &Guard{
		cache:     cache.NewRedisCacheAdapter(client),
		namespace: namespace,
		maxHold:   maxHold,
	}, nil
}

// MaxHold is the longest this guard will keep any reservation. A consumer
// whose assertions can stay acceptable for longer than this must not use the
// guard: the reservation would lapse while the assertion still verifies.
// Consumers should check it once at wiring time rather than per call.
func (g *Guard) MaxHold() time.Duration {
	return g.maxHold
}

// Reserve claims key until holdUntil and reports whether this call was the
// first to do so. A false return means the identifier has already been
// presented and the assertion carrying it must be rejected.
//
// holdUntil is the instant after which the assertion can no longer be
// presented successfully, which is its expiry plus any clock skew the caller
// tolerates, not the bare expiry: releasing the identifier while the assertion
// would still verify is what a replay needs. The resulting lifetime is clamped
// to [minHoldTTL, maxHold].
//
// An error means the store could not be consulted. The identifier's status is
// then unknown, and an unknown identifier must be treated as a replay.
func (g *Guard) Reserve(ctx context.Context, key Key, holdUntil time.Time) (bool, error) {
	if key.Issuer == "" || key.Party == "" || key.ID == "" {
		// An assertion with no jti reaches here only through a caller that
		// failed to require one, and an empty part would put every such
		// assertion on the same storage key. Subject is exempt: a client
		// assertion legitimately leaves it empty, because Party already
		// names the whole minting party.
		return false, errors.New("replay: Key needs an Issuer, a Party, and an ID")
	}

	ttl := min(max(time.Until(holdUntil), minHoldTTL), g.maxHold)

	claimed, err := g.cache.Add(ctx, g.namespace+":"+key.storageKey(), ttl)
	if err != nil {
		return false, fmt.Errorf("reserve assertion id: %w", err)
	}
	return claimed, nil
}
