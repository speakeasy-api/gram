package testenv

import (
	"fmt"
	"os"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// ClickhouseClientFunc creates a new ClickHouse connection for tests.
type ClickhouseClientFunc func(t *testing.T) (clickhouse.Conn, error)

// newClickhouseClientFactory creates a ClickhouseClientFunc that connects to the
// test ClickHouse instance. Connection details are read from TEST_CLICKHOUSE_*
// environment variables.
func newClickhouseClientFactory() (ClickhouseClientFunc, error) {
	host := os.Getenv("TEST_CLICKHOUSE_HOST")
	port := os.Getenv("TEST_CLICKHOUSE_NATIVE_PORT")
	user := os.Getenv("TEST_CLICKHOUSE_USER")
	password := os.Getenv("TEST_CLICKHOUSE_PASSWORD")
	database := os.Getenv("TEST_CLICKHOUSE_DB")

	if host == "" || port == "" {
		return nil, fmt.Errorf("TEST_CLICKHOUSE_HOST and TEST_CLICKHOUSE_NATIVE_PORT environment variables must be set")
	}

	// Use defaults matching the mise task if not specified
	if user == "" {
		user = "gram"
	}
	if password == "" {
		password = "gram"
	}
	if database == "" {
		database = "default"
	}

	addr := fmt.Sprintf("%s:%s", host, port)

	return func(t *testing.T) (clickhouse.Conn, error) {
		t.Helper()

		ctx := t.Context()

		conn, err := clickhouse.Open(&clickhouse.Options{
			Addr: []string{addr},
			Auth: clickhouse.Auth{
				Database: database,
				Username: user,
				Password: password,
			},
			Settings: clickhouse.Settings{
				"async_insert":          0, // Forces inserts to be synchronous
				"wait_for_async_insert": 0,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("open clickhouse connection: %w", err)
		}

		if err = conn.Ping(ctx); err != nil {
			return nil, fmt.Errorf("ping clickhouse: %w", err)
		}

		t.Cleanup(func() {
			if err := conn.Close(); err != nil {
				t.Logf("failed to close clickhouse connection: %v", err)
			}
		})

		return conn, nil
	}, nil
}
