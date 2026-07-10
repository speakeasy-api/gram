// Package thirdparty contains shared composition helpers and policies for
// outbound vendor clients. Vendor-specific API wrappers remain in subpackages.
package thirdparty

import (
	"log/slog"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

// HTTPRateLimit describes the upstream scope shared by all processes. KeyFor
// must match the vendor's ownership model (account, source IP, workspace,
// etc.), not Gram's tenant model.
type HTTPRateLimit struct {
	Name   string
	KeyFor ratelimit.HTTPKeyFunc
	Rate   ratelimit.Rate
}

// WorkOSHTTPRateLimit returns the source-wide WorkOS API policy.
func WorkOSHTTPRateLimit() HTTPRateLimit {
	// WorkOS documents 6,000 requests per 60 seconds per source IP. The lower
	// sustained rate leaves headroom and the small burst smooths webhook waves.
	return HTTPRateLimit{
		Name:   "workos-api",
		KeyFor: ratelimit.StaticHTTPKey("global"),
		Rate:   ratelimit.PerMinute(5_400).WithBurst(20),
	}
}

// OpenRouterHTTPRateLimit returns the account-wide OpenRouter API policy.
func OpenRouterHTTPRateLimit() HTTPRateLimit {
	// OpenRouter governs paid capacity globally: creating child API keys does
	// not create more capacity. Preserve the existing 300 RPM ceiling and its
	// headroom, but apply it once to the whole account and to every endpoint.
	return HTTPRateLimit{
		Name:   "openrouter-api",
		KeyFor: ratelimit.StaticHTTPKey("account"),
		Rate:   ratelimit.PerMinute(250).WithBurst(50),
	}
}

// LoopsHTTPRateLimit returns the team-wide Loops API policy.
func LoopsHTTPRateLimit() HTTPRateLimit {
	// Loops documents a baseline of 10 requests per second per team. Keep the
	// combined steady rate and burst at that ceiling.
	return HTTPRateLimit{
		Name:   "loops-api",
		KeyFor: ratelimit.StaticHTTPKey("team"),
		Rate:   ratelimit.PerSecond(8).WithBurst(2),
	}
}

// NewRateLimitedHTTPClient builds a guardian client whose every physical
// attempt participates in the vendor's fleet-wide bucket and shared
// Retry-After cooldown. Additional guardian options retain per-SDK control of
// retry behavior and network policy.
func NewRateLimitedHTTPClient(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	guardianPolicy *guardian.Policy,
	redisClient *redis.Client,
	limit HTTPRateLimit,
	opts ...guardian.ClientOption,
) *guardian.HTTPClient {
	limiter := ratelimit.New(ratelimit.NewRedisStore(redisClient), limit.Name, limit.Rate, ratelimit.WithMetrics(meterProvider))
	opts = append(opts, guardian.WithRoundTripperMiddleware(ratelimit.HTTPMiddleware(logger, limiter, limit.KeyFor)))
	return guardianPolicy.PooledClient(opts...)
}
