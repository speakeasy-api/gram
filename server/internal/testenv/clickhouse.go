package testenv

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/testcontainers/testcontainers-go"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

type ClickhouseClientFunc func(t *testing.T) (clickhouse.Conn, error)

// NewTestClickhouse creates a new ClickHouse container with the schema initialized
// from migration files. Returns a container reference and a function to create
// test connections. The container is automatically cleaned up when the test ends.
func NewTestClickhouse(ctx context.Context) (*clickhousecontainer.ClickHouseContainer, ClickhouseClientFunc, error) {
	// Get the schema file path relative to this file's location
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return nil, nil, fmt.Errorf("failed to get current file path")
	}
	schemaPath := filepath.Join(filepath.Dir(filename), "..", "..", "clickhouse", "schema.sql")

	container, err := clickhousecontainer.Run(ctx, "clickhouse/clickhouse-server:25.8.3",
		clickhousecontainer.WithUsername("gram"),
		clickhousecontainer.WithPassword("gram"),
		clickhousecontainer.WithDatabase("gram"),
		clickhousecontainer.WithInitScripts(schemaPath),
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
				Database: "gram",
				Username: "gram",
				Password: "gram",
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
