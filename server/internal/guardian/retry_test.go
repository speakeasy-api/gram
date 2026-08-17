package guardian_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func fastRetryConfig() *guardian.RetryConfig {
	cfg := guardian.DefaultRetryConfig()
	cfg.WaitMin = time.Millisecond
	cfg.WaitMax = 5 * time.Millisecond
	cfg.MaxAttempts = 2
	return cfg
}

func TestClient_RetriesExhausted_PreservesFinalStatusAndBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"export pipeline wedged","request_id":"req_123"}`))
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := policy.Client(guardian.WithRetryConfig(fastRetryConfig()))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/logs", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err)

	var exhausted *guardian.RetriesExhaustedError
	require.ErrorAs(t, err, &exhausted)
	require.Equal(t, http.StatusInternalServerError, exhausted.StatusCode)
	require.Equal(t, 3, exhausted.Attempts)
	require.Contains(t, exhausted.Body, "export pipeline wedged")
	require.Contains(t, err.Error(), "giving up after 3 attempt(s)")
	require.Contains(t, err.Error(), "last status 500")
}

func TestClient_ContextCancellationIsNotWrappedAsExhaustion(t *testing.T) {
	t.Parallel()

	// The handler stalls until the client abandons the request; unblocking
	// on the request context keeps server.Close from waiting on it.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := policy.Client(guardian.WithRetryConfig(fastRetryConfig()))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	var exhausted *guardian.RetriesExhaustedError
	require.NotErrorAs(t, err, &exhausted, "caller-side aborts must not look like retry exhaustion")
}

func TestClient_RetriesExhausted_RedactsPasswordAndControlChars(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("line one\nline two\x07"))
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := policy.Client(guardian.WithRetryConfig(fastRetryConfig()))

	authedURL := strings.Replace(server.URL, "http://", "http://user:hunter2@", 1)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, authedURL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err)

	var exhausted *guardian.RetriesExhaustedError
	require.ErrorAs(t, err, &exhausted)
	require.NotContains(t, exhausted.Error(), "hunter2")
	require.Equal(t, "line one line two ", exhausted.Body)
}

func TestClient_RetriesExhausted_TransportFailureHasNoStatus(t *testing.T) {
	t.Parallel()

	// A server that is already closed yields a connection error on every
	// attempt, so exhaustion happens without ever seeing a response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := policy.Client(guardian.WithRetryConfig(fastRetryConfig()))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err)

	var exhausted *guardian.RetriesExhaustedError
	require.ErrorAs(t, err, &exhausted)
	require.Equal(t, 0, exhausted.StatusCode)
	require.Empty(t, exhausted.Body)
	require.Error(t, exhausted.Err)
}

func TestClient_CustomErrorHandlerStillWins(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	sentinel := errors.New("custom handler ran")
	cfg := fastRetryConfig()
	cfg.ErrorHandler = func(resp *http.Response, err error, numTries int) (*http.Response, error) {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, sentinel
	}
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := policy.Client(guardian.WithRetryConfig(cfg))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.ErrorIs(t, err, sentinel)
}
