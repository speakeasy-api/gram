package growthsignals

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/sync/singleflight"
)

const (
	// lookupTTL bounds how stale a resolved name may be. A burst of events from
	// one organization then costs one query rather than one per event, and a
	// renamed organization or project catches up within the window.
	lookupTTL = 5 * time.Minute

	// lookupCacheSize caps each cache so the long tail of organizations that
	// emit a single event cannot grow it without bound. It sits well above the
	// number of organizations active in any TTL window, so eviction is not part
	// of the steady state.
	lookupCacheSize = 4096
)

// lookupCache memoizes one kind of enrichment lookup.
//
// The value is cached whatever it holds, so an id that resolves to nothing is
// as cheap the second time as one that resolves to a name — the misses are what
// a firehose produces most of. Errors are never cached, so a database blip
// costs one failed event rather than a TTL of them.
type lookupCache[K comparable, V any] struct {
	entries *expirable.LRU[K, V]
	load    func(context.Context, K) (V, error)

	// inflight collapses concurrent misses for the same key into one load. A
	// burst of stream messages from one organization all miss together on the
	// first event, and without this each message issues its own query for the
	// same row.
	inflight singleflight.Group
}

func newLookupCache[K comparable, V any](load func(context.Context, K) (V, error)) *lookupCache[K, V] {
	return &lookupCache[K, V]{
		entries:  expirable.NewLRU[K, V](lookupCacheSize, nil, lookupTTL),
		load:     load,
		inflight: singleflight.Group{},
	}
}

// resolve returns the cached value for key, loading it on a miss.
func (c *lookupCache[K, V]) resolve(ctx context.Context, key K) (V, error) {
	if cached, ok := c.entries.Get(key); ok {
		return cached, nil
	}

	// The key is stringified for the flight group only; the cache itself stays
	// typed. Callers that arrive while a load is in progress wait for it rather
	// than issuing their own.
	flight := c.inflight.DoChan(fmt.Sprint(key), func() (any, error) {
		// Re-read inside the flight. A caller that missed the cache before an
		// earlier flight finished can still reach this point after that flight
		// was removed from the group, and without this it would issue a second
		// query for a row already cached.
		if cached, ok := c.entries.Get(key); ok {
			return cached, nil
		}

		value, err := c.load(ctx, key)
		if err != nil {
			return nil, err
		}

		c.entries.Add(key, value)

		return value, nil
	})

	var result singleflight.Result
	select {
	case <-ctx.Done():
		// Waiting on somebody else's query must not outlive this caller. The
		// flight continues for whoever else is waiting on it.
		var zero V
		return zero, fmt.Errorf("growth signal lookup for %v: %w", key, ctx.Err())
	case result = <-flight:
	}

	loaded, err := result.Val, result.Err
	if err != nil {
		var zero V
		return zero, err
	}

	value, ok := loaded.(V)
	if !ok {
		var zero V
		return zero, fmt.Errorf("growth signal cache loaded %T for key %v", loaded, key)
	}

	return value, nil
}
