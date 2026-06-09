package testenv

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcr "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/speakeasy-api/gram/server/internal/testinfra"
)

type RedisClientFunc func(t *testing.T, db int) (*redis.Client, error)

func NewTestRedis(ctx context.Context) (*tcr.RedisContainer, RedisClientFunc, error) {
	container, err := tcr.Run(
		ctx, "redis:6.2-alpine",
		testcontainers.WithLogger(NewTestcontainersLogger()),
		testcontainers.WithWaitStrategy(
			wait.ForLog("* Ready to accept connections"),
			testinfra.PortWait("6379/tcp"),
			wait.ForExec([]string{"redis-cli", "ping"}),
		),
		testinfra.WithoutPublishedPorts(),
	)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to start redis container: %w", err)
	}

	return container, newRedisClientFunc(container), nil
}

func newRedisClientFunc(container *tcr.RedisContainer) RedisClientFunc {
	return func(t *testing.T, db int) (*redis.Client, error) {
		t.Helper()

		ctx := t.Context()

		addr, err := testinfra.ContainerAddr(ctx, container, "6379/tcp")
		if err != nil {
			return nil, fmt.Errorf("resolve redis address: %w", err)
		}

		client := redis.NewClient(&redis.Options{
			Addr:         addr,
			DB:           db,
			DialTimeout:  1 * time.Second,
			ReadTimeout:  300 * time.Millisecond,
			WriteTimeout: 1 * time.Second,
			Protocol:     2,
		})

		var pingErr error
		for attempt := range 10 {
			if pingErr = client.Ping(ctx).Err(); pingErr == nil {
				break
			}
			if attempt < 9 {
				time.Sleep(100 * time.Millisecond)
			}
		}
		if pingErr != nil {
			_ = client.Close()
			return nil, fmt.Errorf("redis not ready after retries: %w", pingErr)
		}

		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Fatalf("failed to close redis client: %v", err)
			}
		})

		return client, nil
	}
}
