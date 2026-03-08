package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const defaultPlatformRateLimit = 600

// RateLimitConfigLoader loads rate limit configuration for a given attribute.
// It allows the middleware to look up per-slug overrides from the platform_rate_limits
// table (cached in Redis). If no override is found, the middleware falls back to the
// default limit of 600 requests per minute.
type RateLimitConfigLoader interface {
	// GetLimit returns the rate limit for the given attribute type and value.
	// Returns 0 and a nil error if no override exists, in which case the
	// default limit should be used.
	GetLimit(ctx context.Context, attributeType, attributeValue string) (int, error)
}

type rateLimitErrorResponse struct {
	Error      string `json:"error"`
	RetryAfter int    `json:"retryAfter"`
}

// RateLimitMiddleware returns middleware that enforces platform rate limits on MCP routes.
// It extracts the MCP slug from the URL path and checks against the rate limiter.
// Only applies to POST requests to /mcp/{slug} and /mcp/{project}/{toolset}/{environment} paths.
// Non-MCP requests and non-POST methods pass through unchanged.
func RateLimitMiddleware(limiter ratelimit.Limiter, configLoader RateLimitConfigLoader, logger *slog.Logger) func(http.Handler) http.Handler {
	logger = logger.With(attr.SlogComponent("ratelimit"))

	meter := otel.GetMeterProvider().Meter("github.com/speakeasy-api/gram/server/internal/middleware")
	checkCounter, err := meter.Int64Counter(
		"mcp.ratelimit.platform.check",
		metric.WithDescription("Platform rate limit checks on MCP requests"),
		metric.WithUnit("{check}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create platform rate limit counter", attr.SlogError(err))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			key, ok := extractMCPKey(r.URL.Path)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()

			limit, err := resolveLimit(ctx, configLoader, key)
			if err != nil {
				logger.WarnContext(ctx, "rate limit config lookup failed, using default",
					attr.SlogError(err),
					slog.String("mcp_key", key),
				)
				limit = defaultPlatformRateLimit
			}

			result, err := limiter.Allow(ctx, "platform:"+key, limit)
			if err != nil {
				// On rate limiter failure, allow the request through to avoid
				// blocking traffic when Redis is unavailable.
				logger.ErrorContext(ctx, "rate limiter check failed",
					attr.SlogError(err),
					slog.String("mcp_key", key),
				)
				next.ServeHTTP(w, r)
				return
			}

			ratelimit.SetHeaders(w, result)

			if checkCounter != nil {
				outcome := "allowed"
				if !result.Allowed {
					outcome = "limited"
				}
				checkCounter.Add(ctx, 1, metric.WithAttributes(
					attr.RateLimitLayer("platform"),
					attr.RateLimitKey(key),
					attr.Outcome(outcome),
				))
			}

			if !result.Allowed {
				retryAfter := max(int(time.Until(result.ResetAt).Seconds())+1, 1)

				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)

				resp := rateLimitErrorResponse{
					Error:      "rate limit exceeded",
					RetryAfter: retryAfter,
				}
				if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
					logger.ErrorContext(ctx, "encode rate limit response",
						attr.SlogError(encErr),
					)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractMCPKey parses the URL path and returns a rate-limiting key for MCP routes.
// Returns ("", false) for non-MCP paths.
//
// IMPORTANT: This function must be updated whenever MCP POST routes change.
// See route registration in server/internal/mcp/impl.go (ServePublic, ServeAuthenticated).
//
// Supported patterns:
//   - /mcp/{mcpSlug}                         -> key = "{mcpSlug}"
//   - /mcp/{project}/{toolset}/{environment} -> key = "{project}:{toolset}"
func extractMCPKey(path string) (string, bool) {
	if !strings.HasPrefix(path, "/mcp/") {
		return "", false
	}

	// Trim the "/mcp/" prefix and any trailing slash.
	trimmed := strings.TrimPrefix(path, "/mcp/")
	trimmed = strings.TrimRight(trimmed, "/")
	if trimmed == "" {
		return "", false
	}

	segments := strings.Split(trimmed, "/")
	switch len(segments) {
	case 1:
		// /mcp/{mcpSlug}
		return segments[0], true
	case 3:
		// /mcp/{project}/{toolset}/{environment}
		return segments[0] + ":" + segments[1], true
	default:
		return "", false
	}
}

// resolveLimit returns the rate limit for the given key, checking for overrides first.
func resolveLimit(ctx context.Context, configLoader RateLimitConfigLoader, key string) (int, error) {
	if configLoader == nil {
		return defaultPlatformRateLimit, nil
	}

	limit, err := configLoader.GetLimit(ctx, "mcp_slug", key)
	if err != nil {
		return 0, fmt.Errorf("get rate limit for %q: %w", key, err)
	}
	if limit > 0 {
		return limit, nil
	}

	return defaultPlatformRateLimit, nil
}
