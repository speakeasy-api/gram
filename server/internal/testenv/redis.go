package testenv

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcr "github.com/testcontainers/testcontainers-go/modules/redis"
)

type RedisClientFunc func(t *testing.T, db int) (*redis.Client, error)

func NewTestRedis(ctx context.Context) (*tcr.RedisContainer, RedisClientFunc, error) {
	container, err := tcr.Run(ctx, "redis:6.2-alpine", testcontainers.WithLogger(NewTestcontainersLogger()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start redis container: %w", err)
	}

	return container, newRedisClientFunc(container), nil
}

func newRedisClientFunc(container *tcr.RedisContainer) RedisClientFunc {
	return func(t *testing.T, db int) (*redis.Client, error) {
		t.Helper()

		cstr, err := container.ConnectionString(t.Context())
		if err != nil {
			return nil, fmt.Errorf("failed to get redis connection string: %w", err)
		}

		uri, err := url.Parse(cstr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse redis connection string: %w", err)
		}

		// Resolve hostname to IP up front so that Redis connections inside a
		// synctest bubble never trigger DNS lookups (Go's net.DefaultResolver
		// uses a global singleflight whose goroutines live outside any bubble).
		host, port, _ := net.SplitHostPort(uri.Host)
		if ips, err := net.LookupHost(host); err == nil && len(ips) > 0 {
			host = ips[0]
		}

		client := redis.NewClient(&redis.Options{
			Addr:         net.JoinHostPort(host, port),
			DB:           db,
			DialTimeout:  1 * time.Second,
			ReadTimeout:  300 * time.Millisecond,
			WriteTimeout: 1 * time.Second,
		})

		// Verify the connection is alive before returning. Without this,
		// the container's mapped port may be open before Redis is fully
		// ready to speak RESP, causing "can't parse map reply: HTTP/1.1
		// 400 Bad Request" errors under CI load.
		ctx := t.Context()
		for attempt := range 10 {
			if err := client.Ping(ctx).Err(); err == nil {
				break
			} else if attempt == 9 {
				_ = client.Close()
				return nil, fmt.Errorf("redis not ready after retries: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
		}

		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Fatalf("failed to close redis client: %v", err)
			}
		})

		return client, nil
	}
}
