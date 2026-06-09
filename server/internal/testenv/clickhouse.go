package testenv

import (
	"context"
	"fmt"

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
		return nil, nil, fmt.Errorf("start clickhouse container: %w", err)
	}
	return container, clientFunc, nil
}
