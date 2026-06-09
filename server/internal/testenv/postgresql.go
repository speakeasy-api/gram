package testenv

import (
	"context"
	"fmt"

	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/speakeasy-api/gram/server/internal/testinfra"
)

type PostgresDBCloneFunc = testinfra.PostgresDBCloneFunc

// NewTestPostgres creates a new Postgres container with a template database built
// from a SQL init script. A reference to the container is returned as well as
// a function to create test databases from the template. All "clone" databases
// are automatically dropped when the test ends using t.Cleanup() hooks.
func NewTestPostgres(ctx context.Context) (*postgres.PostgresContainer, PostgresDBCloneFunc, error) {
	container, cloner, err := testinfra.NewTestPostgres(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("start postgres container: %w", err)
	}
	return container, cloner, nil
}
