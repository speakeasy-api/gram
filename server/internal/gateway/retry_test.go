package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsRetryableTransportError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "eof", err: io.EOF, want: true},
		{name: "eof wrapped in url.Error", err: &url.Error{Op: "Post", URL: "https://app.fly.dev/tool-call", Err: io.EOF}, want: true},
		{name: "unexpected eof not retried", err: io.ErrUnexpectedEOF, want: false},
		{name: "connection refused", err: syscall.ECONNREFUSED, want: true},
		{name: "dial reset retried", err: &net.OpError{Op: "dial", Err: syscall.ECONNRESET}, want: true},
		{name: "read reset not retried", err: &net.OpError{Op: "read", Err: syscall.ECONNRESET}, want: false},
		{name: "context canceled", err: context.Canceled, want: false},
		{name: "context deadline exceeded", err: context.DeadlineExceeded, want: false},
		{name: "context canceled wrapped in url.Error", err: &url.Error{Op: "Post", URL: "https://app.fly.dev/tool-call", Err: context.Canceled}, want: false},
		{name: "generic error", err: errors.New("boom"), want: false},
	}

	for _, tc := range cases {
		got := isRetryableTransportError(tc.err)
		require.Equalf(t, tc.want, got, "isRetryableTransportError(%v)", tc.err)
	}
}

func fastRetryConfig() retryConfig {
	return retryConfig{
		initialInterval: time.Millisecond,
		maxInterval:     time.Millisecond,
		maxAttempts:     3,
		backoffFactor:   2,
		retryableStatus: nil,
	}
}

// fastRetryConfigWith returns fastRetryConfig with the given per-method
// retryable-status map, so status-path tests retry within microseconds instead
// of the production second-scale waits.
func fastRetryConfigWith(retryableStatus map[string][]int) retryConfig {
	cfg := fastRetryConfig()
	cfg.retryableStatus = retryableStatus
	return cfg
}

func okResponse() *http.Response {
	return statusResponse(http.MethodPost, http.StatusOK)
}

func statusResponse(method string, status int) *http.Response {
	rec := httptest.NewRecorder()
	rec.WriteHeader(status)
	resp := rec.Result()
	resp.Request = httptest.NewRequest(method, "https://app.fly.dev/tool-call", http.NoBody)
	return resp
}

// countingBody is a response body that records how many times it is closed, so
// tests can assert the retry loop closes the responses it discards.
type countingBody struct {
	io.Reader
	closes *int
}

func (b countingBody) Close() error {
	*b.closes++
	return nil
}

// retryableStatusResponse builds a response whose body increments *closes when
// closed, for asserting the retry loop drains and closes discarded responses.
func retryableStatusResponse(method string, status int, closes *int) *http.Response {
	resp := statusResponse(method, status)
	resp.Body = countingBody{Reader: strings.NewReader("runner at capacity"), closes: closes}
	return resp
}

func TestRetryWithBackoff_RescuesTransportErrorThenSucceeds(t *testing.T) {
	t.Parallel()

	calls := 0
	resp, err := retryWithBackoff(t.Context(), fastRetryConfig(), func() (*http.Response, error) {
		calls++
		if calls < 3 {
			return nil, &url.Error{Op: "Post", URL: "https://app.fly.dev/tool-call", Err: io.EOF}
		}
		return okResponse(), nil
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 3, calls, "should retry the two EOFs then return the success")
	require.NoError(t, resp.Body.Close())
}

func TestRetryWithBackoff_ExhaustsAttemptsOnTransportError(t *testing.T) {
	t.Parallel()

	calls := 0
	resp, err := retryWithBackoff(t.Context(), fastRetryConfig(), func() (*http.Response, error) {
		calls++
		return nil, &url.Error{Op: "Post", URL: "https://app.fly.dev/tool-call", Err: io.EOF}
	})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	require.Error(t, err)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, resp)
	require.Equal(t, 3, calls, "transport errors should be retried up to maxAttempts")
}

