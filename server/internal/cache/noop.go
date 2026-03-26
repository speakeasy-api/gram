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

// ListAppend implements [Cache].
func (s *noopCache) ListAppend(ctx context.Context, key string, value any, ttl time.Duration) error {
	return nil
}

// ListRange implements [Cache].
func (s *noopCache) ListRange(ctx context.Context, key string, start int64, stop int64, value any) error {
	return nil
}

// Set implements [Cache].
func (s *noopCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	return nil
}

// Update implements [Cache].
func (s *noopCache) Update(ctx context.Context, key string, value any) error {
	return nil
}
