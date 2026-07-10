package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const retryAfterWriteTimeout = time.Second

type rateLimitRoundTripper struct {
	next    http.RoundTripper
	limiter *Limiter
	keyFor  HTTPKeyFunc
	logger  *slog.Logger
}

// HTTPKeyFunc maps an outbound request to the upstream-owned quota bucket.
// Vendor policies can use a constant account key or derive scopes such as a
// Slack workspace + method or GitHub installation from the request.
type HTTPKeyFunc func(*http.Request) string

// StaticHTTPKey returns a key function for account-wide or source-wide quotas.
func StaticHTTPKey(key string) HTTPKeyFunc {
	return func(*http.Request) string { return key }
}

// HTTPMiddleware gates every physical HTTP attempt through limiter. It fails
// open when the backing store is unavailable, but preserves request context
// cancellation. Retry-After on 429 and 503 responses is published to the
// shared store so other replicas pause before their next attempt too.
func HTTPMiddleware(logger *slog.Logger, limiter *Limiter, keyFor HTTPKeyFunc) func(http.RoundTripper) http.RoundTripper {
	return func(next http.RoundTripper) http.RoundTripper {
		return &rateLimitRoundTripper{
			next:    next,
			limiter: limiter,
			keyFor:  keyFor,
			logger:  logger,
		}
	}
}

func (r *rateLimitRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	key := r.keyFor(req)
	if key == "" {
		return nil, fmt.Errorf("resolve HTTP rate-limit key: empty key")
	}
	if err := r.limiter.Wait(req.Context(), key); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("wait for HTTP rate limit: %w", err)
		}
		r.logger.WarnContext(req.Context(), "HTTP rate limiter unavailable, allowing request",
			attr.SlogRateLimitName(r.limiter.name),
			attr.SlogError(err),
		)
	}

	resp, err := r.next.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("rate-limited HTTP round trip: %w", err)
	}

	retryAfter := responseRetryAfter(resp, time.Now())
	if retryAfter <= 0 {
		return resp, nil
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), retryAfterWriteTimeout)
	defer cancel()
	if err := r.limiter.SetRetryAfter(ctx, key, retryAfter); err != nil {
		r.logger.WarnContext(req.Context(), "failed to share upstream Retry-After",
			attr.SlogRateLimitName(r.limiter.name),
			attr.SlogRetryWait(retryAfter),
			attr.SlogError(err),
		)
	}

	return resp, nil
}

func responseRetryAfter(resp *http.Response, now time.Time) time.Duration {
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		if retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), now); retryAfter > 0 {
			return retryAfter
		}
	}

	if strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0" {
		reset, err := strconv.ParseInt(strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset")), 10, 64)
		if err == nil {
			when := time.Unix(reset, 0)
			if when.After(now) {
				return when.Sub(now)
			}
		}
	}

	if strings.TrimSpace(resp.Header.Get("RateLimit-Remaining")) == "0" {
		resetSeconds, err := strconv.ParseInt(strings.TrimSpace(resp.Header.Get("RateLimit-Reset")), 10, 64)
		if err == nil && resetSeconds > 0 {
			return time.Duration(resetSeconds) * time.Second
		}
	}

	return 0
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}
