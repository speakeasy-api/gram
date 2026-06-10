package testinfra

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/testcontainers/testcontainers-go"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/wait"
)

type ClickhouseClientFunc func(t *testing.T) (clickhouse.Conn, error)

// NewTestClickhouse creates a new ClickHouse container with the schema initialized
// from migration files. Returns a container reference and a function to create
// test connections. The per-test connection is automatically closed via t.Cleanup.
func NewTestClickhouse(ctx context.Context) (*clickhousecontainer.ClickHouseContainer, ClickhouseClientFunc, error) {
	container, err := clickhousecontainer.Run(ctx, "clickhouse/clickhouse-server:25.8.3",
		clickhousecontainer.WithUsername("gram"),
		clickhousecontainer.WithPassword("gram"),
		clickhousecontainer.WithInitScripts(rootPath("clickhouse", "schema.sql")),
		testcontainers.WithWaitStrategy(
			PortWait("9000/tcp"),
			wait.ForExec([]string{"clickhouse-client", "--user", "gram", "--password", "gram", "--query", "SELECT 1"}),
		),
		WithoutPublishedPorts(),
		testcontainers.WithLogger(NewTestcontainersLogger()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start clickhouse container: %w", err)
	}

	return container, newClickhouseClientFunc(container), nil
}

func newClickhouseClientFunc(container *clickhousecontainer.ClickHouseContainer) ClickhouseClientFunc {
	return func(t *testing.T) (clickhouse.Conn, error) {
		t.Helper()

		ctx := t.Context()

		addr, err := ContainerAddr(ctx, container, "9000/tcp")
		if err != nil {
			return nil, fmt.Errorf("resolve clickhouse address: %w", err)
		}

		conn, err := clickhouse.Open(&clickhouse.Options{
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
		})
		if err != nil {
			return nil, fmt.Errorf("connect to clickhouse: %w", err)
		}

		// The container wait can complete while the image entrypoint is
		// still running init scripts against its localhost-only bootstrap
		// instance; retry until the final server accepts connections.
		var pingErr error
		for attempt := range 50 {
			if pingErr = conn.Ping(ctx); pingErr == nil || ctx.Err() != nil {
				break
			}
			if attempt < 49 {
				time.Sleep(200 * time.Millisecond)
			}
		}
		if pingErr != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("ping clickhouse: %w", pingErr)
		}

		t.Cleanup(func() {
			if err := conn.Close(); err != nil {
				t.Logf("close clickhouse connection: %v", err)
			}
		})

		return conn, nil
	}
}
