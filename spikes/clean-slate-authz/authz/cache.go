package authz

import "sync"

// cacheKey covers every input to a snapshot. A snapshot is a pure function of
// (org, subject, layer kind, revision, schema hash), so a hit can never be
// stale: there is nothing to invalidate, only entries nobody asks for again.
type cacheKey struct {
	org      string
	subject  Subject
	kind     LayerKind
	revision int64
	schema   string
	facts    bool
	scopes   string
}

// Cache is an in-process memo. A nil *Cache disables caching.
type Cache struct {
	mu    sync.RWMutex
	snaps map[cacheKey]*Snapshot
	facts map[cacheKey]*OrgFacts
	max   int
}

func NewCache(max int) *Cache {
	return &Cache{snaps: map[cacheKey]*Snapshot{}, facts: map[cacheKey]*OrgFacts{}, max: max}
}

func (c *Cache) get(k cacheKey) (*Snapshot, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.snaps[k]
	return s, ok
}

func (c *Cache) put(k cacheKey, s *Snapshot) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.snaps) >= c.max {
		c.snaps = map[cacheKey]*Snapshot{} // spike: crude eviction; production would use an LRU + singleflight
	}
	c.snaps[k] = s
}

func (c *Cache) getFacts(k cacheKey) (*OrgFacts, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	f, ok := c.facts[k]
	return f, ok
}

func (c *Cache) putFacts(k cacheKey, f *OrgFacts) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.facts) >= c.max {
		c.facts = map[cacheKey]*OrgFacts{}
	}
	c.facts[k] = f
}
