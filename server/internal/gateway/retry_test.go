package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		statusCodes:     nil,
		methods:         nil,
	}
}

func okResponse() *http.Response {
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusOK)
	resp := rec.Result()
	resp.Request = httptest.NewRequest(http.MethodPost, "https://app.fly.dev/tool-call", http.NoBody)
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
