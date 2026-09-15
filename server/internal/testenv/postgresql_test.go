package testenv

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
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
