package testenv

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics"
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
		clickhousecontainer.WithDatabase("gram"),
		clickhousecontainer.WithInitScripts(filepath.Join("..", "..", "clickhouse", "schema.sql")),
		testcontainers.WithLogger(NewTestcontainersLogger()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start clickhouse container: %w", err)
	}

	// Get host and port
	host, err := container.Host(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get clickhouse host: %w", err)
	}

	port, err := container.MappedPort(ctx, "9000/tcp")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get clickhouse port: %w", err)
	}

	// Connect and run migrations
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%s", host, port.Port())},
		Auth: clickhouse.Auth{
			Database: "gram",
			Username: "gram",
			Password: "gram",
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
	}
	defer o11y.NoLogDefer(func() error {
		return conn.Close()
	})

	if err = conn.Ping(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to ping clickhouse: %w", err)
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
				Database: "gram",
				Username: "gram",
				Password: "gram",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
		}

		t.Cleanup(func() {
			defer o11y.NoLogDefer(func() error {
				if err2 := conn.Close(); err2 != nil {
					t.Logf("failed to close clickhouse connection: %v", err2)
					return err2
				}
				return nil
			})
		})

		return conn, nil
	}
}

// NewTestClickhouseProvider creates a ClickHouse test container and returns
// a direct connection and the container for manual lifecycle management.
// This is intended for use in TestMain where testing.T is not available.
// The caller is responsible for closing the connection and terminating the container.
func NewTestClickhouseProvider(ctx context.Context) (clickhouse.Conn, *clickhousecontainer.ClickHouseContainer, error) {
	container, _, err := NewTestClickhouse(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create clickhouse test container: %w", err)
	}

	// Get host and port
	host, err := container.Host(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get clickhouse host: %w", err)
	}

	port, err := container.MappedPort(ctx, "9000/tcp")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get clickhouse port: %w", err)
	}

	// Connect directly
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%s", host, port.Port())},
		Auth: clickhouse.Auth{
			Database: "gram",
			Username: "gram",
			Password: "gram",
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
	}

	return conn, container, nil
}

// NewSharedToolMetricsClient creates a shared ClickHouse container and tool metrics client
// for use in TestMain. Returns the client and a cleanup function.
// This is intended for tests that need a single shared ClickHouse instance across all tests.
func NewSharedToolMetricsClient(ctx context.Context) (*toolmetrics.ClickhouseClient, func() error, error) {
	conn, container, err := NewTestClickhouseProvider(ctx)
	if err != nil {
		return nil, nil, err
	}

	client := &toolmetrics.ClickhouseClient{
		Conn:   conn,
		Logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}

	cleanup := func() error {
		_ = conn.Close()
		return container.Terminate(ctx)
	}

	return client, cleanup, nil
}
