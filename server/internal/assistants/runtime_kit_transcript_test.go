package assistants

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestKitTranscriptStableMessageIDs(t *testing.T) {
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
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_transcript_projection")
	require.NoError(t, err)
	assistant, project := seedAssistant(t, db, "kit-transcript", 1)
	thread := seedThreadWithEvent(t, db, assistant, "kit-transcript", "kit-transcript", eventStatusPending)
	var chatID uuid.UUID
	require.NoError(t, db.QueryRow(t.Context(), `SELECT chat_id FROM assistant_threads WHERE project_id=$1 AND id=$2`, project, thread).Scan(&chatID))
	runtime := uuid.New()
	_, err = db.Exec(t.Context(), `INSERT INTO assistant_runtimes(id,project_id,assistant_id,assistant_thread_id,backend,state,runtime_version) VALUES($1,$2,$3,$4,'local','active',3)`, runtime, project, assistant, uuid.Nil)
	require.NoError(t, err)
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
	var initialReadings, initialEvents int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM publish_outbox`).Scan(&initialReadings))
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM outbox`).Scan(&initialEvents))
	require.Positive(t, initialReadings)
	// Conversation events and storage readings share the publish outbox.
	var topics int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(DISTINCT topic) FROM publish_outbox`).Scan(&topics))
	require.GreaterOrEqual(t, topics, int64(2))
	result, err = projector.Handle(t.Context(), b, first)
	require.NoError(t, err)
	require.False(t, result.Changed)
	result, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"agent_message","messageId":"reply","content":[{"type":"text","text":"two"}]}`))
	require.NoError(t, err)
	require.True(t, result.Changed)
	var rows, readings, events int64
	var content string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*),min(content) FROM chat_messages WHERE project_id=$1 AND chat_id=$2`, project, chatID).Scan(&rows, &content))
	require.EqualValues(t, 1, rows)
	require.Equal(t, "two", content)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM publish_outbox`).Scan(&readings))
	require.Equal(t, initialReadings, readings)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM outbox`).Scan(&events))
	require.Equal(t, initialEvents, events)
	// Dashboard user rows are correlated explicitly, never by matching text.
	echoID := uuid.New()
	_, err = writer.Write(t.Context(), project, []chat.MessageWrite{{Params: chatrepo.CreateChatMessageParams{ID: echoID, ProjectID: project, ChatID: chatID, Role: "user", Content: "dashboard prompt"}}})
	require.NoError(t, err)
	b.UserEchoes = map[string]uuid.UUID{"user-echo": echoID}
	result, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"user_message","messageId":"user-echo","content":[{"type":"text","text":"dashboard prompt"}]}`))
	require.NoError(t, err)
	require.True(t, result.Ignored)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM chat_messages WHERE project_id=$1 AND chat_id=$2`, project, chatID).Scan(&rows))
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
	_, err = db.Exec(t.Context(), `UPDATE assistant_runtimes SET ended_at=clock_timestamp() WHERE project_id=$1 AND id=$2`, project, runtime)
	require.NoError(t, err)
	_, err = projector.Handle(t.Context(), b, first)
	require.Error(t, err, "stale callbacks must not reach persistence")
}
