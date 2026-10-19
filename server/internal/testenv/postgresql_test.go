package testenv

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestPostgresCloneWithCleanupConnection(t *testing.T) {
	t.Parallel()

	container, clone, err := NewTestPostgres(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })

	// Hold the same connection used by cleanup open while cloning. Cleanup
	// runs outside the clone mutex and must not occupy the template database.
	uri, err := postgresURI(t.Context(), container)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), uri)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })

	pool, err := clone(t, "cleanup_overlap")
	require.NoError(t, err)
	require.NoError(t, pool.Ping(t.Context()))
}

func TestPostgresConcurrentClonesAreDropped(t *testing.T) {
	t.Parallel()

	container, clone, err := NewTestPostgres(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })

	uri, err := postgresURI(t.Context(), container)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), uri)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })

	// Runs after every clone subtest and its drop, before the container stops.
	t.Cleanup(func() {
		remaining, err := testrepo.New(conn).CountDatabasesWithPrefixFixture(context.Background(), "concurrent_clone_")
		require.NoError(t, err)
		require.Zero(t, remaining, "clone databases are dropped when their test ends")
	})

	// Every clone and drop shares the maintenance pool.
	for i := range 8 {
		t.Run(fmt.Sprintf("clone_%d", i), func(t *testing.T) {
			t.Parallel()
			pool, err := clone(t, "concurrent_clone")
			require.NoError(t, err)
			require.NoError(t, pool.Ping(t.Context()))
		})
	}
}
