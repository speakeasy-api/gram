package testenv

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/testcontainers/testcontainers-go"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

type ClickhouseClientFunc func(t *testing.T) (clickhouse.Conn, error)

// NewClickhouseContainer creates a new ClickHouse container with the schema initialized
// from migration files. Returns a container reference and a function to create
// test connections. The container is automatically cleaned up when the test ends.
func NewClickhouseContainer(ctx context.Context) (*clickhousecontainer.ClickHouseContainer, ClickhouseClientFunc, error) {
	container, err := clickhousecontainer.Run(ctx, "clickhouse/clickhouse-server:25.8.3",
		clickhousecontainer.WithUsername("gotest"),
		clickhousecontainer.WithPassword("gotest"),
		clickhousecontainer.WithDatabase("gotestdb"),
		clickhousecontainer.WithInitScripts(rootPath("clickhouse", "schema.sql")),
		testcontainers.WithLogger(NewTestcontainersLogger()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start clickhouse container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get clickhouse host: %w", err)
	}

	port, err := container.MappedPort(ctx, "9000/tcp")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get clickhouse port: %w", err)
	}

	uri := url.URL{
		Scheme: "clickhouse",
		User:   url.UserPassword("gotest", "gotest"),
		Host:   fmt.Sprintf("%s:%s", host, port.Port()),
		Path:   "gotestdb",
	}

	return container, newClickhouseClientFunc(uri.String()), nil
}

func newClickhouseClientFunc(uri string) ClickhouseClientFunc {
	return func(t *testing.T) (clickhouse.Conn, error) {
		t.Helper()

		ctx := t.Context()

		parsed, err := url.Parse(uri)
		if err != nil {
			return nil, fmt.Errorf("failed to parse clickhouse URI: %w", err)
		}

		host, port, _ := net.SplitHostPort(parsed.Host)
		if host == "localhost" {
			host = "127.0.0.1"
		}

		conn, err := clickhouse.Open(&clickhouse.Options{
			Addr: []string{net.JoinHostPort(host, port)},
			Auth: clickhouse.Auth{
				Database: parsed.Path[1:],
				Username: parsed.User.Username(),
				Password: func() string {
					password, _ := parsed.User.Password()
					return password
				}(),
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
