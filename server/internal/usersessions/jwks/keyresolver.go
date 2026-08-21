package jwks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-jose/go-jose/v4"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

// refreshCooldown is the per-replica negative-cache window on the unknown-kid
// path: a source whose upstream was consulted this recently is not consulted
// again, whatever kid arrives. It is what makes probing with random kids
// cost nothing after the first refresh — without it, every probe would reach
// the rate limiter (a Redis round trip) and drain the source's refresh
// budget, delaying the legitimate rotation the budget exists to serve. The
// value bounds how long a genuine just-rotated key can be refused in the
// worst ordering, so it stays small.
const refreshCooldown = 30 * time.Second

// ErrRefreshRateLimited reports an assertion kid that is not in the cached
// key set and cannot be checked upstream because the source's refresh budget
// is exhausted. Callers should treat it exactly like ErrKeyNotFound on the
// wire (the distinction is an operational signal, not a client-facing one).
var ErrRefreshRateLimited = errors.New("key set refresh rate limited")

// KeyResolver is the orchestrator request paths use to turn (source, kid)
// into a verification key. It binds the stateless Resolver to a storage
// policy and — critically — to the per-source refresh rate limiter, and its
// VerificationKey method is the only place the unknown-kid refetch loop
// exists. Unauthenticated surfaces must reach key material through this type
// and never through a hand-rolled loop over Resolver.Resolve, which carries
// no rate limit.
//
// Failure posture is fail-closed throughout, which is the correct posture
// for its consumer (client assertion verification, where an unresolvable key
// set blocks exactly one client): resolver errors, cache errors, and rate
// limiter errors all fail the resolution. In particular a rate limiter store
// outage fails closed — rotation is delayed rather than the refresh path
// degrading into an unthrottled fetch amplifier.
type KeyResolver struct {
	resolver *Resolver
	cache    Cache
	limiter  *ratelimit.Limiter
	logger   *slog.Logger
	group    singleflight.Group
}

// NewKeyResolver binds a Resolver to its storage and refresh-limit policy.
// Every dependency is required; a nil limiter in particular is refused
// rather than defaulted, because forgetting it is precisely the mis-assembly
// this constructor exists to catch.
//
// The limiter is charged once per forced (unknown-kid) refresh, keyed by the
// source's CacheKey, and its budget is shared fleet-wide while caches are
// per-replica: after a real key rotation, each replica needs one forced
// refresh to converge, so size the limiter burst at or above the replica
// count or rotation propagates one replica per refill.
func NewKeyResolver(resolver *Resolver, cache Cache, limiter *ratelimit.Limiter, logger *slog.Logger) (*KeyResolver, error) {
	if resolver == nil {
		return nil, errors.New("jwks: KeyResolver requires a Resolver")
	}
	if cache == nil {
		return nil, errors.New("jwks: KeyResolver requires a Cache")
	}
	if limiter == nil {
		return nil, errors.New("jwks: KeyResolver requires a refresh rate limiter")
	}
	if logger == nil {
		return nil, errors.New("jwks: KeyResolver requires a logger")
	}
	return &KeyResolver{
		resolver: resolver,
		cache:    cache,
		limiter:  limiter,
		logger:   logger.With(attr.SlogComponent("jwks")),
		group:    singleflight.Group{},
	}, nil
}

