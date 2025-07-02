package cache

import (
	"context"
	"fmt"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
)

var _ Cache = (*RedisCacheAdapter)(nil)

// RedisCacheAdapter implements the Cache interface using Redis.
type RedisCacheAdapter struct {
	client *redis.Client
	cache  *redisCache.Cache
}

func NewRedisCacheAdapter(client *redis.Client) *RedisCacheAdapter {
	cache := redisCache.New(&redisCache.Options{
		Redis: client,
	})
	return &RedisCacheAdapter{
		client: client,
		cache:  cache,
	}
}

func (r *RedisCacheAdapter) Get(ctx context.Context, key string, value any) error {
	//nolint:wrapcheck // Wrapping happens in the typed cache implementation
	return r.cache.Get(ctx, key, value)
}

func (r *RedisCacheAdapter) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	//nolint:wrapcheck // Wrapping happens in the typed cache implementation
	return r.cache.Set(&redisCache.Item{
		Ctx:   ctx,
		Key:   key,
		Value: value,
		TTL:   ttl,
	})
}

func (r *RedisCacheAdapter) Update(ctx context.Context, key string, value any) error {
	ttl, err := r.client.TTL(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to fetch TTL for key %s: %w", key, err)
	}

	if ttl <= 0 {
		return fmt.Errorf("failed to fetch TTL for key %s", key)
	}

	//nolint:wrapcheck // Wrapping happens in the typed cache implementation
	return r.cache.Set(&redisCache.Item{
		Ctx:   ctx,
		Key:   key,
		Value: value,
		TTL:   ttl,
	})
}

func (r *RedisCacheAdapter) Delete(ctx context.Context, key string) error {
	//nolint:wrapcheck // Wrapping happens in the typed cache implementation
	return r.cache.Delete(ctx, key)
}
