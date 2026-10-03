package mcp

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

var (
	// ID-JAG key refreshes are rare rotations. The fleet-wide budget permits
	// each replica to converge while bounding random-kid probes.
	idJAGKeyRefreshRate = ratelimit.PerMinute(10)

	// Cold and forced key fetches share an issuer-scoped fleet-wide budget.
	idJAGKeyFetchRate = ratelimit.PerMinute(30)
)

func newIDJAGValidator(
	db *pgxpool.Pool,
	redisClient *redis.Client,
	policy *guardian.Policy,
	meterProvider metric.MeterProvider,
	logger *slog.Logger,
) *idjag.Validator {
	cache := idjag.NewIssuerKeyCache(db)
	store := ratelimit.NewRedisStore(redisClient)
	keyResolver := jwks.NewKeyResolver(
		jwks.NewResolver(policy, meterProvider, logger),
		cache,
		ratelimit.New(store, "idjag_jwks_refresh", idJAGKeyRefreshRate),
		ratelimit.New(store, "idjag_jwks_fetch", idJAGKeyFetchRate),
		logger,
	)
	keys := idjag.NewIssuerVerificationKeys(keyResolver, cache)
	guard := replay.NewRedisGuard(redisClient, "idjag_jti", assertioncore.ReplayHoldFor(idjag.MaxLifetime))
	return idjag.NewValidator(keys, guard, idjag.NewPostgresStore(db))
}
