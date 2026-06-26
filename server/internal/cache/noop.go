package cache

import (
	"context"
	"errors"
	"time"
)

var NoopCache = &noopCache{}

type noopCache struct{}

var _ Cache = (*noopCache)(nil)

// Delete implements [Cache].
func (s *noopCache) Delete(ctx context.Context, key string) error {
	return nil
}

// DeleteByPrefix implements [Cache].
func (s *noopCache) DeleteByPrefix(ctx context.Context, prefix string) error {
	return nil
}

// Get implements [Cache].
func (s *noopCache) Get(ctx context.Context, key string, value any) error {
	return errors.New("no cache entry for key")
}

// GetAndDelete implements [Cache].
func (s *noopCache) GetAndDelete(ctx context.Context, key string, value any) error {
	return errors.New("no cache entry for key")
}

// ListAppend implements [Cache].
func (s *noopCache) ListAppend(ctx context.Context, key string, value any, ttl time.Duration) error {
	return nil
}

// ListRange implements [Cache].
func (s *noopCache) ListRange(ctx context.Context, key string, start int64, stop int64, value any) error {
	return nil
}

// ListDrain implements [Cache].
func (s *noopCache) ListDrain(ctx context.Context, key string, value any) error {
	return nil
}

// Set implements [Cache].
func (s *noopCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	return nil
}

// Add implements [Cache]. With no backing store there is nothing to dedupe
// against, so every caller "wins" and persistence proceeds — dedup degrades
// to a no-op rather than silently dropping every write.
func (s *noopCache) Add(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return true, nil
}

// Expire implements [Cache].
func (s *noopCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return nil
}

// Update implements [Cache].
func (s *noopCache) Update(ctx context.Context, key string, value any) error {
	return nil
}
