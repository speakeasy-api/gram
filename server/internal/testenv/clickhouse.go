package testenv

import (
	"context"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"

	"github.com/speakeasy-api/gram/server/internal/testinfra"
)

type ClickhouseClientFunc = testinfra.ClickhouseClientFunc

// NewTestClickhouse creates a new ClickHouse container with the schema initialized
// from migration files. Returns a container reference and a function to create
// test connections. The container is automatically cleaned up when the test ends.
func NewTestClickhouse(ctx context.Context) (*clickhousecontainer.ClickHouseContainer, ClickhouseClientFunc, error) {
	container, clientFunc, err := testinfra.NewTestClickhouse(ctx)
	if err != nil {
		//nolint:wrapcheck // delegation; testinfra already wraps with context
		return nil, nil, err
	}
	return container, clientFunc, nil
}

// FlushClickHouseAsyncInserts synchronously drains ClickHouse's async insert
// queue. Some write paths (e.g. telemetry logs) use server-side async
// fire-and-forget inserts, so rows only become visible to queries after a
// buffer flush. Calling this after a write makes the data deterministically
// visible without polling.
func FlushClickHouseAsyncInserts(t *testing.T, conn clickhouse.Conn) {
	t.Helper()

	require.NoError(t, conn.Exec(t.Context(), "SYSTEM FLUSH ASYNC INSERT QUEUE"))
}
