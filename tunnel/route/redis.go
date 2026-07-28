package route

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis stores live routes and snapshots; Postgres owns durable tunnel sources.
type Redis struct {
	client redis.UniversalClient
}

func NewRedis(client redis.UniversalClient) *Redis {
	return &Redis{
		client: client,
	}
}

func (r *Redis) Publish(ctx context.Context, tunnelID, addr string, ttl time.Duration) error {
	expiresAt := time.Now().Add(ttl).UnixMilli()
	key := RouteKey(tunnelID)
	_, err := r.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.ZAdd(ctx, key, redis.Z{Score: float64(expiresAt), Member: addr})
		pipe.Expire(ctx, key, ttl)
		return nil
	})
	if err != nil {
		return fmt.Errorf("publish route: %w", err)
	}
	return nil
}

func (r *Redis) Candidates(ctx context.Context, tunnelID string) ([]string, error) {
	key := RouteKey(tunnelID)
	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	var candidatesCmd *redis.StringSliceCmd
	_, err := r.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.ZRemRangeByScore(ctx, key, "-inf", now)
		candidatesCmd = pipe.ZRangeByScore(ctx, key, &redis.ZRangeBy{Min: now, Max: "+inf"})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	candidates, err := candidatesCmd.Result()
	if err != nil {
		return nil, fmt.Errorf("read route candidates: %w", err)
	}
	sort.Strings(candidates)
	return candidates, nil
}

func (r *Redis) Unpublish(ctx context.Context, tunnelID, addr string) error {
	if err := r.client.ZRem(ctx, RouteKey(tunnelID), addr).Err(); err != nil {
		return fmt.Errorf("unpublish route: %w", err)
	}
	return nil
}

func (r *Redis) Delete(ctx context.Context, tunnelID string) error {
	if err := r.client.Del(ctx, RouteKey(tunnelID)).Err(); err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

type connectionSnapshot struct {
	ExpiresAt   time.Time    `json:"expires_at"`
	Connections []Connection `json:"connections"`
}

func (r *Redis) PublishConnections(ctx context.Context, tunnelID, owner string, connections []Connection, ttl time.Duration) error {
	raw, err := json.Marshal(connectionSnapshot{
		ExpiresAt:   time.Now().Add(ttl),
		Connections: connections,
	})
	if err != nil {
		return fmt.Errorf("marshal tunnel connections: %w", err)
	}

	key := ConnectionKey(tunnelID)
	_, err = r.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, key, owner, raw)
		pipe.Expire(ctx, key, ttl)
		return nil
	})
	if err != nil {
		return fmt.Errorf("publish tunnel connections: %w", err)
	}
	return nil
}

func (r *Redis) Connections(ctx context.Context, tunnelID string) ([]Connection, error) {
	key := ConnectionKey(tunnelID)
	fields, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("read tunnel connections: %w", err)
	}
	if len(fields) == 0 {
		return nil, nil
	}

	now := time.Now()
	expiredOwners := make([]string, 0)
	connections := make([]Connection, 0)
	for owner, raw := range fields {
		var snapshot connectionSnapshot
		if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
			return nil, fmt.Errorf("unmarshal tunnel connections: %w", err)
		}
		if now.After(snapshot.ExpiresAt) {
			expiredOwners = append(expiredOwners, owner)
			continue
		}
		connections = append(connections, snapshot.Connections...)
	}
	if len(expiredOwners) > 0 {
		_ = r.client.HDel(ctx, key, expiredOwners...).Err()
	}
	return connections, nil
}

func (r *Redis) DeleteConnectionOwner(ctx context.Context, tunnelID, owner string) error {
	if err := r.client.HDel(ctx, ConnectionKey(tunnelID), owner).Err(); err != nil {
		return fmt.Errorf("delete tunnel connection owner: %w", err)
	}
	return nil
}

func (r *Redis) DeleteConnections(ctx context.Context, tunnelID string) error {
	if err := r.client.Del(ctx, ConnectionKey(tunnelID)).Err(); err != nil {
		return fmt.Errorf("delete tunnel connections: %w", err)
	}
	return nil
}

var _ Store = (*Redis)(nil)
var _ ConnectionSnapshotStore = (*Redis)(nil)