// VerificationKey returns the verification key an assertion's kid names,
// consulting the upstream key source as the refresh policy allows.
//
// The rotation contract: key rotation upstream is unpredictable and is
// signalled only by an assertion arriving with an unknown kid. When that
// happens against a set served from storage, one rate-limited forced refresh
// runs and the selection is retried against the fresh set. When it happens
// against a set the upstream was consulted for within refreshCooldown —
// including the refresh this very call performed — the answer is current and
// ErrKeyNotFound is terminal, with no further fetch and no limiter charge.
// Concurrent callers coalesce: a burst of assertions bearing a freshly
// rotated kid costs one refresh, not one per request.
func (k *KeyResolver) VerificationKey(ctx context.Context, source Source, kid string) (*jose.JSONWebKey, error) {
	if source.kind == sourceInline {
		// Inline sets have no upstream to refresh from, so an unknown kid is
		// terminal by construction and no rate limit applies.
		result, err := k.resolver.Resolve(ctx, source, CacheState{Document: nil, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}})
		if err != nil {
			return nil, err
		}
		return selectKey(result.KeySet, kid)
	}

	result, err := k.resolveShared(ctx, source)
	if err != nil {
		return nil, err
	}

	key, err := selectKey(result.KeySet, kid)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return nil, err
	}

	// Unknown kid. If this resolution already consulted the upstream (a
	// fresh fetch or a 304 confirmation), the answer is current and
	// terminal; the result was stored, so follow-up probes fail from cache.
	if result.Outcome != CacheOutcomeCached {
		return nil, err
	}

	refreshed, err := k.refreshShared(ctx, source)
	if err != nil {
		return nil, err
	}
	return selectKey(refreshed.KeySet, kid)
}

// resolveShared runs one cache-honouring resolution for a source, coalescing
// concurrent callers so a cold or expired entry costs a single upstream
// fetch however many verifications race on it. The cache is read inside the
// flight so every waiter shares the freshest stored state.
//
// singleflight runs the function under the first caller's context; a
// coalesced waiter can therefore fail from a cancellation it did not cause.
// fetchTimeout bounds the damage, and the next call simply starts a new
// flight.
func (k *KeyResolver) resolveShared(ctx context.Context, source Source) (*Result, error) {
	result, err, _ := k.group.Do("resolve\x00"+source.CacheKey(), func() (any, error) {
		state, err := k.cache.Get(ctx, source.CacheKey())
		if err != nil {
			return nil, fmt.Errorf("read key set cache: %w", err)
		}
		resolved, err := k.resolver.Resolve(ctx, source, state)
		if err != nil {
			return nil, err
		}
		k.store(ctx, source, state, resolved)
		return resolved, nil
	})
	if err != nil {
		return nil, err
	}
	resolved, ok := result.(*Result)
	if !ok {
		return nil, fmt.Errorf("unexpected resolve flight result type %T", result)
	}
	return resolved, nil
}

// refreshShared runs the forced refresh behind the unknown-kid path,
// coalescing concurrent callers into one limiter charge and one fetch.
func (k *KeyResolver) refreshShared(ctx context.Context, source Source) (*Result, error) {
	result, err, _ := k.group.Do("refresh\x00"+source.CacheKey(), func() (any, error) {
		state, err := k.cache.Get(ctx, source.CacheKey())
		if err != nil {
			return nil, fmt.Errorf("read key set cache: %w", err)
		}

		// The negative cache: an upstream consulted within the cooldown gave
		// a current answer, so serve the stored set for re-selection instead
		// of spending refresh budget. This is what a completed concurrent
		// refresh (in this process or another replica sharing a durable
		// cache) looks like from here, and also what an attacker probing
		// random kids sees after their first refresh.
		if !state.RefreshedAt.IsZero() && time.Since(state.RefreshedAt) < refreshCooldown {
			if keys, parseErr := parseKeySet(state.Document); parseErr == nil {
				return &Result{
					Outcome:  CacheOutcomeCached,
					KeySet:   keys,
					Document: state.Document,
					ETag:     state.ETag,
					TTL:      0,
				}, nil
			}
			// A stored document that no longer screens clean is not worth
			// protecting; fall through to the limiter and refresh.
		}

		allowed, err := k.limiter.Allow(ctx, source.CacheKey())
		if err != nil {
			// A limiter store outage fails closed: rotation waits rather
			// than the refresh path running unthrottled.
			return nil, fmt.Errorf("key set refresh rate limiter: %w", err)
		}
		if !allowed.Allowed {
			k.logger.WarnContext(ctx, "jwks forced refresh denied by rate limiter",
				attr.SlogURLFull(source.uri),
				attr.SlogJWKSOrigin(source.origin),
			)
			return nil, fmt.Errorf("retry after %s: %w", allowed.RetryAfter, ErrRefreshRateLimited)
		}

		// Freshness is stripped so Resolve must consult the upstream; the
		// document and validator are kept so the consult can be a cheap 304
		// revalidation — which still proves the kid is genuinely unknown.
		forced := CacheState{
			Document:    state.Document,
			ETag:        state.ETag,
			ExpiresAt:   time.Time{},
			RefreshedAt: state.RefreshedAt,
		}
		resolved, err := k.resolver.Resolve(ctx, source, forced)
		if err != nil {
			// A failed consult still stamps the cooldown: the upstream was
			// genuinely contacted, and without the marker every unknown-kid
			// probe against an unreachable source would charge the limiter
			// and retry the origin instead of being negative-cached like a
			// successful consult. Everything else about the stored state is
			// kept as it was.
			k.markConsultFailure(ctx, source, state)
			return nil, err
		}
		k.store(ctx, source, state, resolved)
		return resolved, nil
	})
	if err != nil {
		return nil, err
	}
	resolved, ok := result.(*Result)
	if !ok {
		return nil, fmt.Errorf("unexpected refresh flight result type %T", result)
	}
	return resolved, nil
}

