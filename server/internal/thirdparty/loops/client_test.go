package loops

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNew_NoopWhenAPIKeyEmpty(t *testing.T) {
	t.Parallel()

	client := New(t.Context(), testenv.NewLogger(t), newTestPolicy(t), "")
	_, ok := client.(*noopClient)
	require.True(t, ok, "expected noop client when API key is empty")
}

func TestNew_NoopWhenAPIKeyUnset(t *testing.T) {
	t.Parallel()

	client := New(t.Context(), testenv.NewLogger(t), newTestPolicy(t), "unset")
	_, ok := client.(*noopClient)
	require.True(t, ok, "expected noop client when API key is the unset placeholder")
}

func TestNew_HTTPWhenAPIKeyConfigured(t *testing.T) {
	t.Parallel()

	client := New(t.Context(), testenv.NewLogger(t), newTestPolicy(t), "secret-key")
	_, ok := client.(*httpClient)
	require.True(t, ok, "expected http client when API key is configured")
}

func TestNoopClient_SendTransactional_DropsAndReturnsNil(t *testing.T) {
	t.Parallel()

	client := New(t.Context(), testenv.NewLogger(t), newTestPolicy(t), "")
	err := client.SendTransactional(t.Context(), SendTransactionalInput{
		TransactionalID: "tid-123",
		Email:           "user@example.com",
		DataVariables:   map[string]string{"foo": "bar"},
		AddToAudience:   true,
	})
	require.NoError(t, err)
}

func TestHTTPClient_SendTransactional_SendsExpectedRequest(t *testing.T) {
	t.Parallel()

	type capture struct {
		mu          sync.Mutex
		body        transactionalRequest
		authHeader  string
		contentType string
		readErr     error
		decodeErr   error
	}
	captured := &capture{
		mu:          sync.Mutex{},
		body:        transactionalRequest{TransactionalID: "", Email: "", DataVariables: nil, AddToAudience: false},
		authHeader:  "",
		contentType: "",
		readErr:     nil,
		decodeErr:   nil,
	}
	var calls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, err := io.ReadAll(r.Body)
		captured.mu.Lock()
		captured.authHeader = r.Header.Get("Authorization")
		captured.contentType = r.Header.Get("Content-Type")
		captured.readErr = err
		if err == nil {
			captured.decodeErr = json.Unmarshal(body, &captured.body)
		}
		captured.mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(server.Close)

	c := newTestHTTPClient(t, server.URL, "secret-key")

	err := c.SendTransactional(t.Context(), SendTransactionalInput{
		TransactionalID: "tid-abc",
		Email:           "alice@example.com",
		DataVariables:   map[string]string{"workspace_name": "Acme"},
		AddToAudience:   true,
	})
	require.NoError(t, err)

	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
	captured.mu.Lock()
	defer captured.mu.Unlock()
	require.NoError(t, captured.readErr, "handler failed reading body")
	require.NoError(t, captured.decodeErr, "handler failed decoding body")
	require.Equal(t, "Bearer secret-key", captured.authHeader)
	require.Equal(t, "application/json", captured.contentType)
	require.Equal(t, "tid-abc", captured.body.TransactionalID)
	require.Equal(t, "alice@example.com", captured.body.Email)
	require.Equal(t, map[string]string{"workspace_name": "Acme"}, captured.body.DataVariables)
	require.True(t, captured.body.AddToAudience)
}

func TestHTTPClient_SendTransactional_ErrorOnNon200(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"bad token"}`))
	}))
	t.Cleanup(server.Close)

	c := newTestHTTPClient(t, server.URL, "bad-key")

	err := c.SendTransactional(t.Context(), SendTransactionalInput{
		TransactionalID: "tid",
		Email:           "user@example.com",
		DataVariables:   nil,
		AddToAudience:   false,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 401")
	require.Contains(t, err.Error(), "bad token")
}

func TestHTTPClient_SendTransactional_ErrorOnAPIFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":false,"message":"template not found"}`))
	}))
	t.Cleanup(server.Close)

	c := newTestHTTPClient(t, server.URL, "secret-key")

	err := c.SendTransactional(t.Context(), SendTransactionalInput{
		TransactionalID: "missing",
		Email:           "user@example.com",
		DataVariables:   nil,
		AddToAudience:   false,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "template not found")
}

func TestHTTPClient_SendTransactional_ErrorOnInvalidJSONResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	t.Cleanup(server.Close)

	c := newTestHTTPClient(t, server.URL, "secret-key")

	err := c.SendTransactional(t.Context(), SendTransactionalInput{
		TransactionalID: "tid",
		Email:           "user@example.com",
		DataVariables:   nil,
		AddToAudience:   false,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode transactional response")
}

func TestHTTPClient_SendTransactional_ContextCancelled(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	c := newTestHTTPClient(t, server.URL, "secret-key")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := c.SendTransactional(ctx, SendTransactionalInput{
		TransactionalID: "tid",
		Email:           "user@example.com",
		DataVariables:   nil,
		AddToAudience:   false,
	})
	require.Error(t, err)
}

func TestHTTPClient_SendTransactional_OmitsEmptyVariables(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		rawBody []byte
		readErr error
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		mu.Lock()
		rawBody = body
		readErr = err
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(server.Close)

	c := newTestHTTPClient(t, server.URL, "secret-key")

	err := c.SendTransactional(t.Context(), SendTransactionalInput{
		TransactionalID: "tid",
		Email:           "user@example.com",
		DataVariables:   nil,
		AddToAudience:   false,
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NoError(t, readErr)
	require.NotContains(t, string(rawBody), "dataVariables")
	require.NotContains(t, string(rawBody), "addToAudience")
}

func newTestPolicy(t *testing.T) *guardian.Policy {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return policy
}

func newTestHTTPClient(t *testing.T, baseURL, apiKey string) *httpClient {
	t.Helper()
	policy := newTestPolicy(t)
	return &httpClient{
		logger:     testenv.NewLogger(t),
		httpClient: policy.PooledClient(),
		baseURL:    baseURL,
		apiKey:     apiKey,
	}
}
