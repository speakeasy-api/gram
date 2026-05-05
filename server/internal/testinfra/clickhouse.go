package testinfra

import (
	"context"
	"fmt"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/testcontainers/testcontainers-go"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

type ClickhouseClientFunc func(t *testing.T) (clickhouse.Conn, error)

// sharedClickhouseConn is a single ClickHouse connection shared across all
// tests in a package. Set once from TestMain via LaunchSharedClickhouse so
// individual tests can pull it without each booting their own container.
//
// Used by callers (e.g. internal/authz) that cannot import internal/testenv
// because of circular dependencies: testenv pulls in service packages that
// already depend on the package under test.
var sharedClickhouseConn clickhouse.Conn

// LaunchSharedClickhouse boots a ClickHouse container, opens a connection,
// registers it for NewClickhouseStub, and returns a cleanup func intended
// to be called from TestMain after m.Run.
func LaunchSharedClickhouse(ctx context.Context) (func() error, error) {
	container, _, err := NewTestClickhouse(ctx)
	if err != nil {
		return nil, err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get clickhouse host: %w", err)
	}

	port, err := container.MappedPort(ctx, "9000/tcp")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get clickhouse port: %w", err)
	}

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%s", host, port.Port())},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "gram",
			Password: "gram",
		},
		Settings: clickhouse.Settings{
			"async_insert":          0,
			"wait_for_async_insert": 0,
		},
	})
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
	}

	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	sharedClickhouseConn = conn

	return func() error {
		sharedClickhouseConn = nil
		closeErr := conn.Close()
		termErr := container.Terminate(ctx)
		if closeErr != nil {
			return fmt.Errorf("close clickhouse connection: %w", closeErr)
		}
		if termErr != nil {
			return fmt.Errorf("terminate clickhouse container: %w", termErr)
		}
		return nil
	}, nil
}

// NewClickhouseStub returns the shared ClickHouse connection registered by
// the calling package's TestMain via LaunchSharedClickhouse. Panics if no
// shared connection is registered.
func NewClickhouseStub() clickhouse.Conn {
	if sharedClickhouseConn == nil {
		panic("testinfra: shared ClickHouse connection not set; call LaunchSharedClickhouse from TestMain")
	}
	return sharedClickhouseConn
}

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
	return func(t *testing.T) (clickhouse.Conn, error) {
		t.Helper()

		ctx := t.Context()
		host, err := container.Host(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get clickhouse host: %w", err)
		}

		port, err := container.MappedPort(ctx, "9000/tcp")
		if err != nil {
			return nil, fmt.Errorf("failed to get clickhouse port: %w", err)
		}

		conn, err := clickhouse.Open(&clickhouse.Options{
			Addr: []string{fmt.Sprintf("%s:%s", host, port.Port())},
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
