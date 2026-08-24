package jwks

import (
	"context"
	"sync"
	"time"
)

// Cache is the pluggable storage policy behind a KeyResolver. The ephemeral
// in-process MemoryCache serves client assertion verification; a durable
// row-backed implementation (trusted issuer key sets persisted on their
// configuration rows) satisfies the same interface. Implementations must be
// safe for concurrent use, and callers must not mutate a returned Document.
type Cache interface {
	// Get returns the stored state for a source key; the zero CacheState
	// means nothing is cached. An error means the backing store itself
	// failed and is propagated to the caller rather than treated as a miss —
	// a store outage that silently converted every lookup into an upstream
	// fetch would be an availability-driven amplifier.
	Get(ctx context.Context, key string) (CacheState, error)

	// Put replaces the stored state for a source key.
	Put(ctx context.Context, key string, state CacheState) error
}

const (
	// defaultMemoryCacheEntries bounds MemoryCache so key-source churn over
	// a process lifetime cannot accumulate entries forever.
	defaultMemoryCacheEntries = 1024

	// defaultMemoryCacheBytes bounds the summed Document bytes held by a
	// MemoryCache. The entry cap alone is not enough: each document may run
	// up to maxKeySetBytes, so a count-only bound would let a party able to
	// register many key sources pin hundreds of megabytes of heap per
	// replica by serving maximum-size documents.
	defaultMemoryCacheBytes = 32 * 1024 * 1024
)

// MemoryCache is the ephemeral per-replica Cache: a map guarded by an
// RWMutex. Per-replica storage is adequate here because the security
// property of the refresh path lives in the fleet-wide rate limiter, not the
// cache — a cold replica costs one extra fetch per source, nothing more.
type MemoryCache struct {
	mu         sync.RWMutex
	entries    map[string]CacheState
	maxEntries int
	maxBytes   int
	totalBytes int
}

// NewMemoryCache returns an empty MemoryCache bounded at
// defaultMemoryCacheEntries entries and defaultMemoryCacheBytes of summed
// document bytes.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		mu:         sync.RWMutex{},
		entries:    make(map[string]CacheState),
		maxEntries: defaultMemoryCacheEntries,
		maxBytes:   defaultMemoryCacheBytes,
		totalBytes: 0,
	}
}

var _ Cache = (*MemoryCache)(nil)

// Get returns the stored state for key, or the zero CacheState when nothing
// is stored. Expired entries are still returned: the resolver wants their
// Document and ETag for a conditional refresh, and freshness is its
// decision, not the cache's.
func (c *MemoryCache) Get(_ context.Context, key string) (CacheState, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.entries[key], nil
}

// Put stores state under key. Over either budget (entry count or summed
// document bytes), entries closest to expiry are evicted first — expired
// entries naturally lead that ordering, so live entries only fall out when
// the cache is genuinely full of fresher ones. The just-written key is never
// evicted, so a Put always lands. The linear scans are fine at this write
// rate: a Put happens at most once per source per TTL.
func (c *MemoryCache) Put(_ context.Context, key string, state CacheState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.entries[key]; ok {
		c.totalBytes -= len(existing.Document)
	}
	c.entries[key] = state
	c.totalBytes += len(state.Document)

	for len(c.entries) > c.maxEntries || c.totalBytes > c.maxBytes {
		evict := ""
		var earliest time.Time
		for k, v := range c.entries {
			if k == key {
				continue
			}
			if evict == "" || v.ExpiresAt.Before(earliest) {
				evict = k
				earliest = v.ExpiresAt
			}
		}
		if evict == "" {
			break
		}
		c.totalBytes -= len(c.entries[evict].Document)
		delete(c.entries, evict)
	}
	return nil
}
