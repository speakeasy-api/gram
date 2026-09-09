package anthropicinference

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestStoreDeduplicatesGrowingTranscripts(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	userID, err := store.ResolveActor(t.Context(), config, frame.Actor)
	require.NoError(t, err)
	require.Empty(t, userID)
	require.NoError(t, store.Save(t.Context(), config, frame, userID))
	require.NoError(t, store.Save(t.Context(), config, frame, userID))
	frame.RequestID = "next-request"
	frame.Messages = append(frame.Messages,
		Message{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"EXAMPLE reply"}]`)},
		Message{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"EXAMPLE second prompt"}]`)},
	)
	require.NoError(t, store.Save(t.Context(), config, frame, userID))
	require.NoError(t, store.Save(t.Context(), config, frame, userID))
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 3)
	require.Equal(t, "EXAMPLE prompt", messages[0].Content)
	require.Equal(t, "EXAMPLE reply", messages[1].Content)
	require.Equal(t, "EXAMPLE second prompt", messages[2].Content)
	require.JSONEq(t, string(frame.Messages[0].Content), string(messages[0].ContentRaw))
}

func TestStoreRejectsCrossOrganizationProject(t *testing.T) {
	t.Parallel()
	store, _, config := newTestStore(t)
	config.OrganizationID = "org_other_example"
	_, err := store.ResolveActor(t.Context(), config, exampleFrame().Actor)
	require.Error(t, err)
}

func TestConversationIdentitySeparatesActorsAndProjects(t *testing.T) {
	t.Parallel()
	config := Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}
	frame := exampleFrame()
	first := conversationID(config, frame)
	frame.Actor.ID = "other-actor"
	require.NotEqual(t, first, conversationID(config, frame))
	frame = exampleFrame()
	config.ProjectID = uuid.New()
	require.NotEqual(t, first, conversationID(config, frame))
}

func TestConversationWithoutSessionIsRequestScoped(t *testing.T) {
	t.Parallel()
	config := Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}
	frame := exampleFrame()
	frame.SessionID = ""
	first := conversationID(config, frame)
	require.Equal(t, first, conversationID(config, frame))
	frame.RequestID = "next-request"
	require.NotEqual(t, first, conversationID(config, frame))
}

func TestConversationIdentityIgnoresOptionalEmailWhenActorIDExists(t *testing.T) {
	t.Parallel()
	config := Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}
	frame := exampleFrame()
	first := conversationID(config, frame)
	frame.Actor.EmailAddress = ""
	require.Equal(t, first, conversationID(config, frame))
}

func TestSignedWebhookPersistsTranscriptAndEnforcesPolicy(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	result := new(risk.ScanResult)
	result.Action = "block"
	scanner := &recordingScanner{inputs: nil, userIDs: nil, result: result, err: nil}
	service := &Service{store: store, scanner: scanner}
	key := []byte("EXAMPLE-signing-secret")
	config.SigningSecrets = []string{"whsec_" + base64.StdEncoding.EncodeToString(key)}
	mux := goahttp.NewMuxer()
	Attach(mux, testenv.NewLogger(t), service, &testResolver{config: config, err: nil})
	frame := exampleFrame()
	body, err := json.Marshal(frame)
	require.NoError(t, err)
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/hooks/anthropic-inference/example", bytes.NewReader(body))
		request.Header = signedHeaders(body, key, time.Now())
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		require.Contains(t, response.Body.String(), `"action":"deny"`)
	}
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "EXAMPLE prompt", messages[0].Content)
	require.Len(t, scanner.inputs, 2)
}

func TestStoreDisplaysActorEmail(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Actor.EmailAddress = " Person@Example.test "
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	queries := chatrepo.New(db)
	conversation, err := queries.GetChat(t.Context(), chatrepo.GetChatParams{ID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Equal(t, "person@example.test", conversation.ExternalUserID.String)
	messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversation.ID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "person@example.test", messages[0].ExternalUserID.String)
}

func TestStoreRefreshesActorLabelWithoutDuplicatingConversation(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Actor.EmailAddress = ""
	frame.Source.Application = ""
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	queries := chatrepo.New(db)
	id := conversationID(config, frame)
	conversation, err := queries.GetChat(t.Context(), chatrepo.GetChatParams{ID: id, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.False(t, conversation.ExternalUserID.Valid, "no email: conversation label should be null until email arrives")
	frame.Actor.EmailAddress = "person@example.test"
	frame.Source.Application = "claude-code"
	require.Equal(t, id, conversationID(config, frame))
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	conversation, err = queries.GetChat(t.Context(), chatrepo.GetChatParams{ID: id, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Equal(t, frame.Actor.EmailAddress, conversation.ExternalUserID.String)
	messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: id, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, frame.Actor.EmailAddress, messages[0].ExternalUserID.String)
	require.Equal(t, "claude-code-web", messages[0].Source.String)
}

func TestStorePreservesKnownEmailWhenLaterFrameOmitsIt(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Actor.EmailAddress = "person@example.test"
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	// A later frame for the same conversation omits the actor email.
	frame.RequestID = "next-request"
	frame.Actor.EmailAddress = ""
	frame.Messages = append(frame.Messages, Message{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"reply"}]`)})
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	queries := chatrepo.New(db)
	conversation, err := queries.GetChat(t.Context(), chatrepo.GetChatParams{ID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Equal(t, "person@example.test", conversation.ExternalUserID.String, "conversation label must not regress to actor ID")
}

func TestStorePreservesInferenceApplicationSource(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	for _, tc := range []struct{ application, source string }{
		{"claude-ai", "claude-chat-web"},
		{"claude-code", "claude-code-web"},
		{"claude-design", "claude-design"},
		{"future-application", "future-application"},
		{"", "anthropic-inference"},
	} {
		frame := exampleFrame()
		frame.Source.Application = tc.application
		frame.SessionID = "source-test-" + tc.source
		require.NoError(t, store.Save(t.Context(), config, frame, ""))
		messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
		require.NoError(t, err)
		require.Len(t, messages, 1)
		require.Equal(t, tc.source, messages[0].Source.String)
	}
}
