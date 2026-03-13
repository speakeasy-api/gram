package cache

import (
	"context"
	"encoding/json"
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

func (r *RedisCacheAdapter) ListAppend(ctx context.Context, key string, value any, ttl time.Duration) error {
	// Marshal the value to JSON
	data, err := r.cache.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}

	// Use RPUSH to atomically append to the list
	if err := r.client.RPush(ctx, key, data).Err(); err != nil {
		return fmt.Errorf("rpush: %w", err)
	}

	// Set expiration if TTL is provided and key is new
	if ttl > 0 {
		// Only set TTL if it's not already set (to avoid resetting TTL on each append)
		exists, err := r.client.TTL(ctx, key).Result()
		if err != nil {
			return fmt.Errorf("check ttl: %w", err)
		}
		if exists == -1 { // Key exists but has no TTL
			if err := r.client.Expire(ctx, key, ttl).Err(); err != nil {
				return fmt.Errorf("expire: %w", err)
			}
		}
	}

	return nil
}

func (r *RedisCacheAdapter) ListRange(ctx context.Context, key string, start, stop int64, value any) error {
	// Use LRANGE to get all elements as compressed/marshaled bytes
	result, err := r.client.LRange(ctx, key, start, stop).Result()
	if err != nil {
		return fmt.Errorf("lrange: %w", err)
	}

	// If no results, return nil (empty list)
	if len(result) == 0 {
		return nil
	}

	// We need to unmarshal each item individually since they're stored as compressed msgpack blobs
	// First, re-marshal them as a JSON array by unmarshaling each one and collecting them
	var jsonItems []json.RawMessage
	for _, item := range result {
		// Unmarshal the compressed msgpack item into a generic interface
		var elem any
		if err := r.cache.Unmarshal([]byte(item), &elem); err != nil {
			return fmt.Errorf("unmarshal element: %w", err)
		}

		// Marshal it back to JSON
		jsonBytes, err := json.Marshal(elem)
		if err != nil {
			return fmt.Errorf("marshal to json: %w", err)
		}
		jsonItems = append(jsonItems, jsonBytes)
	}

	// Marshal the JSON items into a JSON array
	jsonArray, err := json.Marshal(jsonItems)
	if err != nil {
		return fmt.Errorf("marshal items: %w", err)
	}

	// Unmarshal the JSON array into the target type
	if err := json.Unmarshal(jsonArray, value); err != nil {
		return fmt.Errorf("unmarshal to target: %w", err)
	}

	return nil
}

func (r *RedisCacheAdapter) DeleteByPrefix(ctx context.Context, prefix string) error {
	var cursor uint64
	for {
		keys, next, err := r.client.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return fmt.Errorf("scan keys with prefix %q: %w", prefix, err)
		}
		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("delete keys with prefix %q: %w", prefix, err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}
