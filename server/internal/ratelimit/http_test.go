package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fakeStore struct {
	mu                  sync.Mutex
	takeResults         []Result
	takeErr             error
	retryAfterDurations []time.Duration
	retryAfterErr       error
	setDurations        []time.Duration
	setErr              error
}

func (s *fakeStore) take(context.Context, string, Rate, int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.takeErr != nil {
		return Result{Allowed: false, Remaining: 0, RetryAfter: 0}, s.takeErr
	}
	result := s.takeResults[0]
	s.takeResults = s.takeResults[1:]
	return result, nil
}

func (s *fakeStore) retryAfter(context.Context, string) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retryAfterErr != nil {
		return 0, s.retryAfterErr
	}
	if len(s.retryAfterDurations) == 0 {
		return 0, nil
	}
	result := s.retryAfterDurations[0]
	s.retryAfterDurations = s.retryAfterDurations[1:]
	return result, nil
}

func (s *fakeStore) setRetryAfter(_ context.Context, _ string, retryAfter time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setDurations = append(s.setDurations, retryAfter)
	return s.setErr
}

type stubRoundTripper struct {
	response *http.Response
	err      error
	calls    int
}

func (s *stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	s.calls++
	return s.response, s.err
}

func testHTTPResponse(statusCode int, header http.Header, body string) *http.Response {
	return &http.Response{
		Status:           fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		StatusCode:       statusCode,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           header,
		Body:             io.NopCloser(strings.NewReader(body)),
		ContentLength:    int64(len(body)),
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}
}

func TestHTTPMiddlewareWaitsForToken(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		takeResults: []Result{
			{Allowed: false, Remaining: 0, RetryAfter: time.Nanosecond},
			{Allowed: true, Remaining: 1, RetryAfter: 0},
		},
		takeErr:             nil,
		retryAfterDurations: nil,
		retryAfterErr:       nil,
		setDurations:        nil,
		setErr:              nil,
	}
	limiter := New(store, "vendor", PerSecond(10).WithBurst(1))
	next := &stubRoundTripper{
		response: testHTTPResponse(http.StatusNoContent, make(http.Header), ""),
		err:      nil,
		calls:    0,
	}
	transport := HTTPMiddleware(testenv.NewLogger(t), limiter, StaticHTTPKey("account"))(next)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, 1, next.calls)
}

func TestHTTPMiddlewareSharesRetryAfter(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		takeResults:         []Result{{Allowed: true, Remaining: 1, RetryAfter: 0}},
		takeErr:             nil,
		retryAfterDurations: nil,
		retryAfterErr:       nil,
		setDurations:        nil,
		setErr:              nil,
	}
	limiter := New(store, "vendor", PerSecond(10).WithBurst(1))
	next := &stubRoundTripper{
		response: testHTTPResponse(http.StatusTooManyRequests, http.Header{"Retry-After": []string{"2"}}, "rate limited"),
		err:      nil,
		calls:    0,
	}
	transport := HTTPMiddleware(testenv.NewLogger(t), limiter, StaticHTTPKey("account"))(next)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, []time.Duration{2 * time.Second}, store.setDurations)
}

func TestHTTPMiddlewareFailsOpenWhenStoreUnavailable(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		takeResults:         nil,
		takeErr:             nil,
		retryAfterDurations: nil,
		retryAfterErr:       errors.New("redis unavailable"),
		setDurations:        nil,
		setErr:              nil,
	}
	limiter := New(store, "vendor", PerSecond(10).WithBurst(1))
	next := &stubRoundTripper{
		response: testHTTPResponse(http.StatusNoContent, make(http.Header), ""),
		err:      nil,
		calls:    0,
	}
	transport := HTTPMiddleware(testenv.NewLogger(t), limiter, StaticHTTPKey("account"))(next)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, 1, next.calls)
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)
	require.Equal(t, 5*time.Second, parseRetryAfter("5", now))
	require.Equal(t, 10*time.Second, parseRetryAfter(now.Add(10*time.Second).Format(http.TimeFormat), now))
	require.Zero(t, parseRetryAfter("invalid", now))
	require.Zero(t, parseRetryAfter("-1", now))
}

func TestResponseRetryAfterFromRateLimitReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)
	headers := make(http.Header)
	headers.Set("X-RateLimit-Remaining", "0")
	headers.Set("X-RateLimit-Reset", "1783684830")
	resp := testHTTPResponse(http.StatusOK, headers, "")
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })

	require.Equal(t, 30*time.Second, responseRetryAfter(resp, now))
}
