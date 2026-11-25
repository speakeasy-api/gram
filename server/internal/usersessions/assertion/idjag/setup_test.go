package idjag

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newIssuerCacheTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "idjag_cache")
	require.NoError(t, err)
	return db
}
