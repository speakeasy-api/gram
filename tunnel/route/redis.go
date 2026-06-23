package route

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyPrefix namespaces route keys in Redis: tunnel_routes:<tunnelID>.
const keyPrefix = "tunnel_routes:"

// Redis is a Store backed by go-redis. Loss of Redis only degrades routing (in
// prod it would fall back to the DB); it is a cache, never the source of truth.
type Redis struct {
	client redis.UniversalClient
}

// NewRedis wraps an existing redis client.
func NewRedis(client redis.UniversalClient) *Redis { return &Redis{client: client} }

func (r *Redis) Publish(ctx context.Context, tunnelID, addr string, ttl time.Duration) error {
	if err := r.client.Set(ctx, keyPrefix+tunnelID, addr, ttl).Err(); err != nil {
		return fmt.Errorf("publish route: %w", err)
	}
	return nil
}

func (r *Redis) Lookup(ctx context.Context, tunnelID string) (string, bool, error) {
	addr, err := r.client.Get(ctx, keyPrefix+tunnelID).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("lookup route: %w", err)
	}
	return addr, true, nil
}

func (r *Redis) Delete(ctx context.Context, tunnelID string) error {
	if err := r.client.Del(ctx, keyPrefix+tunnelID).Err(); err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

var _ Store = (*Redis)(nil)
