package route

import (
	"context"
	"errors"
	"fmt"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
)

const keyPrefix = "tunnel_routes:"

const connectionKeyPrefix = "tunnel_connections:"

// Redis stores live routes and snapshots; Postgres owns durable tunnel sources.
type Redis struct {
	client redis.UniversalClient
	cache  *redisCache.Cache
}

func NewRedis(client redis.UniversalClient) *Redis {
	return &Redis{
		client: client,
		cache: redisCache.New(&redisCache.Options{
			Redis: client,
		}),
	}
}

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

func (r *Redis) PublishConnections(ctx context.Context, tunnelID string, connections []Connection, ttl time.Duration) error {
	if err := r.cache.Set(&redisCache.Item{
		Ctx: ctx,
		Key: connectionKeyPrefix + tunnelID,
		Value: struct {
			Connections []Connection `json:"connections"`
		}{
			Connections: connections,
		},
		TTL: ttl,
	}); err != nil {
		return fmt.Errorf("publish tunnel connections: %w", err)
	}
	return nil
}

func (r *Redis) DeleteConnections(ctx context.Context, tunnelID string) error {
	if err := r.cache.Delete(ctx, connectionKeyPrefix+tunnelID); err != nil {
		return fmt.Errorf("delete tunnel connections: %w", err)
	}
	return nil
}

var _ Store = (*Redis)(nil)
var _ ConnectionSnapshotStore = (*Redis)(nil)
