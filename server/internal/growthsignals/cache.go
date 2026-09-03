package growthsignals

import (
	"context"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
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
}

func newLookupCache[K comparable, V any](load func(context.Context, K) (V, error)) *lookupCache[K, V] {
	return &lookupCache[K, V]{
		entries: expirable.NewLRU[K, V](lookupCacheSize, nil, lookupTTL),
		load:    load,
	}
}

// resolve returns the cached value for key, loading it on a miss.
func (c *lookupCache[K, V]) resolve(ctx context.Context, key K) (V, error) {
	if cached, ok := c.entries.Get(key); ok {
		return cached, nil
	}

	value, err := c.load(ctx, key)
	if err != nil {
		var zero V
		return zero, err
	}

	c.entries.Add(key, value)

	return value, nil
}
