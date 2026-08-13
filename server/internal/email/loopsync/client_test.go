package loopsync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListsAndCreatesTransactionalEmails(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/transactional-emails", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		switch r.Method {
		case http.MethodGet:
			assert.Equal(t, "50", r.URL.Query().Get("perPage"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pagination":{"nextCursor":null},"data":[{"id":"id-1","name":"gram.transactional.v2.team_invite"}]}`))
		case http.MethodPost:
			var body map[string]string
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, "gram.transactional.v2.team_invite", body["name"])
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"id-2","name":"gram.transactional.v2.team_invite"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := NewClient(server.URL+"/api/v1", "test-key", server.Client())

	listed, err := client.ListTransactionalEmails(t.Context())
	require.NoError(t, err)
	require.Equal(t, []TransactionalEmail{{
		ID:                                 "id-1",
		Name:                               "gram.transactional.v2.team_invite",
		DraftEmailMessageID:                nil,
		DraftEmailMessageContentRevisionID: nil,
		PublishedEmailMessageID:            nil,
		DataVariables:                      nil,
	}}, listed)

	created, err := client.CreateTransactionalEmail(t.Context(), "gram.transactional.v2.team_invite")
	require.NoError(t, err)
	require.Equal(t, "id-2", created.ID)
}

func TestClient_TransactionalLifecycle(t *testing.T) {
	t.Parallel()

	draftID := "message-1"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/transactional-emails/id-1", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = w.Write([]byte(`{"id":"id-1","name":"gram.transactional.v2.example_notice","draftEmailMessageId":"message-1"}`))
	})
	mux.HandleFunc("/api/v1/transactional-emails/id-1/draft", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		_, _ = w.Write([]byte(`{"id":"id-1","name":"gram.transactional.v2.example_notice","draftEmailMessageId":"message-1"}`))
	})
	mux.HandleFunc("/api/v1/email-messages/message-1", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"message-1","contentRevisionId":"revision-1"}`))
		case http.MethodPost:
			var input UpdateEmailMessageInput
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&input)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, "revision-1", input.ExpectedRevisionID)
			assert.Equal(t, "<Paragraph>{data.resource_name}</Paragraph>", input.LMX)
			_, _ = w.Write([]byte(`{"id":"message-1","contentRevisionId":"revision-2"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/email-messages/message-1/guardian", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = w.Write([]byte(`{"errors":[],"warnings":[]}`))
	})
	mux.HandleFunc("/api/v1/transactional-emails/id-1/publish", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		_, _ = w.Write([]byte(`{"id":"id-1","name":"gram.transactional.v2.example_notice","dataVariables":["resource_name"]}`))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := NewClient(server.URL+"/api/v1", "test-key", server.Client())

	transactional, err := client.GetTransactionalEmail(t.Context(), "id-1")
	require.NoError(t, err)
	require.Equal(t, &draftID, transactional.DraftEmailMessageID)

	transactional, err = client.EnsureDraft(t.Context(), "id-1")
	require.NoError(t, err)
	require.Equal(t, &draftID, transactional.DraftEmailMessageID)

	message, err := client.GetEmailMessage(t.Context(), draftID)
	require.NoError(t, err)
	require.Equal(t, "revision-1", message.ContentRevisionID)

	message, err = client.UpdateEmailMessage(t.Context(), draftID, UpdateEmailMessageInput{
		ExpectedRevisionID: "revision-1",
		Subject:            "Example notice",
		PreviewText:        "Review this notice.",
		FromName:           "Speakeasy",
		FromEmail:          "gram",
		ReplyToEmail:       "gram@speakeasy.com",
		LMX:                "<Paragraph>{data.resource_name}</Paragraph>",
	})
	require.NoError(t, err)
	require.Equal(t, "revision-2", message.ContentRevisionID)

	guardian, err := client.Guardian(t.Context(), draftID)
	require.NoError(t, err)
	require.Empty(t, guardian.Errors)

	published, err := client.Publish(t.Context(), "id-1")
	require.NoError(t, err)
	require.Equal(t, []string{"resource_name"}, published.DataVariables)
}

func TestClient_DoesNotRetryCreate(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "temporary failure", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "test-key", server.Client())
	_, err := client.CreateTransactionalEmail(t.Context(), "gram.transactional.v2.example_notice")
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())
}

func TestClient_RetriesRevisionGuardedUpdate(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"id":"message-1","contentRevisionId":"revision-2"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "test-key", server.Client())
	message, err := client.UpdateEmailMessage(t.Context(), "message-1", UpdateEmailMessageInput{
		ExpectedRevisionID: "revision-1",
		Subject:            "Example notice",
		PreviewText:        "Review this notice.",
		FromName:           "Example Sender",
		FromEmail:          "notifications",
		ReplyToEmail:       "person@example.com",
		LMX:                "<Paragraph>Example</Paragraph>",
	})
	require.NoError(t, err)
	require.Equal(t, "revision-2", message.ContentRevisionID)
	require.Equal(t, int32(2), calls.Load())
}

func TestRetryDelay_HonorsHTTPDateBelowCap(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	retryAt := now.Add(15 * time.Second)
	require.Equal(t, 15*time.Second, retryDelay(now, 0, retryAt.Format(http.TimeFormat)))
}

func TestRetryDelay_CapsServerDelay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	require.Equal(t, maxRetryDelay, retryDelay(now, 0, "3600"))
	require.Equal(t, maxRetryDelay, retryDelay(now, 0, now.Add(time.Hour).Format(http.TimeFormat)))
}
