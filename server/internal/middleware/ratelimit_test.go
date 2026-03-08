package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

// --- extractMCPKey tests ---

func TestExtractMCPKeyPublicRoute(t *testing.T) {
	key, ok := extractMCPKey("/mcp/my-server")
	require.True(t, ok)
	require.Equal(t, "my-server", key)
}

func TestExtractMCPKeyPublicRouteTrailingSlash(t *testing.T) {
	key, ok := extractMCPKey("/mcp/my-server/")
	require.True(t, ok)
	require.Equal(t, "my-server", key)
}

func TestExtractMCPKeyAuthenticatedRoute(t *testing.T) {
	key, ok := extractMCPKey("/mcp/my-project/my-toolset/production")
	require.True(t, ok)
	require.Equal(t, "my-project:my-toolset", key)
}

func TestExtractMCPKeyNonMCPPath(t *testing.T) {
	_, ok := extractMCPKey("/api/v1/tools")
	require.False(t, ok)
}

func TestExtractMCPKeyEmptySlug(t *testing.T) {
	_, ok := extractMCPKey("/mcp/")
	require.False(t, ok)
}

func TestExtractMCPKeyTwoSegments(t *testing.T) {
	// Two segments don't match either pattern.
	_, ok := extractMCPKey("/mcp/project/toolset")
	require.False(t, ok)
}

func TestExtractMCPKeyFourSegments(t *testing.T) {
	// Four segments don't match either pattern.
	_, ok := extractMCPKey("/mcp/a/b/c/d")
	require.False(t, ok)
}

func TestExtractMCPKeyRootPath(t *testing.T) {
	_, ok := extractMCPKey("/")
	require.False(t, ok)
}

// --- mock types ---

type mockLimiter struct {
	result ratelimit.Result
	err    error
	called bool
	key    string
}

func (m *mockLimiter) Allow(_ context.Context, key string, _ int) (ratelimit.Result, error) {
	m.called = true
	m.key = key
	return m.result, m.err
}

type mockConfigLoader struct {
	limit int
	err   error
}

func (m *mockConfigLoader) GetLimit(_ context.Context, _, _ string) (int, error) {
	return m.limit, m.err
}

// nextHandler records whether the downstream handler was called.
func nextHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

// --- middleware tests ---

func TestRateLimitMiddlewareSkipsGetRequests(t *testing.T) {
	t.Parallel()

	limiter := &mockLimiter{}
	mw := RateLimitMiddleware(limiter, nil, slog.New(slog.DiscardHandler))

	var called bool
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodGet, "/mcp/my-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.True(t, called, "GET requests should pass through")
	require.False(t, limiter.called, "limiter should not be invoked for GET")
}

func TestRateLimitMiddlewareSkipsNonMCPPaths(t *testing.T) {
	t.Parallel()

	limiter := &mockLimiter{}
	mw := RateLimitMiddleware(limiter, nil, slog.New(slog.DiscardHandler))

	var called bool
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tools", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.True(t, called, "non-MCP paths should pass through")
	require.False(t, limiter.called, "limiter should not be invoked for non-MCP paths")
}

func TestRateLimitMiddlewareAllowedRequest(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(time.Minute)
	limiter := &mockLimiter{
		result: ratelimit.Result{
			Allowed:   true,
			Limit:     600,
			Remaining: 599,
			ResetAt:   resetAt,
		},
	}
	mw := RateLimitMiddleware(limiter, nil, slog.New(slog.DiscardHandler))

	var called bool
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodPost, "/mcp/my-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.True(t, called, "allowed requests should reach the next handler")
	require.True(t, limiter.called)
	require.Equal(t, "platform:my-server", limiter.key)

	// Rate limit headers should be present.
	require.Equal(t, "600", w.Header().Get(ratelimit.HeaderRateLimitLimit))
	require.Equal(t, "599", w.Header().Get(ratelimit.HeaderRateLimitRemaining))
	require.NotEmpty(t, w.Header().Get(ratelimit.HeaderRateLimitReset))
}

func TestRateLimitMiddlewareBlockedRequest(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(30 * time.Second)
	limiter := &mockLimiter{
		result: ratelimit.Result{
			Allowed:   false,
			Limit:     600,
			Remaining: 0,
			ResetAt:   resetAt,
		},
	}
	mw := RateLimitMiddleware(limiter, nil, slog.New(slog.DiscardHandler))

	var called bool
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodPost, "/mcp/my-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.False(t, called, "blocked requests should NOT reach the next handler")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body rateLimitErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)
	require.Equal(t, "rate limit exceeded", body.Error)
	require.Greater(t, body.RetryAfter, 0)
}

func TestRateLimitMiddlewareFailsOpenOnLimiterError(t *testing.T) {
	t.Parallel()

	limiter := &mockLimiter{
		err: fmt.Errorf("redis connection refused"),
	}
	mw := RateLimitMiddleware(limiter, nil, slog.New(slog.DiscardHandler))

	var called bool
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodPost, "/mcp/my-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.True(t, called, "should fail open and pass request through")
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRateLimitMiddlewareUsesConfigOverride(t *testing.T) {
	t.Parallel()

	limiter := &mockLimiter{
		result: ratelimit.Result{
			Allowed:   true,
			Limit:     1000,
			Remaining: 999,
			ResetAt:   time.Now().Add(time.Minute),
		},
	}
	configLoader := &mockConfigLoader{limit: 1000}
	mw := RateLimitMiddleware(limiter, configLoader, slog.New(slog.DiscardHandler))

	var called bool
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodPost, "/mcp/my-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, "1000", w.Header().Get(ratelimit.HeaderRateLimitLimit))
}

func TestRateLimitMiddlewareAuthenticatedRouteKey(t *testing.T) {
	t.Parallel()

	limiter := &mockLimiter{
		result: ratelimit.Result{
			Allowed:   true,
			Limit:     600,
			Remaining: 599,
			ResetAt:   time.Now().Add(time.Minute),
		},
	}
	mw := RateLimitMiddleware(limiter, nil, slog.New(slog.DiscardHandler))

	var called bool
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodPost, "/mcp/my-project/my-toolset/production", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, "platform:my-project:my-toolset", limiter.key)
}

// --- resolveLimit tests ---

func TestResolveLimitNilConfigLoader(t *testing.T) {
	t.Parallel()

	limit, err := resolveLimit(context.Background(), nil, "my-slug")
	require.NoError(t, err)
	require.Equal(t, defaultPlatformRateLimit, limit)
}

func TestResolveLimitNoOverride(t *testing.T) {
	t.Parallel()

	loader := &mockConfigLoader{limit: 0}
	limit, err := resolveLimit(context.Background(), loader, "my-slug")
	require.NoError(t, err)
	require.Equal(t, defaultPlatformRateLimit, limit)
}

func TestResolveLimitWithOverride(t *testing.T) {
	t.Parallel()

	loader := &mockConfigLoader{limit: 1000}
	limit, err := resolveLimit(context.Background(), loader, "my-slug")
	require.NoError(t, err)
	require.Equal(t, 1000, limit)
}

func TestResolveLimitConfigError(t *testing.T) {
	t.Parallel()

	loader := &mockConfigLoader{err: fmt.Errorf("db connection lost")}
	_, err := resolveLimit(context.Background(), loader, "my-slug")
	require.Error(t, err)
}
