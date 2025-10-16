package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type Suffix string

const (
	SuffixNone Suffix = ""
)

// Cache defines a generic interface for cache operations.
// Implementations can use any underlying storage (Redis, in-memory, etc.)
type Cache interface {
	Get(ctx context.Context, key string, value any) error
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Update(ctx context.Context, key string, value any) error
	Delete(ctx context.Context, key string) error
}

type TypedCacheObject[T CacheableObject[T]] struct {
	logger    *slog.Logger
	cache     Cache
	keySuffix string
}

func NewTypedObjectCache[T CacheableObject[T]](logger *slog.Logger, cache Cache, suffix Suffix) TypedCacheObject[T] {
	return TypedCacheObject[T]{logger: logger, cache: cache, keySuffix: string(suffix)}
}

type CacheableObject[T any] interface {
	CacheKey() string
	AdditionalCacheKeys() []string
	TTL() time.Duration
}

func (d *TypedCacheObject[T]) fullKey(key string) string {
	return key + ":" + d.keySuffix
}

func (d *TypedCacheObject[T]) Delete(ctx context.Context, obj T) error {
	if d.cache == nil {
		return nil
	}

	cacheKey := d.fullKey(obj.CacheKey())
	d.logger.DebugContext(ctx, "invalidating cache", attr.SlogCacheKey(cacheKey))
	err := d.cache.Delete(ctx, cacheKey)
	if err != nil {
		return fmt.Errorf("delete: %s: %w", cacheKey, err)
	}

	for _, key := range obj.AdditionalCacheKeys() {
		err := d.cache.Delete(ctx, d.fullKey(key))
		if err != nil {
			return fmt.Errorf("delete additional: %s: %w", d.fullKey(key), err)
		}
	}

	return nil
}

func (d *TypedCacheObject[T]) DeleteByKey(ctx context.Context, key string) error {
	if d.cache == nil {
		return nil
	}

	cacheKey := d.fullKey(key)
	d.logger.DebugContext(ctx, "invalidating cache by key", attr.SlogCacheKey(cacheKey))
	err := d.cache.Delete(ctx, cacheKey)
	if err != nil {
		return fmt.Errorf("delete by key: %s: %w", cacheKey, err)
	}

	return nil
}

func (d *TypedCacheObject[T]) Get(ctx context.Context, key string) (T, error) {
	if d.cache == nil {
		return *new(T), errors.New("cache is not configured")
	}
	var value T
	err := d.cache.Get(ctx, d.fullKey(key), &value)
	if err != nil {
		return *new(T), fmt.Errorf("%s: get: %w", d.fullKey(key), err)
	}
	return value, nil
}

func (d *TypedCacheObject[T]) Store(ctx context.Context, obj T) error {
	if d.cache == nil {
		return errors.New("cache is not configured")
	}

	ttl := obj.TTL()
	if err := d.cache.Set(ctx, d.fullKey(obj.CacheKey()), obj, ttl); err != nil {
		return fmt.Errorf("store: %s: %w", d.fullKey(obj.CacheKey()), err)
	}
	for _, key := range obj.AdditionalCacheKeys() {
		if err := d.cache.Set(ctx, d.fullKey(key), obj, ttl); err != nil {
			return fmt.Errorf("store additional: %s: %w", d.fullKey(key), err)
		}
	}
	return nil
}

func (d *TypedCacheObject[T]) Update(ctx context.Context, obj T) error {
	if d.cache == nil {
		return errors.New("cache is not configured")
	}

	updateKey := func(key string) error {
		fullKey := d.fullKey(key)
		if err := d.cache.Update(ctx, fullKey, obj); err != nil {
			return fmt.Errorf("failed to update key %s: %w", fullKey, err)
		}
		return nil
	}
	if err := updateKey(obj.CacheKey()); err != nil {
		return err
	}
	for _, key := range obj.AdditionalCacheKeys() {
		if err := updateKey(key); err != nil {
			return err
		}
	}
	return nil
}