func TestRetryWithBackoff_DoesNotRetryNonTransportError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("response body read failed")
	calls := 0
	resp, err := retryWithBackoff(t.Context(), fastRetryConfig(), func() (*http.Response, error) {
		calls++
		return nil, wantErr
	})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	require.ErrorIs(t, err, wantErr)
	require.Nil(t, resp)
	require.Equal(t, 1, calls, "a non-transport error must not be retried (could double-execute a POST)")
}

func TestRetryWithBackoff_DoesNotRetryContextCanceled(t *testing.T) {
	t.Parallel()

	calls := 0
	resp, err := retryWithBackoff(t.Context(), fastRetryConfig(), func() (*http.Response, error) {
		calls++
		return nil, context.Canceled
	})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, resp)
	require.Equal(t, 1, calls, "caller cancellation must not trigger a retry")
}

func TestRetryWithBackoff_FunctionPOSTRetriesOn429(t *testing.T) {
	t.Parallel()

	calls := 0
	resp, err := retryWithBackoff(t.Context(), fastRetryConfigWith(map[string][]int{
		http.MethodPost: {http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}), func() (*http.Response, error) {
		calls++
		if calls < 3 {
			return statusResponse(http.MethodPost, http.StatusTooManyRequests), nil
		}
		return okResponse(), nil
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 3, calls, "a function POST should retry 429 until it succeeds")
	require.NoError(t, resp.Body.Close())
}

func TestRetryWithBackoff_FunctionPOSTRetriesOn503(t *testing.T) {
	t.Parallel()

	calls := 0
	resp, err := retryWithBackoff(t.Context(), fastRetryConfigWith(map[string][]int{
		http.MethodPost: {http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}), func() (*http.Response, error) {
		calls++
		if calls < 2 {
			return statusResponse(http.MethodPost, http.StatusServiceUnavailable), nil
		}
		return okResponse(), nil
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 2, calls, "a function POST should retry 503 until it succeeds")
	require.NoError(t, resp.Body.Close())
}

// Server errors that may be post-execution must never be retried for a
// non-idempotent function POST, or a tool call could be executed twice.
func TestRetryWithBackoff_FunctionPOSTDoesNotRetryServerErrors(t *testing.T) {
	t.Parallel()

	for _, status := range []int{
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusGatewayTimeout,      // 504
	} {
		calls := 0
		resp, err := retryWithBackoff(t.Context(), fastRetryConfigWith(map[string][]int{
			http.MethodPost: {http.StatusTooManyRequests, http.StatusServiceUnavailable},
		}), func() (*http.Response, error) {
			calls++
			return statusResponse(http.MethodPost, status), nil
		})

		require.NoErrorf(t, err, "status %d", status)
		require.NotNilf(t, resp, "status %d", status)
		require.Equalf(t, status, resp.StatusCode, "status %d", status)
		require.Equalf(t, 1, calls, "function POST must not retry %d (risks double-execution)", status)
		require.NoErrorf(t, resp.Body.Close(), "status %d", status)
	}
}

// The runner advertises Retry-After alongside its 429; the status path must
// honor it and still retry.
func TestRetryWithBackoff_FunctionPOSTHonorsRetryAfterOn429(t *testing.T) {
	t.Parallel()

	calls := 0
	resp, err := retryWithBackoff(t.Context(), fastRetryConfigWith(map[string][]int{
		http.MethodPost: {http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}), func() (*http.Response, error) {
		calls++
		if calls < 2 {
			r := statusResponse(http.MethodPost, http.StatusTooManyRequests)
			r.Header.Set("Retry-After", "2")
			return r, nil
		}
		return okResponse(), nil
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 2, calls, "a 429 carrying Retry-After should still be retried")
	require.NoError(t, resp.Body.Close())
}

// Regression guard: idempotent HTTP GET tool calls keep retrying the broad
// transient-status preset.
func TestRetryWithBackoff_HTTPGetRetriesTransientStatus(t *testing.T) {
	t.Parallel()

	cfg := httpToolRetryConfig()
	cfg.initialInterval = time.Millisecond
	cfg.maxInterval = time.Millisecond

	calls := 0
	resp, err := retryWithBackoff(t.Context(), cfg, func() (*http.Response, error) {
		calls++
		if calls < 3 {
			return statusResponse(http.MethodGet, http.StatusServiceUnavailable), nil
		}
		return statusResponse(http.MethodGet, http.StatusOK), nil
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 3, calls, "an HTTP GET should retry the transient-status preset")
	require.NoError(t, resp.Body.Close())
}

// Scope guard: the HTTP tool policy must NOT retry POSTs on the runner's
// throttle statuses. An upstream 429/503 is not provably pre-execution for
// arbitrary APIs, so retrying a non-idempotent HTTP POST could double-execute it.
func TestRetryWithBackoff_HTTPPostNotRetriedOnThrottleStatus(t *testing.T) {
	t.Parallel()

	cfg := httpToolRetryConfig()
	cfg.initialInterval = time.Millisecond
	cfg.maxInterval = time.Millisecond

	for _, status := range []int{
		http.StatusTooManyRequests,    // 429
		http.StatusServiceUnavailable, // 503
	} {
		calls := 0
		resp, err := retryWithBackoff(t.Context(), cfg, func() (*http.Response, error) {
			calls++
			return statusResponse(http.MethodPost, status), nil
		})

		require.NoErrorf(t, err, "status %d", status)
		require.NotNilf(t, resp, "status %d", status)
		require.Equalf(t, status, resp.StatusCode, "status %d", status)
		require.Equalf(t, 1, calls, "the HTTP tool policy must not retry POSTs on %d", status)
		require.NoErrorf(t, resp.Body.Close(), "status %d", status)
	}
}

// A retried 429/503 response must have its body drained and closed before the
// next attempt, or each retry leaks the body and its keep-alive connection. The
// final returned response stays open for the caller.
func TestRetryWithBackoff_ClosesDiscardedResponseBodies(t *testing.T) {
	t.Parallel()

	closes := 0
	calls := 0
	resp, err := retryWithBackoff(t.Context(), fastRetryConfigWith(map[string][]int{
		http.MethodPost: {http.StatusTooManyRequests},
	}), func() (*http.Response, error) {
		calls++
		if calls < 3 {
			return retryableStatusResponse(http.MethodPost, http.StatusTooManyRequests, &closes), nil
		}
		return okResponse(), nil
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 3, calls)
	require.Equal(t, 2, closes, "each discarded 429 response body must be closed to avoid leaking the connection")
	require.NoError(t, resp.Body.Close())
}

// A zero-value config (no caller wired a RetryConfig) must still run the request
// exactly once: maxAttempts clamps up to 1, and a nil retryableStatus retries
// nothing on status.
func TestRetryWithBackoff_ZeroValueConfigRunsOnceAndNeverRetriesStatus(t *testing.T) {
	t.Parallel()

	calls := 0
	resp, err := retryWithBackoff(t.Context(), retryConfig{}, func() (*http.Response, error) {
		calls++
		return statusResponse(http.MethodPost, http.StatusTooManyRequests), nil
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.Equal(t, 1, calls, "a zero-value config must run exactly once and never retry on status")
	require.NoError(t, resp.Body.Close())
}

func TestJitteredBackoff_StaysWithinCapAndSpreads(t *testing.T) {
	t.Parallel()

	require.Zero(t, jitteredBackoff(0), "non-positive base yields no delay")
	require.Zero(t, jitteredBackoff(-time.Second), "negative base yields no delay")

	const base = 2 * time.Second
	seen := make(map[time.Duration]struct{})
	for range 1000 {
		got := jitteredBackoff(base)
		// Full jitter on top of base as a floor: uniform in [base, 2*base).
		require.GreaterOrEqual(t, got, base, "jittered delay must never drop below the floor")
		require.Less(t, got, 2*base, "jittered delay must stay within the 2*base cap")
		seen[got] = struct{}{}
	}
	require.Greater(t, len(seen), 1, "jitter must spread delays, not return a constant")
}
