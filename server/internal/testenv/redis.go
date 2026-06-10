package testenv

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"runtime"
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

	addr, err := resolveRedisAddr(ctx, container)
	if err != nil {
		tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = container.Terminate(tctx)
		return nil, nil, fmt.Errorf("resolve redis address: %w", err)
	}

	return container, newRedisClientFunc(addr), nil
}

// resolveRedisAddr picks the address test clients will dial, exactly once at
// container startup. Docker's published host port can transiently route to a
// different container when many containers run in parallel under CI load
// (testcontainers-go#2749) — observed as the redis client receiving
// "HTTP/1.1 400 Bad Request" from another container's HTTP port. On Linux
// (i.e. CI) the container IP is routable from the host, so we bypass host
// port mapping entirely; the address is verified with a real PING before
// being adopted, falling back to the published port for environments where
// container IPs are not reachable (e.g. remote Docker daemons, macOS).
func resolveRedisAddr(ctx context.Context, container *tcr.RedisContainer) (string, error) {
	if runtime.GOOS == "linux" {
		if ip, err := container.ContainerIP(ctx); err == nil && ip != "" {
			addr := net.JoinHostPort(ip, "6379")
			if err := pingRedis(ctx, addr); err == nil {
				return addr, nil
			}
		}
	}

	cstr, err := container.ConnectionString(ctx)
	if err != nil {
		return "", fmt.Errorf("get redis connection string: %w", err)
	}

	uri, err := url.Parse(cstr)
	if err != nil {
		return "", fmt.Errorf("parse redis connection string: %w", err)
	}

	host, port, err := net.SplitHostPort(uri.Host)
	if err != nil {
		return "", fmt.Errorf("split redis host/port: %w", err)
	}

	// Avoid a DNS lookup for localhost inside synctest bubbles without
	// changing arbitrary Docker/Testcontainers endpoints. Re-resolving the
	// host here can pick an address that is not the actual published Redis
	// endpoint (for example ::1 instead of Docker's IPv4 localhost binding).
	if host == "localhost" {
		host = "127.0.0.1"
	}

	addr := net.JoinHostPort(host, port)
	if err := pingRedis(ctx, addr); err != nil {
		return "", fmt.Errorf("ping redis at %s: %w", addr, err)
	}

	return addr, nil
}

func pingRedis(ctx context.Context, addr string) error {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  1 * time.Second,
		ReadTimeout:  300 * time.Millisecond,
		WriteTimeout: 1 * time.Second,
		Protocol:     2,
	})
	defer func() { _ = client.Close() }()

	var err error
	for range 10 {
		if err = client.Ping(ctx).Err(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("redis not ready after retries: %w", err)
}

func newRedisClientFunc(addr string) RedisClientFunc {
	return func(t *testing.T, db int) (*redis.Client, error) {
		t.Helper()

		client := redis.NewClient(&redis.Options{
			Addr:         addr,
			DB:           db,
			DialTimeout:  1 * time.Second,
			ReadTimeout:  300 * time.Millisecond,
			WriteTimeout: 1 * time.Second,
			Protocol:     2,
		})

		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Fatalf("failed to close redis client: %v", err)
			}
		})

		return client, nil
	}
}
