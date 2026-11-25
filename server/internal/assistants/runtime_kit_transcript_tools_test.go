package assistants

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestKitTranscriptToolCallTranslation(t *testing.T) {
	t.Parallel()
	var b KitTranscriptBinding
	b.RuntimeID, b.ProjectID, b.ChatID, b.AssistantID = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	b.SessionID = "session"
	id := KitTranscriptMessageID(b, "tool_call", "call")
	pending := json.RawMessage(`{"status":"pending","title":"search_records","rawInput":{"query":"hello"}}`)
	write, apply, err := translateKitToolCall(b, id, "call", pending, nil)
	require.NoError(t, err)
	require.True(t, apply)
	require.Equal(t, "assistant", write.Params.Role)
	require.Equal(t, "kit", write.Params.Source.String)
	require.JSONEq(t, `[{"id":"call","type":"function","function":{"name":"search_records","arguments":"{\"query\":\"hello\"}"}}]`, string(write.Params.ToolCalls))
	var old chatrepo.ChatMessage
	old.ToolCalls = write.Params.ToolCalls
	for _, patch := range []string{
		`{"title":"Finding relevant records"}`,
		`{"status":"in_progress"}`,
		`{"status":"completed","rawOutput":{"result":1}}`,
		`{"status":"failed","rawOutput":null}`,
		string(pending),
	} {
		updated, apply, err := translateKitToolCall(b, id, "call", json.RawMessage(patch), &old)
		require.NoError(t, err)
		require.True(t, apply)
		require.JSONEq(t, string(write.Params.ToolCalls), string(updated.Params.ToolCalls))
	}
	_, apply, err = translateKitToolCall(b, id, "call", json.RawMessage(`{"title":"Finding relevant records","status":"in_progress"}`), nil)
	require.NoError(t, err)
	require.False(t, apply, "a sparse late update must not invent a function name")
	_, _, err = translateKitToolCall(b, id, "other-call", pending, &old)
	require.ErrorContains(t, err, "identity")
}

func TestKitTranscriptToolProjectionReplay(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_tool_projection")
	require.NoError(t, err)
	assistant, project := seedAssistant(t, db, "kit-tool-projection", 1)
	thread := seedThreadWithEvent(t, db, assistant, "kit-tool-projection", "kit-tool-projection", eventStatusPending)
	queries := assistantrepo.New(db)
	threadRow, err := queries.LoadAssistantThreadForBootstrap(t.Context(), assistantrepo.LoadAssistantThreadForBootstrapParams{ProjectID: project, ThreadID: thread})
	require.NoError(t, err)
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: assistant, RuntimeVersion: 3}))
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant, AssistantThreadID: thread, Backend: "local", State: "active"}))
	runtimeRow, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant})
	require.NoError(t, err)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), db, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	var b KitTranscriptBinding
	b.RuntimeID, b.ProjectID, b.AssistantID = runtimeRow.ID, project, assistant
	b.ThreadID, b.ChatID, b.SessionID = thread, threadRow.ChatID, "session"
	projector := NewKitTranscriptProjector(db, writer, nil, func(_ context.Context, _ pgx.Tx, got KitTranscriptBinding) error {
		require.Equal(t, b.SessionID, got.SessionID)
		return nil
	})
	apply := func(raw string) KitTranscriptResult {
		t.Helper()
		var update kitTranscriptUpdate
		require.NoError(t, json.Unmarshal([]byte(raw), &update))
		result, err := projector.handleKitToolUpdate(t.Context(), b, update, json.RawMessage(raw))
		require.NoError(t, err)
		return result
	}
	updates := []string{
		`{"sessionUpdate":"tool_call_update","toolCallId":"call","status":"pending","title":"search_records","rawInput":{"query":"hello"},"_future":{"keep":true}}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"call","title":"Finding relevant records"}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"call","status":"in_progress"}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"call","status":"completed","rawOutput":{"records":[1]}}`,
	}
	for _, update := range updates {
		apply(update)
	}
	readings, err := testrepo.New(db).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	events, err := testrepo.New(db).CountOutboxRows(t.Context())
	require.NoError(t, err)
	require.False(t, apply(updates[len(updates)-1]).Changed)
	for _, update := range updates {
		apply(update)
	}
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ProjectID: project, ChatID: b.ChatID})
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for _, message := range messages {
		require.Equal(t, "kit", message.Source.String)
		switch message.Role {
		case "assistant":
			require.Equal(t, KitTranscriptMessageID(b, "tool_call", "call"), message.ID)
			require.JSONEq(t, `[{"id":"call","type":"function","function":{"name":"search_records","arguments":"{\"query\":\"hello\"}"}}]`, string(message.ToolCalls))
		case "tool":
			require.Equal(t, KitTranscriptMessageID(b, "tool_call_update", "call"), message.ID)
			require.Equal(t, "call", message.ToolCallID.String)
			require.JSONEq(t, `{"sessionUpdate":"tool_call_update","toolCallId":"call","status":"completed","title":"Finding relevant records","rawInput":{"query":"hello"},"rawOutput":{"records":[1]},"_future":{"keep":true}}`, message.Content)
		default:
			t.Fatalf("unexpected role: %s", message.Role)
		}
	}
	afterReadings, err := testrepo.New(db).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Equal(t, readings, afterReadings)
	afterEvents, err := testrepo.New(db).CountOutboxRows(t.Context())
	require.NoError(t, err)
	require.Equal(t, events, afterEvents)
	failed := `{"sessionUpdate":"tool_call_update","toolCallId":"call","status":"failed","rawOutput":null}`
	require.True(t, apply(failed).Changed)
	require.False(t, apply(failed).Changed)
	require.NoError(t, queries.RetireAssistantRuntimesForSwitch(t.Context(), assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: project, AssistantID: assistant}))
	var update kitTranscriptUpdate
	require.NoError(t, json.Unmarshal([]byte(updates[0]), &update))
	_, err = projector.handleKitToolUpdate(t.Context(), b, update, json.RawMessage(updates[0]))
	require.Error(t, err, "retired runtimes must not project either row")
}
