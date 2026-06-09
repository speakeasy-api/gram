package testenv

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/testcontainers/testcontainers-go"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

type ClickhouseClientFunc func(t *testing.T) (clickhouse.Conn, error)

// NewTestClickhouse creates a new ClickHouse container with the schema initialized
// from migration files. Returns a container reference and a function to create
// test connections. The container is automatically cleaned up when the test ends.
func NewTestClickhouse(ctx context.Context) (*clickhousecontainer.ClickHouseContainer, ClickhouseClientFunc, error) {
	container, err := clickhousecontainer.Run(ctx, "clickhouse/clickhouse-server:25.8.3",
		clickhousecontainer.WithUsername("gram"),
		clickhousecontainer.WithPassword("gram"),
		clickhousecontainer.WithInitScripts(rootPath("clickhouse", "schema.sql")),
		testcontainers.WithLogger(NewTestcontainersLogger()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start clickhouse container: %w", err)
	}

	return container, newClickhouseClientFunc(container), nil
}

func newClickhouseClientFunc(container *clickhousecontainer.ClickHouseContainer) ClickhouseClientFunc {
	// Resolved once per container: probing candidate addresses on every
	// client creation would cost seconds per test on hosts where the
	// container IP is unroutable.
	resolveAddr := sync.OnceValues(func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		candidates, err := candidateAddrs(ctx, container, "9000/tcp")
		if err != nil {
			return "", fmt.Errorf("resolve clickhouse address: %w", err)
		}

		var lastErr error
		for i, addr := range candidates {
			attempts := 3
			if i == len(candidates)-1 {
				attempts = 10
			}

			if lastErr = pingClickhouse(ctx, addr, attempts); lastErr == nil {
				return addr, nil
			}
		}

		return "", fmt.Errorf("clickhouse not reachable on %v: %w", candidates, lastErr)
	})

	return func(t *testing.T) (clickhouse.Conn, error) {
		t.Helper()

		ctx := t.Context()

		addr, err := resolveAddr()
		if err != nil {
			return nil, err
		}

		conn, err := clickhouse.Open(clickhouseTestOptions(addr))
		if err != nil {
			return nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
		}

		if err = conn.Ping(ctx); err != nil {
			return nil, fmt.Errorf("failed to ping clickhouse: %w", err)
		}

		t.Cleanup(func() {
			if err := conn.Close(); err != nil {
				t.Logf("failed to close clickhouse connection: %v", err)
			}
		})

		return conn, nil
	}
}

func clickhouseTestOptions(addr string) *clickhouse.Options {
	return &clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "gram",
			Password: "gram",
		},
		Settings: clickhouse.Settings{
			"async_insert":          0, // Forces inserts to be synchronous
			"wait_for_async_insert": 0,
		},
	}
}

func pingClickhouse(ctx context.Context, addr string, attempts int) error {
	opts := clickhouseTestOptions(addr)
	opts.DialTimeout = 1 * time.Second

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return fmt.Errorf("open clickhouse connection: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	for attempt := range attempts {
		if err = conn.Ping(ctx); err == nil {
			return nil
		}
		if attempt < attempts-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return fmt.Errorf("ping clickhouse: %w", err)
}
