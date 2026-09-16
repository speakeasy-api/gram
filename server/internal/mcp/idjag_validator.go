package mcp

import (
	"fmt"
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
) (*idjag.Validator, error) {
	cache, err := idjag.NewIssuerKeyCache(db)
	if err != nil {
		return nil, fmt.Errorf("create issuer key cache: %w", err)
	}
	store := ratelimit.NewRedisStore(redisClient)
	keyResolver, err := jwks.NewKeyResolver(
		jwks.NewResolver(policy, meterProvider, logger),
		cache,
		ratelimit.New(store, "idjag_jwks_refresh", idJAGKeyRefreshRate),
		ratelimit.New(store, "idjag_jwks_fetch", idJAGKeyFetchRate),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("create issuer key resolver: %w", err)
	}
	keys, err := idjag.NewIssuerVerificationKeys(keyResolver, cache)
	if err != nil {
		return nil, fmt.Errorf("create issuer verification keys: %w", err)
	}
	guard, err := replay.NewRedisGuard(redisClient, "idjag_jti", assertioncore.ReplayHoldFor(idjag.MaxLifetime))
	if err != nil {
		return nil, fmt.Errorf("create replay guard: %w", err)
	}
	postgresStore, err := idjag.NewPostgresStore(db)
	if err != nil {
		return nil, fmt.Errorf("create ID-JAG store: %w", err)
	}
	validator, err := idjag.NewValidator(keys, guard, postgresStore)
	if err != nil {
		return nil, fmt.Errorf("create validator: %w", err)
	}
	return validator, nil
}
