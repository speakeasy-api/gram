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

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestStoreDeduplicatesGrowingTranscripts(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	userID, err := store.ResolveActor(t.Context(), config, frame)
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
	_, err := store.ResolveActor(t.Context(), config, exampleFrame())
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
	messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversation.ID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for _, msg := range messages {
		require.Equal(t, "person@example.test", msg.ExternalUserID.String, "new messages must inherit preserved conversation email, not actor ID")
	}
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

func TestStorePreservesLastKnownUser(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	require.NoError(t, store.Save(t.Context(), config, frame, "user-example"))
	queries := chatrepo.New(db)
	messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "user-example", messages[0].UserID.String)
	frame.Actor.EmailAddress = ""
	userID, err := store.ResolveActor(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "user-example", userID)
	frame.Messages = append(frame.Messages, Message{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"continue"}]`)})
	require.NoError(t, store.Save(t.Context(), config, frame, userID))
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	messages, err = queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for _, msg := range messages {
		require.Equal(t, "user-example", msg.UserID.String)
	}
	frame.Actor.EmailAddress = "unknown@example.test"
	userID, err = store.ResolveActor(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "user-example", userID)
	frame.Actor.ID = "different-actor"
	userID, err = store.ResolveActor(t.Context(), config, frame)
	require.NoError(t, err)
	require.Empty(t, userID)
}

func TestStoreAppendsOnlyMessagesBeyondStoredCount(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = nil
	for range 7 {
		frame.Messages = append(frame.Messages, Message{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"continue"}]`)})
	}
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	frame.Messages[0].Content = json.RawMessage(`[{"type":"text","text":"edited history"}]`)
	for range 3 {
		frame.Messages = append(frame.Messages, Message{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"new continue"}]`)})
	}
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	frame.Messages = frame.Messages[5:]
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 10)
	for _, msg := range messages[:7] {
		require.Equal(t, "continue", msg.Content)
	}
	for _, msg := range messages[7:] {
		require.Equal(t, "new continue", msg.Content)
	}
}

func TestStoreKeepsOriginalToolDetails(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = []Message{{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"example-call","tool_name":"read_file"}]`)}}
	original := string(frame.Messages[0].Content)
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	frame.Messages[0].Content = json.RawMessage(`[{"type":"tool_use","id":"example-call","tool_name":"read_file","input":{"path":"example.txt"}}]`)
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	frame.Messages[0].Content = json.RawMessage(original)
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.JSONEq(t, original, string(messages[0].ContentRaw))
}

func TestStorePreservesNativePolicyScopesAndIncomingMessageCount(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = []Message{
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"Reading the file"},{"type":"tool_use","id":"example-call","tool_name":"read_file","input":{"path":"example.txt"}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"example-call","tool_name":"read_file","content":"file output"},{"type":"text","text":"Summarize this"},{"type":"attachment","file_name":"example.txt","text":"attachment contents"}]`)},
	}
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	queries := chatrepo.New(db)
	chatID := conversationID(config, frame)
	messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 4)
	require.Equal(t, "assistant", messages[0].Role)
	require.Equal(t, "Reading the file", messages[0].Content)
	require.Empty(t, messages[0].ToolCalls)
	require.Equal(t, "assistant", messages[1].Role)
	require.Empty(t, messages[1].Content)
	require.JSONEq(t, `[{"id":"example-call","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"example.txt\"}"}}]`, string(messages[1].ToolCalls))
	require.Equal(t, "tool", messages[2].Role)
	require.Equal(t, "example-call", messages[2].ToolCallID.String)
	require.Equal(t, "file output", messages[2].Content)
	require.Equal(t, "user", messages[3].Role)
	require.Equal(t, "Summarize this", messages[3].Content)
	parts, err := queries.ListChatContentPartsByChatID(t.Context(), chatrepo.ListChatContentPartsByChatIDParams{ChatID: chatID, ProjectID: config.ProjectID, ParentChatMessageIds: []uuid.UUID{messages[3].ID}})
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Equal(t, "prompt_attachment", parts[0].Kind)
	require.Equal(t, messages[3].ID, parts[0].ParentChatMessageID.UUID)
	require.NotEmpty(t, parts[0].ContentAssetUrl)
	require.False(t, parts[0].RiskAnalyzedAt.Valid)
	count, err := queries.CountInferenceMessages(t.Context(), chatrepo.CountInferenceMessagesParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: config.ProjectID, Valid: true}})
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	frame.Messages = append(frame.Messages, Message{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"summary"}]`)})
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	messages, err = queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 5)
	require.Equal(t, "summary", messages[4].Content)
}

func TestExternalContentPartsCommitWithParent(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	require.NoError(t, store.Save(t.Context(), config, frame, ""))
	chatID := conversationID(config, frame)
	var write chat.ExternalMessageWrite
	write.Params.ID = uuid.New()
	write.Params.ChatID = chatID
	write.Params.ProjectID = config.ProjectID
	write.Params.Role = "user"
	write.Params.Content = "parent"
	write.Params.ExternalMessageID = conv.ToPGText("anthropic-inference:1")
	write.Params.Origin = conv.ToPGText("anthropic-inference")
	write.WorkloadSource = metering.WorkloadSourceHook
	url, err := store.writer.WriteContentPartAsset(t.Context(), config.ProjectID, chatID, []byte("attachment"))
	require.NoError(t, err)
	var part chatrepo.CreateChatContentPartParams
	part.ChatID = chatID
	part.ProjectID = config.ProjectID
	part.ParentChatMessageID = uuid.NullUUID{UUID: write.Params.ID, Valid: true}
	part.Kind = "prompt_attachment"
	part.ContentAssetUrl = url
	part.CreatedAt = conv.ToPGTimestamptz(time.Now())
	part.Metadata = []byte("invalid JSON")
	parts := map[uuid.UUID][]chatrepo.CreateChatContentPartParams{write.Params.ID: {part}}
	_, err = store.writer.WriteExternalWithContentParts(t.Context(), config.ProjectID, []chat.ExternalMessageWrite{write}, parts)
	require.Error(t, err)
	queries := chatrepo.New(db)
	count, err := queries.CountInferenceMessages(t.Context(), chatrepo.CountInferenceMessagesParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: config.ProjectID, Valid: true}})
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "failed attachment must roll back its parent's counted ordinal")
	parts[write.Params.ID][0].Metadata = []byte(`{}`)
	inserted, err := store.writer.WriteExternalWithContentParts(t.Context(), config.ProjectID, []chat.ExternalMessageWrite{write}, parts)
	require.NoError(t, err)
	require.EqualValues(t, 1, inserted)
	inserted, err = store.writer.WriteExternalWithContentParts(t.Context(), config.ProjectID, []chat.ExternalMessageWrite{write}, parts)
	require.NoError(t, err)
	require.Zero(t, inserted)
	saved, err := queries.ListChatContentPartsByChatID(t.Context(), chatrepo.ListChatContentPartsByChatIDParams{ChatID: chatID, ProjectID: config.ProjectID, ParentChatMessageIds: []uuid.UUID{write.Params.ID}})
	require.NoError(t, err)
	require.Len(t, saved, 1)
}
