package assistants

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRecordCompactedGenerationWritesNewGeneration(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_record_compacted")
	require.NoError(t, err)

	projectID, assistantID, chatID, threadID := insertAssistantFixture(t, conn)
	ctx := t.Context()

	// Seed a long-ish generation 1 — the un-compacted history that cron is
	// currently re-loading every fire.
	seedRows := []struct {
		role       string
		content    string
		toolCalls  []byte
		toolCallID pgtype.Text
	}{
		{role: "user", content: "first cron fire"},
		{role: "assistant", content: "did a thing"},
		{role: "user", content: "second cron fire"},
		{role: "assistant", content: "did another thing"},
		{role: "user", content: "third cron fire"},
		{role: "assistant", content: "summary of work so far"},
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
			Generation: 1,
		}))
	}

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil)
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	// Compacted transcript: one summary + a couple of preserved recent turns.
	compacted := []runtimeMessage{
		{Role: "system", Content: "<<summary of prior turns>>"},
		{Role: "user", Content: "third cron fire"},
		{Role: "assistant", Content: "summary of work so far"},
	}

	require.NoError(t, core.RecordCompactedGeneration(ctx, projectID, threadID, assistantID, compacted))

	maxGen, err := q.GetMaxGenerationForChat(ctx, chatID)
	require.NoError(t, err)
	require.EqualValues(t, 2, maxGen, "compacted write must land in a fresh generation, not append to gen 1")

	history, err := core.loadChatHistory(ctx, chatID, projectID)
	require.NoError(t, err)
	// loadChatHistory drops system rows. The compacted transcript had one
	// system row (the summary) + 2 user/assistant rows; loadChatHistory must
	// return the latter two.
	require.Len(t, history, 2, "latest generation must contain only the compacted shape, minus system rows")
	require.Equal(t, "user", history[0].Role)
	require.Equal(t, "third cron fire", history[0].Content)
	require.Equal(t, "assistant", history[1].Role)
	require.Equal(t, "summary of work so far", history[1].Content)
}

func TestRecordCompactedGenerationRejectsForeignAssistant(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_record_compacted_foreign")
	require.NoError(t, err)

	projectID, _, _, threadID := insertAssistantFixture(t, conn)
	ctx := t.Context()

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil)
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	stranger := uuid.New()
	compacted := []runtimeMessage{{Role: "user", Content: "x"}}
	err = core.RecordCompactedGeneration(ctx, projectID, threadID, stranger, compacted)
	require.Error(t, err, "principal must own the thread's assistant")
}

// recordCompactedGenerationMalformedFixture builds a self-contained
// fixture for the malformed-message rejection tests. Each malformed-shape
// scenario lives in its own Test* function to comply with the project's
// no-t.Run convention.
func recordCompactedGenerationMalformedFixture(t *testing.T, slug string) (*ServiceCore, uuid.UUID, uuid.UUID, uuid.UUID, context.Context) {
	t.Helper()

	conn, err := assistantsInfra.CloneTestDatabase(t, slug)
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	ctx := t.Context()

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil)
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	return core, projectID, assistantID, threadID, ctx
}

func TestRecordCompactedGenerationRejectsToolRowMissingToolCallID(t *testing.T) {
	t.Parallel()
	core, projectID, assistantID, threadID, ctx := recordCompactedGenerationMalformedFixture(t, "assistants_record_compacted_malformed_tool_id")
	msgs := []runtimeMessage{{Role: "tool", Content: "x"}}
	require.Error(t, core.RecordCompactedGeneration(ctx, projectID, threadID, assistantID, msgs), "tool row without tool_call_id must be rejected")
}

func TestRecordCompactedGenerationRejectsUnknownRole(t *testing.T) {
	t.Parallel()
	core, projectID, assistantID, threadID, ctx := recordCompactedGenerationMalformedFixture(t, "assistants_record_compacted_malformed_role")
	msgs := []runtimeMessage{{Role: "narrator", Content: "x"}}
	require.Error(t, core.RecordCompactedGeneration(ctx, projectID, threadID, assistantID, msgs), "unknown role must be rejected")
}

func TestRecordCompactedGenerationRejectsAssistantToolCallMissingID(t *testing.T) {
	t.Parallel()
	core, projectID, assistantID, threadID, ctx := recordCompactedGenerationMalformedFixture(t, "assistants_record_compacted_malformed_tc_id")
	msgs := []runtimeMessage{{
		Role:      "assistant",
		ToolCalls: []runtimeToolCall{{ID: "", Name: "x", Arguments: "{}"}},
	}}
	require.Error(t, core.RecordCompactedGeneration(ctx, projectID, threadID, assistantID, msgs), "assistant tool_call without id must be rejected")
}

func TestRecordCompactedGenerationRejectsAssistantToolCallMissingName(t *testing.T) {
	t.Parallel()
	core, projectID, assistantID, threadID, ctx := recordCompactedGenerationMalformedFixture(t, "assistants_record_compacted_malformed_tc_name")
	msgs := []runtimeMessage{{
		Role:      "assistant",
		ToolCalls: []runtimeToolCall{{ID: "c", Name: "", Arguments: "{}"}},
	}}
	require.Error(t, core.RecordCompactedGeneration(ctx, projectID, threadID, assistantID, msgs), "assistant tool_call without name must be rejected")
}

func TestRecordCompactedGenerationRejectsEmptyMessages(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_record_compacted_empty")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	ctx := t.Context()

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil)
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	err = core.RecordCompactedGeneration(ctx, projectID, threadID, assistantID, nil)
	require.Error(t, err, "empty compacted transcript must be rejected — there is nothing to persist")
}
