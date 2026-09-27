//go:build localaccounts_integration

package localaccounts

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newProfilePostgres(t *testing.T) testenv.PostgresDBCloneFunc {
	t.Helper()
	container, clone, err := testenv.NewTestPostgres(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	return clone
}

func newProfileFixture(t *testing.T, clone testenv.PostgresDBCloneFunc, scanLocation ...*time.Location) (*pgxpool.Pool, *productfeatures.Client) {
	t.Helper()
	db, err := clone(t, "local_account_profiles")
	require.NoError(t, err)
	t.Cleanup(db.Close)
	if len(scanLocation) > 0 {
		config := db.Config()
		config.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
			conn.TypeMap().RegisterType(&pgtype.Type{Name: "timestamptz", OID: pgtype.TimestamptzOID, Codec: &pgtype.TimestamptzCodec{ScanLocation: scanLocation[0]}})
			return nil
		}
		db, err = pgxpool.NewWithConfig(t.Context(), config)
		require.NoError(t, err)
		t.Cleanup(db.Close)
	}
	_, err = db.Exec(t.Context(), string(testenv.ReadFixture(t, "fixtures/profiles.sql")))
	require.NoError(t, err)
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	return db, productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), db, redisClient)
}
