package productfeaturestest

import (
	"log/slog"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// NewClient isolates feature caches when parallel tests use the same mock org
// in separate Postgres databases. It still exercises durable reads and writes.
func NewClient(t *testing.T, logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *productfeatures.Client {
	t.Helper()
	redisClient := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	return productfeatures.NewClient(logger, tracerProvider, db, redisClient)
}
