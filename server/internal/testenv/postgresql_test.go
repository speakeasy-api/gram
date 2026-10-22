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

	// Hold a connection like the maintenance pool's open while cloning. It
	// must not occupy the template database.
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

	// Every clone and drop shares the maintenance pool, and clones of the
	// same template run concurrently.
	for i := range 8 {
		t.Run(fmt.Sprintf("clone_%d", i), func(t *testing.T) {
			t.Parallel()
			pool, err := clone(t, "concurrent_clone")
			require.NoError(t, err)
			require.NoError(t, pool.Ping(t.Context()))
		})
	}
}
