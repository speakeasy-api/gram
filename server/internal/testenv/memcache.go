package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// memoryCache is a minimal in-memory cache.Cache for unit tests that need
// real storage semantics without a Redis container. Values round-trip
// through JSON like the Redis adapter's codec; TTLs are ignored.
type memoryCache struct {
	mu      sync.Mutex
	entries map[string][]byte
}

var _ cache.Cache = (*memoryCache)(nil)

// NewMemoryCache returns an empty in-memory cache.Cache.
func NewMemoryCache() cache.Cache {
	return &memoryCache{mu: sync.Mutex{}, entries: map[string][]byte{}}
}

func (m *memoryCache) Get(ctx context.Context, key string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.entries[key]
	if !ok {
		return errors.New("no cache entry for key")
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return errors.New("unmarshal cache entry: " + err.Error())
	}
	return nil
}

func (m *memoryCache) GetAndDelete(ctx context.Context, key string, value any) error {
	// Atomic lookup+removal under one lock, mirroring Redis GETDEL: exactly
	// one concurrent caller wins the entry, and the entry is consumed even
	// when it fails to decode.
	m.mu.Lock()
	raw, ok := m.entries[key]
	delete(m.entries, key)
	m.mu.Unlock()
	if !ok {
		return errors.New("no cache entry for key")
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return errors.New("unmarshal cache entry: " + err.Error())
	}
	return nil
}

func (m *memoryCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return errors.New("marshal cache entry: " + err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = raw
	return nil
}

func (m *memoryCache) Add(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[key]; ok {
		return false, nil
	}
	m.entries[key] = []byte("1")
	return true, nil
}

func (m *memoryCache) Update(ctx context.Context, key string, value any) error {
	return m.Set(ctx, key, value, 0)
}

func (m *memoryCache) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

func (m *memoryCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return nil
}

func (m *memoryCache) ListAppend(ctx context.Context, key string, value any, ttl time.Duration) error {
	return errors.New("unimplemented")
}

func (m *memoryCache) ListRange(ctx context.Context, key string, start, stop int64, value any) error {
	return errors.New("unimplemented")
}

func (m *memoryCache) DeleteByPrefix(ctx context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.entries {
		if strings.HasPrefix(key, prefix) {
			delete(m.entries, key)
		}
	}
	return nil
}
