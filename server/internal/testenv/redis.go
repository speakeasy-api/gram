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

		host, port, err := net.SplitHostPort(uri.Host)
		if err != nil {
			return nil, fmt.Errorf("split redis host/port: %w", err)
		}

		// Avoid a DNS lookup for localhost inside synctest bubbles without
		// changing arbitrary Docker/Testcontainers endpoints. Re-resolving the
		// host here can pick an address that is not the actual published Redis
		// endpoint (for example ::1 instead of Docker's IPv4 localhost binding).
		if host == "localhost" {
			host = "127.0.0.1"
		}

		client := redis.NewClient(&redis.Options{
			Addr:         net.JoinHostPort(host, port),
			DB:           db,
			DialTimeout:  1 * time.Second,
			ReadTimeout:  300 * time.Millisecond,
			WriteTimeout: 1 * time.Second,
		})

		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Fatalf("failed to close redis client: %v", err)
			}
		})

		return client, nil
	}
}
