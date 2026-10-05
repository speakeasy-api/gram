package mcp

import (
	"log/slog"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/privatekeyjwt"
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
// shared Redis client, whose replay guard enforces single use.
func newClientAssertionVerifier(redisClient *redis.Client, policy *guardian.Policy, meterProvider metric.MeterProvider, logger *slog.Logger) *privatekeyjwt.Verifier {
	store := ratelimit.NewRedisStore(redisClient)
	refreshLimiter := ratelimit.New(store, "client_assertion_jwks_refresh", clientAssertionKeyRefreshRate)
	fetchLimiter := ratelimit.New(store, "client_assertion_jwks_fetch", clientAssertionKeyFetchRate)
	keys := jwks.NewKeyResolver(jwks.NewResolver(policy, meterProvider, logger), jwks.NewMemoryCache(), refreshLimiter, fetchLimiter, logger)
	return privatekeyjwt.NewVerifier(keys, replay.NewRedisGuard(redisClient, "client_assertion_jti", privatekeyjwt.DefaultMaxReplayHold))
}