// store persists a resolution that consulted the upstream. A Put failure is
// logged rather than propagated: the resolution in hand is valid and the
// verification it serves should not fail because storage hiccuped — the cost
// of the miss is a future refetch, which the limiter and cooldown already
// bound.
//
// prior is the stored state the resolution started from. The resolve and
// refresh flights do not coalesce with each other, so a slow fetch can
// complete after a concurrent flight already stored a fresher (possibly
// post-rotation) document; re-reading the cache and skipping the write when
// its RefreshedAt has moved past prior's keeps the newer result. The check
// is read-then-write rather than atomic — the Cache interface has no
// compare-and-swap — but it shrinks the overwrite window from a full fetch
// (up to fetchTimeout) to the gap between these two calls.
func (k *KeyResolver) store(ctx context.Context, source Source, prior CacheState, result *Result) {
	if result.Outcome != CacheOutcomeRefreshed && result.Outcome != CacheOutcomeNotModified {
		return
	}
	if current, err := k.cache.Get(ctx, source.CacheKey()); err == nil && current.RefreshedAt.After(prior.RefreshedAt) {
		return
	}
	state := CacheState{
		Document:    result.Document,
		ETag:        result.ETag,
		ExpiresAt:   time.Now().Add(result.TTL),
		RefreshedAt: time.Now(),
	}
	if err := k.cache.Put(ctx, source.CacheKey(), state); err != nil {
		k.logger.WarnContext(ctx, "jwks key set cache write failed",
			attr.SlogURLFull(source.uri),
			attr.SlogJWKSOrigin(source.origin),
			attr.SlogError(err),
		)
	}
}

// markConsultFailure re-stamps RefreshedAt on the stored state after a forced
// refresh whose upstream consult failed, so the cooldown applies to failures
// the same as to successes. The write failure policy matches store: log only.
func (k *KeyResolver) markConsultFailure(ctx context.Context, source Source, prior CacheState) {
	marked := CacheState{
		Document:    prior.Document,
		ETag:        prior.ETag,
		ExpiresAt:   prior.ExpiresAt,
		RefreshedAt: time.Now(),
	}
	if err := k.cache.Put(ctx, source.CacheKey(), marked); err != nil {
		k.logger.WarnContext(ctx, "jwks consult-failure cooldown write failed",
			attr.SlogURLFull(source.uri),
			attr.SlogJWKSOrigin(source.origin),
			attr.SlogError(err),
		)
	}
}
