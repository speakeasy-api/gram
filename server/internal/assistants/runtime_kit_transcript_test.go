package assistants

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
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
	for _, reason := range []string{"", "end_turn"} {
		u.StopReason = reason
		_, apply, _, _, err = translateKitMessage(b, id, u, nil, nil)
		require.NoError(t, err)
		require.False(t, apply, "omitted content cannot create a message")
	}
	u.StopReason = ""

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
	_, err = queries.InsertKitRuntimeSession(t.Context(), assistantrepo.InsertKitRuntimeSessionParams{SessionID: b.SessionID, RuntimeID: runtime, ProjectID: project, AssistantID: assistant, ThreadID: thread})
	require.NoError(t, err)
	core := &ServiceCore{db: db}
	projector := NewKitTranscriptProjector(db, writer, nil, func(ctx context.Context, tx pgx.Tx, got KitTranscriptBinding) error {
		binding, err := core.ValidateKitRuntimeSessionInTx(ctx, tx, kitRuntimeSessionKey{ProjectID: got.ProjectID, AssistantID: got.AssistantID, RuntimeID: got.RuntimeID, ThreadID: got.ThreadID}, got.SessionID)
		if err == nil {
			require.Equal(t, got.ChatID, binding.ChatID)
		}
		return err
	})
	notification := func(update string) kitacp.Notification {
		return kitacp.Notification{SessionID: b.SessionID, Method: "session/update", Params: json.RawMessage(`{"sessionId":"session","update":` + update + `}`)}
	}
	malformedID := uuid.New()
	changed, err := writer.ProjectMessage(t.Context(), project, malformedID, func(ctx context.Context, tx pgx.Tx) error { return projector.guard(ctx, tx, b) }, func(*chatrepo.ChatMessage) (chat.MessageWrite, bool, error) {
		write := chat.MessageWrite{Params: chatrepo.CreateChatMessageParams{ID: malformedID, ProjectID: project, ChatID: chatID, Role: "assistant"}}
		write.Params.Source.String = "kit" // Valid deliberately false.
		return write, true, nil
	})
	require.ErrorContains(t, err, "invalid projection ownership")
	require.False(t, changed)
	beforeOmitted, err := testrepo.New(db).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	omitted, err := projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"agent_message","messageId":"omitted","stopReason":"end_turn"}`))
	require.NoError(t, err)
	require.False(t, omitted.Changed)
	_, err = chatrepo.New(db).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, ID: KitTranscriptMessageID(b, "agent_message", "omitted")})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	afterOmitted, err := testrepo.New(db).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Equal(t, beforeOmitted, afterOmitted, "omitted content must not enqueue storage or metering events")
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
	require.NoError(t, riskrepo.New(db).MarkMessagesRiskAnalyzed(t.Context(), riskrepo.MarkMessagesRiskAnalyzedParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, MessageIds: []uuid.UUID{KitTranscriptMessageID(b, "agent_message", "reply")}}))
	result, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"agent_message","messageId":"reply","content":[{"type":"text","text":"two"}]}`))
	require.NoError(t, err)
	require.True(t, result.Changed)
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ProjectID: project, ChatID: chatID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "two", messages[0].Content)
	require.False(t, messages[0].RiskAnalyzedAt.Valid, "replacement must be scanned again")
	require.NoError(t, riskrepo.New(db).MarkMessagesRiskAnalyzed(t.Context(), riskrepo.MarkMessagesRiskAnalyzedParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, MessageIds: []uuid.UUID{KitTranscriptMessageID(b, "agent_message", "reply")}}))

	beforeTerminal, err := chatrepo.New(db).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, ID: KitTranscriptMessageID(b, "agent_message", "reply")})
	require.NoError(t, err)
	require.True(t, beforeTerminal.RiskAnalyzedAt.Valid, "analysis marker must be set before terminal projection")

	// A reason-only terminal update must update the same canonical row, even
	// though content is unchanged. Replaying it must be idempotent.
	reducer := newKitTranscriptReducer()
	_, err = reducer.Apply(first)
	require.NoError(t, err)
	terminal, err := reducer.Apply(notification(`{"sessionUpdate":"state_update","state":"idle","stopReason":"cancelled"}`))
	require.NoError(t, err)
	result, err = projector.Handle(t.Context(), b, terminal)
	require.NoError(t, err)
	require.True(t, result.Changed)
	result, err = projector.Handle(t.Context(), b, terminal)
	require.NoError(t, err)
	require.False(t, result.Changed)
	row, err := chatrepo.New(db).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, ID: KitTranscriptMessageID(b, "agent_message", "reply")})
	require.NoError(t, err)
	require.Equal(t, "two", row.Content)
	require.Equal(t, "cancelled", row.FinishReason.String)
	require.Equal(t, beforeTerminal.ContentRaw, row.ContentRaw, "reason-only projection must preserve raw content")
	require.Equal(t, beforeTerminal.ToolCalls, row.ToolCalls, "reason-only projection must preserve tool calls")
	require.True(t, row.RiskAnalyzedAt.Valid, "reason-only updates preserve completed risk analysis")
	for _, field := range []string{"raw content", "tool calls"} {
		patch := chatrepo.PatchKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, ChatID: chatID, ID: row.ID, Content: row.Content, ContentRaw: row.ContentRaw, ToolCalls: row.ToolCalls, ContentHash: row.ContentHash, FinishReason: row.FinishReason}
		if field == "raw content" {
			patch.ContentRaw = []byte(`[{"type":"text","text":"updated raw"}]`)
		} else {
			patch.ToolCalls = []byte(`[{"id":"new-call"}]`)
		}
		_, err := chatrepo.New(db).PatchKitProjectionMessage(t.Context(), patch)
		require.NoError(t, err)
		row, err = chatrepo.New(db).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, ID: row.ID})
		require.NoError(t, err)
		require.False(t, row.RiskAnalyzedAt.Valid, field)
		require.NoError(t, riskrepo.New(db).MarkMessagesRiskAnalyzed(t.Context(), riskrepo.MarkMessagesRiskAnalyzedParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, MessageIds: []uuid.UUID{row.ID}}))
	}

	redisClient, err := assistantsInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	stream := chat.NewTurnStream(redisClient)
	projector.stream = stream
	anchor, err := stream.Publish(t.Context(), chatID, chat.TurnFrame{Kind: chat.TurnFrameText, Text: "before"})
	require.NoError(t, err)
	// Idle alone is not an authoritative completion reason, and terminal output
	// without a matching message must not create an empty canonical row.
	result, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"state_update","state":"idle","messageId":"reply"}`))
	require.NoError(t, err)
	require.False(t, result.Changed)
	result, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"state_update","state":"idle","messageId":"no-output","stopReason":"end_turn"}`))
	require.NoError(t, err)
	require.False(t, result.Changed)
	_, err = stream.Publish(t.Context(), chatID, chat.TurnFrame{Kind: chat.TurnFrameText, Text: "still-open"})
	require.NoError(t, err)
	receiveCtx, cancelReceive := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelReceive()
	frames, err := stream.Subscribe(receiveCtx, chatID, anchor)
	require.NoError(t, err)
	var frame chat.TurnFrame
	select {
	case received, ok := <-frames:
		require.True(t, ok, "stream closed before the expected frame")
		frame = received
	case <-receiveCtx.Done():
		t.Fatal("timed out waiting for the expected stream frame")
	}
	require.Equal(t, chat.TurnFrameText, frame.Kind, "ambiguous idle and no-output terminal must not close the stream")
	require.Equal(t, "still-open", frame.Text)
	projector.stream = nil
	// Replaying a content event without terminal metadata cannot erase it.
	_, err = projector.Handle(t.Context(), b, notification(`{"sessionUpdate":"agent_message","messageId":"reply","content":[{"type":"text","text":"two"}]}`))
	require.NoError(t, err)
	row, err = chatrepo.New(db).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, ID: row.ID})
	require.NoError(t, err)
	require.Equal(t, "cancelled", row.FinishReason.String)
	// This replay replaces the raw-content/tool-call mutations above, so it
	// preserves the terminal reason but must invalidate their completed scan.
	require.False(t, row.RiskAnalyzedAt.Valid, "content replay must invalidate risk analysis")

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
	stale := first
	stale.SessionID = wrong.SessionID
	stale.Params = json.RawMessage(`{"sessionId":"stale-session","update":{"sessionUpdate":"agent_message","messageId":"reply","content":[]}}`)
	_, err = projector.Handle(t.Context(), wrong, stale)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
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

func TestKitTranscriptTerminalReasonTranslation(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"end_turn", "cancelled", "max_tokens", "future_reason"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			old := &chatrepo.ChatMessage{Content: "answer", ContentRaw: []byte(`[{"type":"text","text":"answer"}]`)}
			update := kitTranscriptUpdate{SessionUpdate: "agent_message", StopReason: reason}
			write, apply, _, _, err := translateKitMessage(KitTranscriptBinding{}, uuid.New(), update, nil, old)
			require.NoError(t, err)
			require.True(t, apply)
			require.Equal(t, reason, write.Params.FinishReason.String)
			require.Equal(t, old.Content, write.Params.Content)
			require.Equal(t, old.ContentRaw, write.Params.ContentRaw)
			update.Content = json.RawMessage(`[]`)
			write, apply, _, _, err = translateKitMessage(KitTranscriptBinding{}, uuid.New(), update, nil, nil)
			require.NoError(t, err)
			require.True(t, apply)
			require.Equal(t, reason, write.Params.FinishReason.String)
		})
	}
}
