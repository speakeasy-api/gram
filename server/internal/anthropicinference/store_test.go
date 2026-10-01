package anthropicinference

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestStoreDeduplicatesGrowingTranscripts(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	userID, err := store.ResolveActor(t.Context(), config, frame)
	require.NoError(t, err)
	require.Empty(t, userID)
	saveFrame(t, store, config, frame, userID)
	saveFrame(t, store, config, frame, userID)
	frame.RequestID = "next-request"
	frame.Messages = append(frame.Messages,
		Message{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"EXAMPLE reply"}]`)},
		Message{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"EXAMPLE second prompt"}]`)},
	)
	saveFrame(t, store, config, frame, userID)
	saveFrame(t, store, config, frame, userID)
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
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
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
	saveFrame(t, store, config, frame, "")
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
	saveFrame(t, store, config, frame, "")
	queries := chatrepo.New(db)
	id := conversationID(config, frame)
	conversation, err := queries.GetChat(t.Context(), chatrepo.GetChatParams{ID: id, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.False(t, conversation.ExternalUserID.Valid, "no email: conversation label should be null until email arrives")
	frame.Actor.EmailAddress = "person@example.test"
	frame.Source.Application = "claude-code"
	require.Equal(t, id, conversationID(config, frame))
	saveFrame(t, store, config, frame, "")
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
	saveFrame(t, store, config, frame, "")
	// A later frame for the same conversation omits the actor email.
	frame.RequestID = "next-request"
	frame.Actor.EmailAddress = ""
	frame.Messages = append(frame.Messages, Message{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"reply"}]`)})
	saveFrame(t, store, config, frame, "")
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
		saveFrame(t, store, config, frame, "")
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
	saveFrame(t, store, config, frame, "")
	saveFrame(t, store, config, frame, "user-example")
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
	saveFrame(t, store, config, frame, userID)
	saveFrame(t, store, config, frame, "")
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
	saveFrame(t, store, config, frame, "")
	frame.Messages[0].Content = json.RawMessage(`[{"type":"text","text":"edited history"}]`)
	for range 3 {
		frame.Messages = append(frame.Messages, Message{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"new continue"}]`)})
	}
	saveFrame(t, store, config, frame, "")
	saveFrame(t, store, config, frame, "")
	frame.Messages = frame.Messages[5:]
	saveFrame(t, store, config, frame, "")
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
	saveFrame(t, store, config, frame, "")
	frame.Messages[0].Content = json.RawMessage(`[{"type":"tool_use","id":"example-call","tool_name":"read_file","input":{"path":"example.txt"}}]`)
	saveFrame(t, store, config, frame, "")
	frame.Messages[0].Content = json.RawMessage(original)
	saveFrame(t, store, config, frame, "")
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
	saveFrame(t, store, config, frame, "")
	saveFrame(t, store, config, frame, "")
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
	// Every split row keeps the archival message, but publishes only its own
	// content so consumers cannot classify the full message once per sibling.
	publications, err := testrepo.New(db).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	byID := make(map[string]*conversationv1.Message)
	for _, row := range publications {
		if row.Topic != string(proto.MessageName(&conversationv1.Message{})) {
			continue
		}
		event := &conversationv1.Message{}
		require.NoError(t, proto.Unmarshal(row.Message, event))
		require.NotContains(t, byID, event.GetId())
		byID[event.GetId()] = event
		require.Empty(t, event.GetBody().GetSourceContentJson())
		require.Nil(t, event.GetBody().GetSourceContent())
	}
	require.Len(t, byID, 4)
	for i, message := range messages {
		source := frame.Messages[0].Content
		if i >= 2 {
			source = frame.Messages[1].Content
		}
		require.JSONEq(t, string(source), string(message.ContentRaw))
	}
	textParts := byID[messages[0].ID.String()].GetBody().GetParts()
	require.Len(t, textParts, 1)
	require.Equal(t, "Reading the file", textParts[0].GetText())
	toolParts := byID[messages[1].ID.String()].GetBody().GetParts()
	require.Len(t, toolParts, 1)
	require.Equal(t, "read_file", toolParts[0].GetToolCall().GetName())
	resultParts := byID[messages[2].ID.String()].GetBody().GetParts()
	require.Len(t, resultParts, 1)
	require.Equal(t, "file output", resultParts[0].GetText())
	promptParts := byID[messages[3].ID.String()].GetBody().GetParts()
	require.Len(t, promptParts, 2)
	require.Equal(t, "Summarize this", promptParts[0].GetText())
	require.Equal(t, parts[0].ContentAssetUrl, promptParts[1].GetContentReference().GetUri())
	require.Equal(t, "example.txt", promptParts[1].GetContentReference().GetFilename())
	require.Equal(t, parts[0].ExternalID.String, promptParts[1].GetContentReference().GetExternalId())
	frame.Messages = append(frame.Messages, Message{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"summary"}]`)})
	saveFrame(t, store, config, frame, "")
	messages, err = queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 5)
	require.Equal(t, "summary", messages[4].Content)
	publications, err = testrepo.New(db).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	var unsplit *conversationv1.Message
	for _, row := range publications {
		if row.Topic != string(proto.MessageName(&conversationv1.Message{})) {
			continue
		}
		event := &conversationv1.Message{}
		require.NoError(t, proto.Unmarshal(row.Message, event))
		if event.GetId() == messages[4].ID.String() {
			unsplit = event
		}
	}
	require.NotNil(t, unsplit)
	require.JSONEq(t, string(frame.Messages[2].Content), string(unsplit.GetBody().GetSourceContentJson()))
}

func TestExternalContentPartsCommitWithParent(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	saveFrame(t, store, config, frame, "")
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

// saveFrame stores a frame and returns the index of its first new message.
func saveFrame(t *testing.T, store *postgresStore, config Config, frame Frame, userID string) int {
	t.Helper()
	start, err := store.Save(t.Context(), config, frame, userID)
	require.NoError(t, err)
	return start
}

func TestStoreAppendsAfterRollingCompaction(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = nil
	for index := range 12 {
		frame.Messages = append(frame.Messages, textMessage("user", fmt.Sprintf("turn %d", index)))
	}
	require.Equal(t, 0, saveFrame(t, store, config, frame, ""))
	// The client replaced the first eight turns with a summary, kept the last
	// four verbatim, and appended the new turn.
	compacted := append([]Message{textMessage("user", "EXAMPLE summary of the first eight turns")}, frame.Messages[8:]...)
	compacted = append(compacted, textMessage("user", "turn 12"))
	frame.Messages = compacted
	require.Equal(t, len(compacted)-1, saveFrame(t, store, config, frame, ""))
	require.Equal(t, len(compacted), saveFrame(t, store, config, frame, ""))
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 13)
	for index, msg := range messages {
		require.Equal(t, fmt.Sprintf("turn %d", index), msg.Content)
	}
}

func TestStoreSkipsRepeatedIdenticalAnchor(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "start"), textMessage("user", "continue"), textMessage("user", "continue")}
	require.Equal(t, 0, saveFrame(t, store, config, frame, ""))
	frame.Messages = append(frame.Messages, textMessage("user", "continue"))
	require.Equal(t, 4, saveFrame(t, store, config, frame, ""))
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 3)
}

func TestStoreContinuesHistoryStoredByCount(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "first"), textMessage("assistant", "second")}
	chatID := conversationID(config, frame)
	// Rows written before content hashing carry an ordinal and no hash. A frame
	// without conversation messages creates the conversation they belong to.
	require.Equal(t, 0, saveFrame(t, store, config, Frame{
		Type: frame.Type, RequestID: frame.RequestID, TenantID: frame.TenantID, Actor: frame.Actor,
		Source: frame.Source, SessionID: frame.SessionID, Model: frame.Model,
		Messages: []Message{{Role: "system", Content: json.RawMessage(`[]`)}},
	}, ""))
	writes := make([]chat.ExternalMessageWrite, 0, len(frame.Messages))
	for index, msg := range frame.Messages {
		var write chat.ExternalMessageWrite
		write.Params.ID = uuid.New()
		write.Params.ChatID = chatID
		write.Params.ProjectID = config.ProjectID
		write.Params.Role = msg.Role
		write.Params.Content = fmt.Sprintf("legacy %d", index)
		write.Params.ExternalMessageID = conv.ToPGText(fmt.Sprintf("anthropic-inference:%d", index))
		write.Params.Origin = conv.ToPGText("anthropic-inference")
		write.Params.CreatedAt = conv.ToPGTimestamptz(time.Now().Add(time.Duration(index) * time.Microsecond))
		write.WorkloadSource = metering.WorkloadSourceHook
		writes = append(writes, write)
	}
	_, err := store.writer.WriteExternalWithContentParts(t.Context(), config.ProjectID, writes, nil)
	require.NoError(t, err)
	frame.Messages = append(frame.Messages, textMessage("user", "third"))
	require.Equal(t, 2, saveFrame(t, store, config, frame, ""))
	require.Equal(t, 3, saveFrame(t, store, config, frame, ""))
	frame.Messages = append(frame.Messages, textMessage("assistant", "fourth"))
	require.Equal(t, 3, saveFrame(t, store, config, frame, ""))
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 4)
	require.Equal(t, "third", messages[2].Content)
	require.Equal(t, "fourth", messages[3].Content)
}

// archiveFrame resolves the conversation the way Service.Process does, then
// archives the frame. It returns the resolved chat, how it was resolved and
// the index of the first message Save had not seen before.
func archiveFrame(t *testing.T, store *postgresStore, config Config, frame Frame) (uuid.UUID, string, int) {
	t.Helper()
	chatID, outcome, err := store.ResolveConversation(t.Context(), config, frame)
	require.NoError(t, err)
	frame.conversation, frame.conversationOutcome = chatID, outcome
	userID, err := store.ResolveActor(t.Context(), config, frame)
	require.NoError(t, err)
	start, err := store.Save(t.Context(), config, frame, userID)
	require.NoError(t, err)
	return chatID, outcome, start
}

func sessionlessFrame(requestID string, messages ...Message) Frame {
	frame := exampleFrame()
	frame.SessionID = ""
	frame.RequestID = requestID
	frame.Source.Application = "claude-design"
	frame.Messages = messages
	return frame
}

func TestStoreAdoptsTranscriptDeliveredWithoutSessionID(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	u1, a1, u2, a2, u3 := textMessage("user", "turn 1"), textMessage("assistant", "reply 1"), textMessage("user", "turn 2"), textMessage("assistant", "reply 2"), textMessage("user", "turn 3")

	first := sessionlessFrame("request-1", u1)
	chatID, outcome, start := archiveFrame(t, store, config, first)
	require.Equal(t, conversationOutcomeNew, outcome)
	require.Equal(t, conversationID(config, first), chatID)
	require.Equal(t, 0, start)

	second := sessionlessFrame("request-2", u1, a1, u2)
	adopted, outcome, start := archiveFrame(t, store, config, second)
	require.Equal(t, conversationOutcomeAdoptedPrefix, outcome)
	require.Equal(t, chatID, adopted)
	require.Equal(t, 1, start)

	third := sessionlessFrame("request-3", u1, a1, u2, a2, u3)
	adopted, outcome, start = archiveFrame(t, store, config, third)
	require.Equal(t, conversationOutcomeAdoptedPrefix, outcome)
	require.Equal(t, chatID, adopted)
	require.Equal(t, 3, start)

	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 5)
	require.Equal(t, "turn 3", messages[4].Content)
	// The request-scoped identities of the later frames never became chats.
	for _, frame := range []Frame{second, third} {
		_, err := chatrepo.New(db).GetChat(t.Context(), chatrepo.GetChatParams{ID: conversationID(config, frame), ProjectID: config.ProjectID})
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
}

func TestStoreAdoptsRedeliveredTranscriptWithoutSessionID(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	u1, a1, u2 := textMessage("user", "turn 1"), textMessage("assistant", "reply 1"), textMessage("user", "turn 2")
	chatID, outcome, _ := archiveFrame(t, store, config, sessionlessFrame("request-1", u1, a1, u2))
	require.Equal(t, conversationOutcomeNew, outcome)
	again, outcome, start := archiveFrame(t, store, config, sessionlessFrame("request-2", u1, a1, u2))
	require.Equal(t, conversationOutcomeAdoptedPrefix, outcome)
	require.Equal(t, chatID, again)
	require.Equal(t, 3, start)
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 3)
}

func TestStoreNeverAdoptsAnotherActorsTranscript(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	u1, a1, u2, a2, u3 := textMessage("user", "turn 1"), textMessage("assistant", "reply 1"), textMessage("user", "turn 2"), textMessage("assistant", "reply 2"), textMessage("user", "turn 3")
	first, _, _ := archiveFrame(t, store, config, sessionlessFrame("request-1", u1, a1, u2))
	other := sessionlessFrame("request-2", u1, a1, u2)
	other.Actor.ID = "other-actor"
	other.Actor.EmailAddress = "other@example.test"
	second, outcome, start := archiveFrame(t, store, config, other)
	require.Equal(t, conversationOutcomeNew, outcome)
	require.NotEqual(t, first, second)
	require.Equal(t, 0, start)

	// The other actor's chat is the newer holder of the same prefix identity;
	// the first actor's continuation still lands in the first actor's chat.
	adopted, outcome, start := archiveFrame(t, store, config, sessionlessFrame("request-3", u1, a1, u2, a2, u3))
	require.Equal(t, conversationOutcomeAdoptedPrefix, outcome)
	require.Equal(t, first, adopted)
	require.Equal(t, 3, start)
	for chatID, want := range map[uuid.UUID]int{first: 5, second: 3} {
		messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: config.ProjectID})
		require.NoError(t, err)
		require.Len(t, messages, want)
	}
}

func TestStoreNeverAdoptsForAnonymousActor(t *testing.T) {
	t.Parallel()
	store, _, config := newTestStore(t)
	u1, a1, u2 := textMessage("user", "turn 1"), textMessage("assistant", "reply 1"), textMessage("user", "turn 2")
	anonymous := func(requestID string, messages ...Message) Frame {
		frame := sessionlessFrame(requestID, messages...)
		frame.Actor.ID = ""
		frame.Actor.EmailAddress = ""
		return frame
	}
	first, outcome, _ := archiveFrame(t, store, config, anonymous("request-1", u1, a1, u2))
	require.Equal(t, conversationOutcomeNew, outcome)
	// With no actor to scope the match to, an identical transcript from an
	// anonymous frame is a new, request-scoped conversation.
	second, outcome, _ := archiveFrame(t, store, config, anonymous("request-2", u1, a1, u2))
	require.Equal(t, conversationOutcomeNew, outcome)
	require.NotEqual(t, first, second)
}
func TestStoreKeepsConversationsWithSharedOpeningApart(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	u1 := textMessage("user", "turn 1")
	first, _, _ := archiveFrame(t, store, config, sessionlessFrame("request-1", u1, textMessage("assistant", "reply A"), textMessage("user", "turn 2A")))
	// Same actor, same opening, different continuation: the opening matches a
	// stored message, but not the first chat's newest, so it is a new chat.
	second, outcome, start := archiveFrame(t, store, config, sessionlessFrame("request-2", u1, textMessage("assistant", "reply B"), textMessage("user", "turn 2B")))
	require.Equal(t, conversationOutcomeNew, outcome)
	require.NotEqual(t, first, second)
	require.Equal(t, 0, start)
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: first, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 3)
	require.Equal(t, "turn 2A", messages[2].Content)
}

func TestStoreContinuesBySessionWhenPresent(t *testing.T) {
	t.Parallel()
	store, _, config := newTestStore(t)
	frame := exampleFrame()
	chatID, outcome, _ := archiveFrame(t, store, config, frame)
	require.Equal(t, conversationOutcomeNew, outcome)
	require.Equal(t, conversationID(config, frame), chatID)
	frame.RequestID = "next-request"
	frame.Messages = append(frame.Messages, textMessage("assistant", "EXAMPLE reply"), textMessage("user", "EXAMPLE second prompt"))
	again, outcome, start := archiveFrame(t, store, config, frame)
	require.Equal(t, conversationOutcomeSession, outcome)
	require.Equal(t, chatID, again)
	require.Equal(t, 1, start)
}

func TestStoreCheckpointFollowsAdoptedConversation(t *testing.T) {
	t.Parallel()
	store, _, config := newTestStore(t)
	u1, a1, u2, a2, u3 := textMessage("user", "turn 1"), textMessage("assistant", "reply 1"), textMessage("user", "turn 2"), textMessage("assistant", "reply 2"), textMessage("user", "turn 3")
	second := sessionlessFrame("request-2", u1, a1, u2)
	archiveFrame(t, store, config, sessionlessFrame("request-1", u1))
	chatID, outcome, _ := archiveFrame(t, store, config, second)
	require.Equal(t, conversationOutcomeAdoptedPrefix, outcome)
	second.conversation, second.conversationOutcome = chatID, outcome
	session, err := store.Begin(t.Context(), config, second, "")
	require.NoError(t, err)
	_, err = session.Load(t.Context())
	require.NoError(t, err)
	require.NoError(t, session.Accept(t.Context(), transcriptHashes(conversationMessages(second.Messages))))

	third := sessionlessFrame("request-3", u1, a1, u2, a2, u3)
	adopted, outcome, _ := archiveFrame(t, store, config, third)
	require.Equal(t, conversationOutcomeAdoptedPrefix, outcome)
	require.Equal(t, chatID, adopted)
	third.conversation, third.conversationOutcome = adopted, outcome
	session, err = store.Begin(t.Context(), config, third, "")
	require.NoError(t, err)
	accepted, err := session.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, accepted, 3)
	require.Equal(t, 3, acceptedPrefix(accepted, transcriptHashes(conversationMessages(third.Messages))))
}

func TestStoreStartsNewChatForUnknownSessionID(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	u1, a1, u2, a2, u3 := textMessage("user", "turn 1"), textMessage("assistant", "reply 1"), textMessage("user", "turn 2"), textMessage("assistant", "reply 2"), textMessage("user", "turn 3")
	original := exampleFrame()
	original.Messages = []Message{u1, a1, u2}
	first, _, _ := archiveFrame(t, store, config, original)

	// The same actor forks the conversation under a new session id. The
	// history is identical, but a session id that names no chat starts one
	// rather than folding the fork into the original.
	fork := exampleFrame()
	fork.SessionID = "session-fork"
	fork.RequestID = "request-fork"
	fork.Messages = []Message{u1, a1, u2, a2, u3}
	second, outcome, start := archiveFrame(t, store, config, fork)
	require.Equal(t, conversationOutcomeNew, outcome)
	require.NotEqual(t, first, second)
	require.Equal(t, 0, start)
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: first, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 3)
}

func TestStoreStartsNewChatWhenSharedPrefixIsAmbiguous(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	u1 := textMessage("user", "build a landing page")
	// The same actor opens two conversations with the identical prompt. Each
	// first frame is a single message, so each starts its own chat, and both
	// chats end in the same identity.
	first, _, _ := archiveFrame(t, store, config, sessionlessFrame("request-a1", u1))
	second, _, _ := archiveFrame(t, store, config, sessionlessFrame("request-b1", u1))
	require.NotEqual(t, first, second)

	// Neither chat can be told apart as the one this continuation belongs to,
	// so it starts a third rather than landing in the wrong one.
	third, outcome, start := archiveFrame(t, store, config, sessionlessFrame("request-a2", u1, textMessage("assistant", "reply A"), textMessage("user", "turn 2A")))
	require.Equal(t, conversationOutcomeAmbiguousPrefix, outcome)
	require.NotEqual(t, first, third)
	require.NotEqual(t, second, third)
	require.Equal(t, 0, start)
	for _, chatID := range []uuid.UUID{first, second} {
		messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: config.ProjectID})
		require.NoError(t, err)
		require.Len(t, messages, 1)
	}

	// From here the continuation is unambiguous and follows the third chat.
	fourth, outcome, start := archiveFrame(t, store, config, sessionlessFrame("request-a3", u1, textMessage("assistant", "reply A"), textMessage("user", "turn 2A"), textMessage("assistant", "reply 2A"), textMessage("user", "turn 3A")))
	require.Equal(t, conversationOutcomeAdoptedPrefix, outcome)
	require.Equal(t, third, fourth)
	require.Equal(t, 3, start)
}
