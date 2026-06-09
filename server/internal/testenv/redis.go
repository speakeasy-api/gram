package testenv

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcr "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

type RedisClientFunc func(t *testing.T, db int) (*redis.Client, error)

func NewTestRedis(ctx context.Context) (*tcr.RedisContainer, RedisClientFunc, error) {
	container, err := tcr.Run(
		ctx, "redis:6.2-alpine",
		testcontainers.WithLogger(NewTestcontainersLogger()),
		testcontainers.WithAdditionalWaitStrategy(
			wait.ForExec([]string{"redis-cli", "ping"}),
		),
	)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to start redis container: %w", err)
	}

	return container, newRedisClientFunc(container), nil
}

func newRedisClientFunc(container *tcr.RedisContainer) RedisClientFunc {
	// Resolved once per container: probing candidate addresses on every
	// client creation would cost seconds per test on hosts where the
	// container IP is unroutable.
	resolveAddr := sync.OnceValues(func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		candidates, err := candidateAddrs(ctx, container, "6379/tcp")
		if err != nil {
			return "", fmt.Errorf("resolve redis address: %w", err)
		}

		var lastErr error
		for i, addr := range candidates {
			attempts := 3
			if i == len(candidates)-1 {
				attempts = 10
			}

			client := newRedisTestClient(addr, 0)
			lastErr = pingRedis(ctx, client, attempts)
			_ = client.Close()
			if lastErr == nil {
				return addr, nil
			}
		}

		return "", fmt.Errorf("redis not reachable on %v: %w", candidates, lastErr)
	})

	return func(t *testing.T, db int) (*redis.Client, error) {
		t.Helper()

		addr, err := resolveAddr()
		if err != nil {
			return nil, err
		}

		client := newRedisTestClient(addr, db)

		if err := pingRedis(t.Context(), client, 10); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("redis not ready after retries: %w", err)
		}

		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Fatalf("failed to close redis client: %v", err)
			}
		})

		return client, nil
	}
}

func newRedisTestClient(addr string, db int) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         addr,
		DB:           db,
		DialTimeout:  1 * time.Second,
		ReadTimeout:  300 * time.Millisecond,
		WriteTimeout: 1 * time.Second,
		Protocol:     2,
	})
}

func pingRedis(ctx context.Context, client *redis.Client, attempts int) error {
	var err error
	for attempt := range attempts {
		if err = client.Ping(ctx).Err(); err == nil {
			return nil
		}
		if attempt < attempts-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return fmt.Errorf("ping redis: %w", err)
}
