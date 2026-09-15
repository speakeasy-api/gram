package mcp

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

// clientAssertionKeyRefreshRate bounds how often one client key set is
// re-fetched because an assertion arrived with an unknown kid. The budget is
// fleet-wide while the key cache is per replica, so after a real rotation
// each replica needs one forced refresh to converge: the burst is sized for
// that, and the sustained rate for nothing more than an occasional rotation.
var clientAssertionKeyRefreshRate = ratelimit.PerMinute(10)

// clientAssertionKeyFetchRate bounds every upstream key set consult one
// issuer's clients can cause between them, cold fetches included. Sized for
// an issuer's legitimate volume, which is one fetch per key source per cache
// lifetime plus the occasional rotation, with headroom for a fleet of
// replicas each warming their own cache.
var clientAssertionKeyFetchRate = ratelimit.PerMinute(30)

// clientAssertionSigningAlgorithms is the assertion algorithm allowlist in
// the form the RFC 8414 document advertises it. Derived from the verifier's
// own allowlist rather than listed by hand, so what is advertised and what is
// accepted cannot drift apart.
func clientAssertionSigningAlgorithms() []string {
	allowed := jwks.AllowedSignatureAlgorithms()
	names := make([]string, len(allowed))
	for i, alg := range allowed {
		names[i] = string(alg)
	}
	return names
}

// newClientAssertionVerifier assembles the client assertion verifier over the
// shared Redis client, or returns nil when there is none. The replay guard
// cannot promise single use without a shared store, so a surface built
// without Redis refuses assertion-authenticated clients rather than waving
// them through; every consumer checks for nil before use. The only
// construction errors are nil dependencies, which the checks above rule out,
// so a failure here is logged and treated exactly like an absent Redis.
func newClientAssertionVerifier(redisClient *redis.Client, policy *guardian.Policy, meterProvider metric.MeterProvider, logger *slog.Logger) *clientauth.Verifier {
	if redisClient == nil {
		return nil
	}
	store := ratelimit.NewRedisStore(redisClient)
	refreshLimiter := ratelimit.New(store, "client_assertion_jwks_refresh", clientAssertionKeyRefreshRate)
	fetchLimiter := ratelimit.New(store, "client_assertion_jwks_fetch", clientAssertionKeyFetchRate)
	keys, err := jwks.NewKeyResolver(jwks.NewResolver(policy, meterProvider, logger), jwks.NewMemoryCache(), refreshLimiter, fetchLimiter, logger)
	if err != nil {
		logger.ErrorContext(context.Background(), "client assertion key resolver unavailable, assertion clients will be refused", attr.SlogError(err))
		return nil
	}
	guard, err := replay.NewRedisGuard(redisClient, "client_assertion_jti", clientauth.DefaultMaxReplayHold)
	if err != nil {
		logger.ErrorContext(context.Background(), "client assertion replay guard unavailable, assertion clients will be refused", attr.SlogError(err))
		return nil
	}
	verifier, err := clientauth.NewVerifier(keys, guard)
	if err != nil {
		logger.ErrorContext(context.Background(), "client assertion verifier unavailable, assertion clients will be refused", attr.SlogError(err))
		return nil
	}
	return verifier
}
