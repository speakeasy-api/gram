package assistants

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestRecordCompactedGenerationWritesNewGeneration(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_record_compacted")
	require.NoError(t, err)

	projectID, assistantID, chatID, threadID := insertAssistantFixture(t, conn)
	ctx := t.Context()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:   "org-test",
		Name: "Test organization",
		Slug: "org-test",
	})
	require.NoError(t, err)
	ownerUserID := "owner-" + uuid.NewString()
	require.NoError(t, assistantrepo.New(conn).UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      projectID,
		OrganizationID: "org-test",
		UserID:         conv.ToPGText(ownerUserID),
		Title:          pgtype.Text{},
	}))
	account, err := hooksrepo.New(conn).UpsertUserAccount(ctx, hooksrepo.UpsertUserAccountParams{
		OrganizationID:      "org-test",
		Provider:            "anthropic",
		ExternalAccountUuid: uuid.NewString(),
		UserID:              pgtype.Text{},
		ExternalOrgID:       pgtype.Text{},
		ExternalAccountID:   pgtype.Text{},
		Email:               conv.ToPGText("compaction@example.test"),
		AccountType:         conv.ToPGText("personal"),
	})
	require.NoError(t, err)
	_, err = hooksrepo.New(conn).LinkChatUserAccount(ctx, hooksrepo.LinkChatUserAccountParams{
		UserAccountID: uuid.NullUUID{UUID: account.ID, Valid: true},
		ID:            chatID,
		ProjectID:     projectID,
	})
	require.NoError(t, err)

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
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	// Compacted transcript: one summary + a couple of preserved recent turns.
	compacted := []runtimeMessage{
		{Role: "system", Content: runtimeTextContent("<<summary of prior turns>>")},
		{Role: "user", Content: runtimeTextContent("third cron fire")},
		{Role: "assistant", Content: runtimeTextContent("summary of work so far")},
	}

	require.NoError(t, core.RecordCompactedGeneration(ctx, projectID, threadID, assistantID, compacted))

	maxGen, err := q.GetMaxGenerationForChat(ctx, chatrepo.GetMaxGenerationForChatParams{ChatID: chatID, ProjectID: projectID})
	require.NoError(t, err)
	require.EqualValues(t, 2, maxGen, "compacted write must land in a fresh generation, not append to gen 1")

	history, err := core.loadChatHistory(ctx, chatID, projectID)
	require.NoError(t, err)
	// loadChatHistory drops system rows. The compacted transcript had one
	// system row (the summary) + 2 user/assistant rows; loadChatHistory must
	// return the latter two.
	require.Len(t, history, 2, "latest generation must contain only the compacted shape, minus system rows")
	require.Equal(t, "user", history[0].Role)
	require.Equal(t, "third cron fire", history[0].Content.Text())
	require.Equal(t, "assistant", history[1].Role)
	require.Equal(t, "summary of work so far", history[1].Content.Text())

	outboxRows, err := testrepo.New(conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Len(t, outboxRows, len(compacted))
	for _, row := range outboxRows {
		reading := &meteringv1.MeterReading{}
		require.NoError(t, proto.Unmarshal(row.Message, reading))
		require.Equal(t, "personal", reading.GetAttributes()[metering.AttributeAccountType])
		require.NotContains(t, reading.GetAttributes(), metering.AttributeMessageUserID)
		require.NotContains(t, reading.GetAttributes(), metering.AttributeMessageExternalUserID)
	}
}

func TestRecordCompactedGenerationRejectsForeignAssistant(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_record_compacted_foreign")
	require.NoError(t, err)

	projectID, _, _, threadID := insertAssistantFixture(t, conn)
	ctx := t.Context()

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	stranger := uuid.New()
	compacted := []runtimeMessage{{Role: "user", Content: runtimeTextContent("x")}}
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
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	return core, projectID, assistantID, threadID, ctx
}

func TestRecordCompactedGenerationRejectsToolRowMissingToolCallID(t *testing.T) {
	t.Parallel()
	core, projectID, assistantID, threadID, ctx := recordCompactedGenerationMalformedFixture(t, "assistants_record_compacted_malformed_tool_id")
	msgs := []runtimeMessage{{Role: "tool", Content: runtimeTextContent("x")}}
	require.Error(t, core.RecordCompactedGeneration(ctx, projectID, threadID, assistantID, msgs), "tool row without tool_call_id must be rejected")
}

func TestRecordCompactedGenerationRejectsUnknownRole(t *testing.T) {
	t.Parallel()
	core, projectID, assistantID, threadID, ctx := recordCompactedGenerationMalformedFixture(t, "assistants_record_compacted_malformed_role")
	msgs := []runtimeMessage{{Role: "narrator", Content: runtimeTextContent("x")}}
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
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(ctx) })
	core.SetChatMessageWriter(chatWriter)

	err = core.RecordCompactedGeneration(ctx, projectID, threadID, assistantID, nil)
	require.Error(t, err, "empty compacted transcript must be rejected — there is nothing to persist")
}
