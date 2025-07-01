package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
)

type Suffix string

const (
	SuffixNone Suffix = ""
)

// Cacheable - when implementing this, make sure all data fields you want stored in the cache are exported (capitalized).
type Cacheable[T any] interface {
	CacheKey() string
	AdditionalCacheKeys() []string
}
type CustomTTL interface {
	TTL() time.Duration
}

type Cache[T Cacheable[T]] struct {
	logger     *slog.Logger
	rdb        *redis.Client
	cache      *redisCache.Cache
	keySuffix  string
	defaultTTL time.Duration
}

func New[T Cacheable[T]](logger *slog.Logger, redisClient *redis.Client, ttl time.Duration, suffix Suffix) Cache[T] {
	cache := redisCache.New(&redisCache.Options{
		Redis: redisClient,
	})

	return Cache[T]{logger: logger, rdb: redisClient, cache: cache, defaultTTL: ttl, keySuffix: string(suffix)}
}

func (d *Cache[T]) fullKey(key string) string {
	return key + ":" + d.keySuffix
}

func (d *Cache[T]) Delete(ctx context.Context, obj T) error {
	if d.cache == nil {
		return nil
	}

	cacheKey := d.fullKey(obj.CacheKey())
	d.logger.DebugContext(ctx, "invalidating cache", slog.String("key", cacheKey))
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

func (d *Cache[T]) Get(context context.Context, key string) (T, error) { //nolint:nolintlint,ireturn
	if d.cache == nil {
		return *new(T), redisCache.ErrCacheMiss
	}

	var returnObj T

	err := d.cache.Get(context, d.fullKey(key), &returnObj)
	if err != nil {
		return returnObj, fmt.Errorf("%s: get: %w", d.fullKey(key), err)
	}

	return returnObj, nil
}

func (d *Cache[T]) Store(context context.Context, obj T) error {
	if d.cache == nil {
		return errors.New("cache is not configured")
	}

	// Check if the object implements a custom TTL function
	ttl := d.defaultTTL
	if objectSpecificTTL, ok := any(obj).(CustomTTL); ok {
		if customTTL := objectSpecificTTL.TTL(); customTTL > 0 {
			ttl = customTTL
		}
	}

	err := d.cache.Set(&redisCache.Item{
		Ctx:   context,
		Key:   d.fullKey(obj.CacheKey()),
		Value: obj,
		TTL:   ttl,
	})
	if err != nil {
		return fmt.Errorf("store: %s: %w", d.fullKey(obj.CacheKey()), err)
	}
	for _, key := range obj.AdditionalCacheKeys() {
		err := d.cache.Set(&redisCache.Item{
			Ctx:   context,
			Key:   d.fullKey(key),
			Value: obj,
			TTL:   ttl,
		})
		if err != nil {
			return fmt.Errorf("store additional: %s: %w", d.fullKey(key), err)
		}
	}
	return nil
}

// Update Updates an object in cache preserving the existing TTL
func (d *Cache[T]) Update(ctx context.Context, obj T) error {
	if d.cache == nil {
		return errors.New("cache is not configured")
	}

	updateKey := func(key string) error {
		fullKey := d.fullKey(key)

		ttl, err := d.rdb.TTL(ctx, fullKey).Result()
		if err != nil {
			return fmt.Errorf("failed to fetch TTL for key %s: %w", fullKey, err)
		}

		if ttl <= 0 {
			return fmt.Errorf("failed to fetch TTL for key %s", fullKey)
		}

		err = d.cache.Set(&redisCache.Item{
			Ctx:   ctx,
			Key:   fullKey,
			Value: obj,
			TTL:   ttl,
		})
		if err != nil {
			return fmt.Errorf("failed to update key %s: %w", fullKey, err)
		}
		return nil
	}

	// Update primary key
	if err := updateKey(obj.CacheKey()); err != nil {
		return err
	}

	// Update additional keys
	for _, key := range obj.AdditionalCacheKeys() {
		if err := updateKey(key); err != nil {
			return err
		}
	}

	return nil
}
