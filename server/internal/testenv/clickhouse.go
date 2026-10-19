package testenv

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/wait"
)

const clickhouseTestHostname = "clickhouse"

type ClickhouseClientFunc func(t *testing.T) (clickhouse.Conn, error)

// NewTestClickhouse creates a new ClickHouse container with the schema initialized
// from migration files. Returns a container reference and a function to create
// test connections. The per-test connection is automatically closed via t.Cleanup.
func NewTestClickhouse(ctx context.Context) (*clickhousecontainer.ClickHouseContainer, ClickhouseClientFunc, error) {
	root, err := FindRepoRoot(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("locate ClickHouse test schema: %w", err)
	}
	if err := ensureDockerReady(ctx); err != nil {
		return nil, nil, fmt.Errorf("wait for docker: %w", err)
	}

	container, err := clickhousecontainer.Run(ctx, "clickhouse/clickhouse-server:26.6.8.7-distroless@sha256:31f67400c46f94f7d9236728f369faa125067f13f98fa2ad620ee190fe70eed5",
		clickhousecontainer.WithUsername("gram"),
		clickhousecontainer.WithPassword("gram"),
		clickhousecontainer.WithInitScripts(filepath.Join(root, "server", "clickhouse", "schema.sql")),
		// The image initializes through a localhost-only bootstrap server, so
		// readiness dials the container hostname to require the final network
		// listener. The distroless image has no shell to resolve $(hostname),
		// so pin a hostname that the runtime maps in /etc/hosts.
		testcontainers.WithConfigModifier(func(config *dockercontainer.Config) {
			config.Hostname = clickhouseTestHostname
		}),
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{"clickhouse-client", "--host", clickhouseTestHostname, "--user", "gram", "--password", "gram", "--query", "SELECT 1"}),
		),
		WithPublishedPortWait("9000/tcp"),
		WithLoopbackPorts(),
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
				"async_insert": 0,
				// Inserts that opt into async_insert per query leave the wait
				// to the connection, which is fire-and-forget in production.
				// Waiting here acks each insert only once its rows, and the
				// materialized views they feed, are committed, so tests read
				// their own writes without flushing or polling.
				"wait_for_async_insert": 1,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("connect to clickhouse: %w", err)
		}

		var pingErr error
		for attempt := range 50 {
			if pingErr = conn.Ping(ctx); pingErr == nil || ctx.Err() != nil {
				break
			}
			if attempt < 49 {
				timer := time.NewTimer(200 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
				case <-timer.C:
				}
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

// FlushClickHouseAsyncInserts drains ClickHouse's async insert queue. It is
// not a read-your-writes barrier: the server's background flusher can take an
// entry off the queue just before the flush looks, and schedule it just after
// the flush stops waiting. Inserts through the test connection already wait
// for their own commit (wait_for_async_insert=1), so tests should not need it.
func FlushClickHouseAsyncInserts(t *testing.T, conn clickhouse.Conn) {
	t.Helper()

	require.NoError(t, conn.Exec(t.Context(), "SYSTEM FLUSH ASYNC INSERT QUEUE"))
}
