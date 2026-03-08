package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

// mockLimiter is a test double for ratelimit.Limiter.
type mockLimiter struct {
	result ratelimit.Result
	err    error
}

func (m *mockLimiter) Allow(_ context.Context, _ string, _ int) (ratelimit.Result, error) {
	return m.result, m.err
}

func TestWriteJSONRPCRateLimitErrorResponseShape(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	resetAt := time.Now().Add(30 * time.Second)
	result := ratelimit.Result{
		Allowed:   false,
		Limit:     100,
		Remaining: 0,
		ResetAt:   resetAt,
	}

	err := writeJSONRPCRateLimitError(w, 100, result)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, w.Code, "JSON-RPC errors use HTTP 200")
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	require.Equal(t, "2.0", body["jsonrpc"])
	require.Nil(t, body["id"])

	errObj, ok := body["error"].(map[string]any)
	require.True(t, ok, "error field should be an object")
	require.Equal(t, float64(rateLimitExceeded), errObj["code"])
	require.Contains(t, errObj["message"], "100 requests per minute")

	data, ok := errObj["data"].(map[string]any)
	require.True(t, ok, "error.data should be an object")
	require.Equal(t, "1m", data["window"])
	require.Equal(t, float64(100), data["limit"])
	require.Greater(t, data["retryAfterMs"], float64(0))
}

func TestWriteJSONRPCRateLimitErrorMinRetryAfter(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	// ResetAt in the past — retryAfterMs should be clamped to 1000.
	result := ratelimit.Result{
		Allowed:   false,
		Limit:     10,
		Remaining: 0,
		ResetAt:   time.Now().Add(-5 * time.Second),
	}

	err := writeJSONRPCRateLimitError(w, 10, result)
	require.NoError(t, err)

	var body map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	errObj := body["error"].(map[string]any)
	data := errObj["data"].(map[string]any)
	require.Equal(t, float64(1000), data["retryAfterMs"], "retryAfterMs should be at least 1000")
}

func TestCheckCustomerRateLimitNilLimiter(t *testing.T) {
	t.Parallel()

	s := &Service{
		rateLimiter: nil,
		logger:      slog.New(slog.DiscardHandler),
	}

	w := httptest.NewRecorder()
	rpm := pgtype.Int4{Int32: 100, Valid: true}

	limited, err := s.checkCustomerRateLimit(context.Background(), w, rpm, "test-key")
	require.NoError(t, err)
	require.False(t, limited, "nil limiter should allow all requests")
}

func TestCheckCustomerRateLimitInvalidRPM(t *testing.T) {
	t.Parallel()

	s := &Service{
		rateLimiter: &mockLimiter{},
		logger:      slog.New(slog.DiscardHandler),
	}

	w := httptest.NewRecorder()

	// Null RPM (not configured).
	limited, err := s.checkCustomerRateLimit(context.Background(), w, pgtype.Int4{Valid: false}, "test-key")
	require.NoError(t, err)
	require.False(t, limited, "null RPM should skip rate limiting")
}

func TestCheckCustomerRateLimitZeroRPM(t *testing.T) {
	t.Parallel()

	s := &Service{
		rateLimiter: &mockLimiter{},
		logger:      slog.New(slog.DiscardHandler),
	}

	w := httptest.NewRecorder()

	// Zero RPM should be treated as unconfigured.
	limited, err := s.checkCustomerRateLimit(context.Background(), w, pgtype.Int4{Int32: 0, Valid: true}, "test-key")
	require.NoError(t, err)
	require.False(t, limited, "zero RPM should skip rate limiting")
}

func TestCheckCustomerRateLimitAllowed(t *testing.T) {
	t.Parallel()

	meter := noop.NewMeterProvider().Meter("test")
	s := &Service{
		rateLimiter: &mockLimiter{
			result: ratelimit.Result{
				Allowed:   true,
				Limit:     100,
				Remaining: 99,
				ResetAt:   time.Now().Add(time.Minute),
			},
		},
		logger:  slog.New(slog.DiscardHandler),
		metrics: newMetrics(meter, slog.New(slog.DiscardHandler)),
	}

	w := httptest.NewRecorder()
	rpm := pgtype.Int4{Int32: 100, Valid: true}

	limited, err := s.checkCustomerRateLimit(context.Background(), w, rpm, "test-key")
	require.NoError(t, err)
	require.False(t, limited)

	// Rate limit headers should be set.
	require.Equal(t, "100", w.Header().Get(ratelimit.HeaderRateLimitLimit))
	require.Equal(t, "99", w.Header().Get(ratelimit.HeaderRateLimitRemaining))
	require.NotEmpty(t, w.Header().Get(ratelimit.HeaderRateLimitReset))
}

func TestCheckCustomerRateLimitBlocked(t *testing.T) {
	t.Parallel()

	meter := noop.NewMeterProvider().Meter("test")
	s := &Service{
		rateLimiter: &mockLimiter{
			result: ratelimit.Result{
				Allowed:   false,
				Limit:     50,
				Remaining: 0,
				ResetAt:   time.Now().Add(30 * time.Second),
			},
		},
		logger:  slog.New(slog.DiscardHandler),
		metrics: newMetrics(meter, slog.New(slog.DiscardHandler)),
	}

	w := httptest.NewRecorder()
	rpm := pgtype.Int4{Int32: 50, Valid: true}

	limited, err := s.checkCustomerRateLimit(context.Background(), w, rpm, "test-key")
	require.NoError(t, err)
	require.True(t, limited, "should indicate request was rate limited")

	// Verify the JSON-RPC error was written.
	require.Equal(t, http.StatusOK, w.Code, "JSON-RPC errors use HTTP 200")

	var body map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)
	require.Equal(t, "2.0", body["jsonrpc"])

	errObj := body["error"].(map[string]any)
	require.Equal(t, float64(rateLimitExceeded), errObj["code"])
}

func TestCheckCustomerRateLimitFailOpen(t *testing.T) {
	t.Parallel()

	s := &Service{
		rateLimiter: &mockLimiter{
			err: fmt.Errorf("redis connection refused"),
		},
		logger: slog.New(slog.DiscardHandler),
	}

	w := httptest.NewRecorder()
	rpm := pgtype.Int4{Int32: 100, Valid: true}

	limited, err := s.checkCustomerRateLimit(context.Background(), w, rpm, "test-key")
	require.NoError(t, err)
	require.False(t, limited, "should fail open on rate limiter error")
}
