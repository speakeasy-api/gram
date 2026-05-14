package assistants

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestServiceCoreSelfHealsHistoryCorruptionOnFirstAttempt(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_self_heal_first")
	require.NoError(t, err)

	projectID, assistantID, chatID, threadID := insertAssistantFixture(t, conn)

	ctx := t.Context()

	// Seed eight chronological user messages plus some assistant/tool noise
	// that should NOT carry over after self-heal trims to user-only.
	seedRows := []struct {
		role       string
		content    string
		toolCallID pgtype.Text
		toolCalls  []byte
	}{
		{role: "user", content: "first"},
		{role: "assistant", content: "ack first"},
		{role: "user", content: "second"},
		{role: "assistant", content: "", toolCalls: []byte(`[{"id":"call_a","type":"function","function":{"name":"x","arguments":"{}"}}]`)},
		{role: "tool", content: `{"r":1}`, toolCallID: pgtype.Text{String: "call_a", Valid: true}},
		{role: "user", content: "third"},
		{role: "user", content: "fourth"},
		{role: "user", content: "fifth"},
		{role: "user", content: "sixth"},
		{role: "user", content: strings.Repeat("x", selfHealUserMessageMaxLen+500)},
		{role: "user", content: "eighth"},
	}
	q := chatrepo.New(conn)
	for _, r := range seedRows {
		require.NoError(t, q.CreateChatMessageWithToolCalls(ctx, chatrepo.CreateChatMessageWithToolCallsParams{
			ChatID:     chatID,
			ProjectID:  uuid.NullUUID{UUID: projectID, Valid: true},
			Role:       r.role,
			Content:    r.content,
			ToolCalls:  r.toolCalls,
			ToolCallID: r.toolCallID,
		}))
	}

	var stopCalls atomic.Int64
	logger := testenv.NewLogger(t)
	tokens := assistanttokens.New("test-jwt-secret", conn, nil)
	corruption := fmt.Errorf("%w: execute fly turn request: status=400 body=provider error: messages: tool_use_id has no corresponding tool_use block", ErrHistoryCorrupted)
	backend := testRuntimeBackend{
		backend:    runtimeBackendFlyIO,
		runTurnErr: corruption,
		stopCalls:  &stopCalls,
	}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), conn, backend, nil, tokens, mustParseURLForServiceTest(t, "https://gram.example.com"), telemetry.NewStub(logger), nil)
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	admitted, err := core.AdmitPendingThreads(ctx, assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)

	result, err := core.ProcessThreadEvents(ctx, projectID, threadID)
	require.NoError(t, err)
	require.True(t, result.RetryAdmission, "self-heal must request re-admission")
	require.False(t, result.RuntimeActive, "self-heal must tear the runtime down so /configure replays with trimmed history")
	require.False(t, result.ProcessedAnyEvent)
	require.Equal(t, int64(1), stopCalls.Load(), "self-heal must Stop the runtime so the next admit cold-starts with trimmed history")

	// Event is back to pending with the corruption error stamped on it; the
	// next claim will bump attempts to 2 which gates self-heal off.
	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(ctx, assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, event.Status)
	require.EqualValues(t, 1, event.Attempts)
	require.True(t, event.LastError.Valid)
	require.Contains(t, event.LastError.String, "tool_use_id has no corresponding tool_use block")

	history, err := core.loadChatHistory(ctx, chatID, projectID)
	require.NoError(t, err)
	require.Len(t, history, selfHealUserMessageCap+1, "trimmed generation should contain the recovery notice + last 5 user messages")

	require.Equal(t, "user", history[0].Role)
	require.Contains(t, history[0].Content, "[gram self-heal]", "leading row must be the recovery notice")
	require.Contains(t, history[0].Content, "tools", "notice should nudge the agent to recover via tools before bothering the user")

	wantTail := []string{"fourth", "fifth", "sixth", "", "eighth"}
	for i, want := range wantTail {
		row := history[i+1]
		require.Equal(t, "user", row.Role, "trimmed row %d", i)
		require.Empty(t, row.ToolCalls)
		require.Empty(t, row.ToolCallID)
		if want == "" {
			// The oversize message must be truncated to the rune cap.
			require.Lenf(t, []rune(row.Content), selfHealUserMessageMaxLen,
				"oversized user message must be truncated to %d runes", selfHealUserMessageMaxLen)
		} else {
			require.Equal(t, want, row.Content, "trimmed tail must preserve recent user prompts verbatim")
		}
	}
}

func TestServiceCoreSkipsSelfHealAfterFirstRetry(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_self_heal_skip")
	require.NoError(t, err)

	projectID, assistantID, chatID, threadID := insertAssistantFixture(t, conn)

	ctx := t.Context()

	// Bump attempts past the self-heal threshold so the corruption hits the
	// terminal-fail branch instead.
	require.NoError(t, chatrepo.New(conn).CreateChatMessageWithToolCalls(ctx, chatrepo.CreateChatMessageWithToolCallsParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "user",
		Content:   "hello",
	}))
	// Simulate a prior attempt by claiming + resetting the event once so
	// attempts == 1 going in; the upcoming ClaimNextPendingEvent will bump
	// to 2 before processEventTurn fires.
	q := assistantsrepo.New(conn)
	_, err = q.ClaimNextPendingEvent(ctx, assistantsrepo.ClaimNextPendingEventParams{
		ProcessingStatus: eventStatusProcessing,
		ProjectID:        projectID,
		ThreadID:         threadID,
		PendingStatus:    eventStatusPending,
	})
	require.NoError(t, err)

	// Reset to pending without resetting attempts.
	eventRow, err := q.GetLatestAssistantThreadEventByThreadID(ctx, assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.NoError(t, q.ResetAssistantThreadEventToPending(ctx, assistantsrepo.ResetAssistantThreadEventToPendingParams{
		PendingStatus: eventStatusPending,
		LastError:     pgtype.Text{String: "prior corruption", Valid: true},
		EventID:       eventRow.ID,
		ProjectID:     projectID,
	}))

	var stopCalls atomic.Int64
	logger := testenv.NewLogger(t)
	tokens := assistanttokens.New("test-jwt-secret", conn, nil)
	corruption := fmt.Errorf("%w: provider error: messages: tool_use_id has no corresponding tool_use block", ErrHistoryCorrupted)
	backend := testRuntimeBackend{
		backend:    runtimeBackendFlyIO,
		runTurnErr: corruption,
		stopCalls:  &stopCalls,
	}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), conn, backend, nil, tokens, mustParseURLForServiceTest(t, "https://gram.example.com"), telemetry.NewStub(logger), nil)
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	admitted, err := core.AdmitPendingThreads(ctx, assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)

	result, err := core.ProcessThreadEvents(ctx, projectID, threadID)
	require.NoError(t, err)
	require.False(t, result.RetryAdmission, "second corruption must NOT retry — self-heal already ran")
	require.True(t, result.RuntimeActive, "completion-fail path keeps the runtime warm")
	require.Equal(t, int64(0), stopCalls.Load(), "second corruption must not Stop the runtime")

	event, err := q.GetLatestAssistantThreadEventByThreadID(ctx, assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, eventStatusFailed, event.Status, "second corruption must terminally fail")

	// Only the seeded gen 0 should exist — no second self-heal generation was written.
	gen, err := chatrepo.New(conn).GetMaxGenerationForChat(ctx, chatID)
	require.NoError(t, err)
	require.EqualValues(t, 0, gen, "self-heal must not run on retry attempts")
}
