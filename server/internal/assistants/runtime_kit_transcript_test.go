package assistants

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestKitTranscriptStableMessageIDs(t *testing.T) {
	t.Parallel()
	b := KitTranscriptBinding{RuntimeID: uuid.New(), SessionID: "session"}
	first := KitTranscriptMessageID(b, "agent_message", "message")
	require.Equal(t, first, KitTranscriptMessageID(b, "agent_message", "message"))
	require.NotEqual(t, first, KitTranscriptMessageID(b, "user_message", "message"))
	b.SessionID = "other"
	require.NotEqual(t, first, KitTranscriptMessageID(b, "agent_message", "message"))
	b.SessionID = "session"
	b.RuntimeID = uuid.New()
	require.NotEqual(t, first, KitTranscriptMessageID(b, "agent_message", "message"))
}

func TestKitTranscriptContentPatchSemantics(t *testing.T) {
	t.Parallel()
	b := KitTranscriptBinding{ProjectID: uuid.New(), ChatID: uuid.New(), AssistantID: uuid.New()}
	id := uuid.New()
	old := &chatrepo.ChatMessage{Content: "previous"}
	u := kitTranscriptUpdate{SessionUpdate: "agent_message", MessageID: "m"}
	_, apply, _, _, err := translateKitMessage(b, id, u, nil, old)
	require.NoError(t, err)
	require.False(t, apply, "omitted leaves content untouched")
	for _, raw := range []string{"null", "[]"} {
		u.Content = json.RawMessage(raw)
		w, apply, _, _, err := translateKitMessage(b, id, u, nil, old)
		require.NoError(t, err)
		require.True(t, apply)
		require.Empty(t, w.Params.Content)
		require.JSONEq(t, "[]", string(w.Params.ContentRaw))
	}
	u.Content = json.RawMessage(`[{"type":"text","text":"replacement"}]`)
	w, apply, text, _, err := translateKitMessage(b, id, u, nil, old)
	require.NoError(t, err)
	require.True(t, apply)
	require.Equal(t, "replacement", text)
	require.Equal(t, id, w.Params.ID)
}

