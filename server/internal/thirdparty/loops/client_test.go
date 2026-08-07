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

	"github.com/stretchr/testify/assert"
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
		DataVariables:   map[string]string{"organization_name": "Acme"},
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
	require.Equal(t, map[string]string{"organization_name": "Acme"}, captured.body.DataVariables)
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

func TestNewWorkflowClient_NoopWhenAPIKeyIsNotConfigured(t *testing.T) {
	t.Parallel()

	for _, apiKey := range []string{"", "unset"} {
		client := NewWorkflowClient(t.Context(), testenv.NewLogger(t), newTestPolicy(t), apiKey)
		require.IsType(t, &noopClient{}, client)
		require.NoError(t, client.UpdateContact(t.Context(), UpdateContactInput{Email: "<EMAIL>"}))
		require.NoError(t, client.SendEvent(t.Context(), SendEventInput{Email: "<EMAIL>", EventName: "trial_started"}))
		contact, err := client.FindContact(t.Context(), FindContactInput{Email: "<EMAIL>"})
		require.NoError(t, err)
		require.Nil(t, contact)
	}
}

func TestHTTPClient_FindContact_SendsEncodedQueryAndDecodesContact(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "<EMAIL>@example.com", r.URL.Query().Get("email"))
		assert.Empty(t, r.URL.Query().Get("userId"))
		assert.Equal(t, "Bearer workflow-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`[{"email":"<EMAIL>@example.com","firstName":"Ada","userId":"<USER_ID>","subscribed":true}]`))
	}))
	t.Cleanup(server.Close)

	c := newTestHTTPClient(t, server.URL, "workflow-key")
	contact, err := c.FindContact(t.Context(), FindContactInput{Email: "<EMAIL>@example.com"})
	require.NoError(t, err)
	require.NotNil(t, contact)
	require.Equal(t, "<EMAIL>@example.com", contact.Email)
	require.Equal(t, "Ada", *contact.FirstName)
	require.Equal(t, "<USER_ID>", *contact.UserID)
	require.True(t, contact.Subscribed)
}

func TestHTTPClient_FindContact_ReturnsNilWhenNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	contact, err := newTestHTTPClient(t, server.URL, "workflow-key").FindContact(t.Context(), FindContactInput{UserID: "<USER_ID>"})
	require.NoError(t, err)
	require.Nil(t, contact)
}

func TestHTTPClient_UpdateContact_SendsTypedCamelCaseProperties(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/contacts/update", r.URL.Path)
		assert.Equal(t, "Bearer workflow-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "<EMAIL>@example.com", body["email"])
		assert.Equal(t, "<USER_ID>", body["userId"])
		assert.Equal(t, "Ada", body["firstName"])
		assert.Equal(t, "<ORG_NAME>", body["organizationName"])
		assert.Equal(t, true, body["trialActive"])
		assert.InDelta(t, float64(14), body["trialDays"], 0)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(server.Close)

	firstName := "Ada"
	err := newTestHTTPClient(t, server.URL, "workflow-key").UpdateContact(t.Context(), UpdateContactInput{
		Email:     "<EMAIL>@example.com",
		FirstName: &firstName,
		UserID:    "<USER_ID>",
		CustomProperties: map[string]any{
			"organizationName": "<ORG_NAME>",
			"trialActive":      true,
			"trialDays":        14,
		},
	})
	require.NoError(t, err)
}

func TestHTTPClient_SendEvent_SendsPropertiesAuthAndIdempotencyKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/events/send", r.URL.Path)
		assert.Equal(t, "Bearer workflow-key", r.Header.Get("Authorization"))
		assert.Equal(t, "event-key", r.Header.Get("Idempotency-Key"))
		var body eventRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "<EMAIL>@example.com", body.Email)
		assert.Equal(t, "<USER_ID>", body.UserID)
		assert.Equal(t, "trial_started", body.EventName)
		assert.Equal(t, map[string]any{"trialEndsAt": "<TRIAL_ENDS_AT>"}, body.EventProperties)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(server.Close)

	err := newTestHTTPClient(t, server.URL, "workflow-key").SendEvent(t.Context(), SendEventInput{
		Email:           "<EMAIL>@example.com",
		UserID:          "<USER_ID>",
		EventName:       "trial_started",
		EventProperties: map[string]any{"trialEndsAt": "<TRIAL_ENDS_AT>"},
		IdempotencyKey:  "event-key",
	})
	require.NoError(t, err)
}

func TestHTTPClient_SendEvent_TreatsDuplicateAsSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"success":false,"message":"duplicate"}`))
	}))
	t.Cleanup(server.Close)

	err := newTestHTTPClient(t, server.URL, "workflow-key").SendEvent(t.Context(), SendEventInput{
		Email:          "<EMAIL>@example.com",
		EventName:      "trial_started",
		IdempotencyKey: "event-key",
	})
	require.NoError(t, err)
}

func TestHTTPClient_WorkflowMethodsReturnAPIErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"message":"invalid property"}`))
	}))
	t.Cleanup(server.Close)

	c := newTestHTTPClient(t, server.URL, "workflow-key")
	err := c.UpdateContact(t.Context(), UpdateContactInput{Email: "<EMAIL>@example.com"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 400")
	require.Contains(t, err.Error(), "invalid property")
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