func TestKitTranscriptPreservesUnknownContentAndToolPatches(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"type":"_future","payload":{"data":[1,2]}}`)
	text, content, generic, err := kitContent([]json.RawMessage{raw})
	require.NoError(t, err)
	require.JSONEq(t, string(raw), text)
	require.Equal(t, []string{"_future"}, generic)
	require.Contains(t, string(content), "_future")
	b := KitTranscriptBinding{}
	id := uuid.New()
	u := kitTranscriptUpdate{SessionUpdate: "tool_call_update", ToolCallID: "call"}
	old := &chatrepo.ChatMessage{Content: `{"toolCallId":"call","rawInput":{"arg":1},"rawOutput":"old"}`}
	w, apply, _, _, err := translateKitMessage(b, id, u, json.RawMessage(`{"sessionUpdate":"tool_call_update","toolCallId":"call","rawOutput":null,"status":"completed"}`), old)
	require.NoError(t, err)
	require.True(t, apply)
	require.Equal(t, "tool", w.Params.Role)
	require.Equal(t, "call", w.Params.ToolCallID.String)
	require.JSONEq(t, `{"sessionUpdate":"tool_call_update","toolCallId":"call","rawInput":{"arg":1},"rawOutput":null,"status":"completed"}`, w.Params.Content)
}

func TestKitTranscriptReplayPatchAndRuntimeFence(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_transcript_projection")
	require.NoError(t, err)
	assistant, project := seedAssistant(t, db, "kit-transcript", 1)
	thread := seedThreadWithEvent(t, db, assistant, "kit-transcript", "kit-transcript", eventStatusPending)
	threadRow, err := assistantrepo.New(db).LoadAssistantThreadForBootstrap(t.Context(), assistantrepo.LoadAssistantThreadForBootstrapParams{ProjectID: project, ThreadID: thread})
	require.NoError(t, err)
	chatID := threadRow.ChatID
	queries := assistantrepo.New(db)
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: assistant, RuntimeVersion: 3}))
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant, AssistantThreadID: thread, Backend: "local", State: "active"}))
	runtimeRow, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant})
	require.NoError(t, err)
	runtime := runtimeRow.ID
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), db, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	b := KitTranscriptBinding{RuntimeID: runtime, ProjectID: project, AssistantID: assistant, ThreadID: thread, ChatID: chatID, SessionID: "session"}
	projector := NewKitTranscriptProjector(db, writer, nil, func(_ context.Context, _ pgx.Tx, got KitTranscriptBinding) error {
		require.Equal(t, b.SessionID, got.SessionID)
		return nil
	})
	notification := func(update string) kitacp.Notification {
		return kitacp.Notification{SessionID: b.SessionID, Method: "session/update", Params: json.RawMessage(`{"sessionId":"session","update":` + update + `}`)}
	}
	first := notification(`{"sessionUpdate":"agent_message","messageId":"reply","content":[{"type":"text","text":"one"}]}`)
	result, err := projector.Handle(t.Context(), b, first)
	require.NoError(t, err)
	require.True(t, result.Changed)
	initialReadings, err := testrepo.New(db).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	initialEvents, err := testrepo.New(db).CountOutboxRows(t.Context())
	require.NoError(t, err)
	require.Positive(t, initialReadings)
	// Conversation events and storage readings share the publish outbox.
	outboxRows, err := testrepo.New(db).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	topics := make(map[string]struct{})
	for _, row := range outboxRows {
		topics[row.Topic] = struct{}{}
	}
	require.GreaterOrEqual(t, len(topics), 2)
	result, err = projector.Handle(t.Context(), b, first)
	require.NoError(t, err)
	require.False(t, result.Changed)
	result, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"agent_message","messageId":"reply","content":[{"type":"text","text":"two"}]}`))
	require.NoError(t, err)
	require.True(t, result.Changed)
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ProjectID: project, ChatID: chatID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "two", messages[0].Content)
	readings, err := testrepo.New(db).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Equal(t, initialReadings, readings)
	events, err := testrepo.New(db).CountOutboxRows(t.Context())
	require.NoError(t, err)
	require.Equal(t, initialEvents, events)
	// Dashboard user rows are correlated explicitly, never by matching text.
	echoID := uuid.New()
	_, err = writer.Write(t.Context(), project, []chat.MessageWrite{{Params: chatrepo.CreateChatMessageParams{ID: echoID, ProjectID: project, ChatID: chatID, Role: "user", Content: "dashboard prompt"}}})
	require.NoError(t, err)
	b.UserEchoes = map[string]uuid.UUID{"user-echo": echoID}
	result, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"user_message","messageId":"user-echo","content":[{"type":"text","text":"dashboard prompt"}]}`))
	require.NoError(t, err)
	require.True(t, result.Ignored)
	rows, err := chatrepo.New(db).CountChatMessages(t.Context(), chatrepo.CountChatMessagesParams{ProjectID: project, ChatID: chatID})
	require.NoError(t, err)
	require.EqualValues(t, 2, rows)
	wrong := b
	wrong.ChatID = uuid.New()
	_, err = projector.Handle(t.Context(), wrong, first)
	require.Error(t, err)
	wrong = b
	wrong.SessionID = "stale-session"
	_, err = projector.Handle(t.Context(), wrong, first)
	require.Error(t, err)
	// Chunks are transient only and foreground idle is not runtime settlement.
	result, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"agent_message_chunk","messageId":"reply","content":{"type":"text","text":"delta"}}`))
	require.NoError(t, err)
	require.True(t, result.Transient)
	result, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}`))
	require.NoError(t, err)
	require.Equal(t, "idle", result.ForegroundState)
	err = queries.RetireAssistantRuntimesForSwitch(t.Context(), assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: project, AssistantID: assistant})
	require.NoError(t, err)
	_, err = projector.Handle(t.Context(), b, first)
	require.Error(t, err, "stale callbacks must not reach persistence")
}
