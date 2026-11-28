package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const shadowReadMessagesMetric = "chat.transcript_shadow.read.messages"

type shadowHistoryFixture struct {
	conn        *pgxpool.Pool
	projectID   uuid.UUID
	assistantID uuid.UUID
	chatID      uuid.UUID
	core        *ServiceCore
	store       *transcriptstore.Store
	reads       *sdkmetric.ManualReader
}

// newShadowHistoryFixture seeds a thread with the given user/assistant
// exchanges and a shadow store whose read metrics the test can collect.
func newShadowHistoryFixture(t *testing.T, name string, contents ...string) shadowHistoryFixture {
	t.Helper()
	ctx := t.Context()
	conn, err := assistantsInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	projectID, assistantID, chatID, _ := insertAssistantFixture(t, conn)
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:   "org-test",
		Name: "Test organization",
		Slug: "org-test",
	})
	require.NoError(t, err)

	q := chatrepo.New(conn)
	for i, content := range contents {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		require.NoError(t, q.CreateChatMessageWithToolCalls(ctx, chatrepo.CreateChatMessageWithToolCallsParams{
			ChatID:     chatID,
			ProjectID:  uuid.NullUUID{UUID: projectID, Valid: true},
			Role:       role,
			Content:    content,
			ToolCalls:  nil,
			ToolCallID: pgtype.Text{},
			Generation: 0,
		}))
	}

	keys := orgencryptiontest.New(t, conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(ctx, "org-test", orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	reads := sdkmetric.NewManualReader()
	store, err := transcriptstore.New(conn, keys.Service, testenv.NewTracerProvider(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reads)))
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendGKE, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, conn))

	return shadowHistoryFixture{conn: conn, projectID: projectID, assistantID: assistantID, chatID: chatID, core: core, store: store, reads: reads}
}

func (f shadowHistoryFixture) messages(t *testing.T) []chatrepo.ChatMessage {
	t.Helper()
	messages, err := chatrepo.New(f.conn).ListLatestGenerationChatMessages(t.Context(), chatrepo.ListLatestGenerationChatMessagesParams{ChatID: f.chatID, ProjectID: f.projectID})
	require.NoError(t, err)
	return messages
}

func (f shadowHistoryFixture) capture(t *testing.T, messageIDs ...uuid.UUID) {
	t.Helper()
	require.NoError(t, f.store.Prepare(t.Context(), "org-test"))
	tx, err := f.conn.Begin(t.Context()) //nolint:glint // notestingrawsql: Capture runs inside the caller's message transaction; message rows come from generated seed queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, f.store.Capture(t.Context(), tx, "org-test", f.projectID, messageIDs))
	require.NoError(t, tx.Commit(t.Context()))
}

// readOutcomes sums the shadow read message counter by outcome.
func (f shadowHistoryFixture) readOutcomes(t *testing.T) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, f.reads.Collect(t.Context(), &collected))
	outcomes := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != shadowReadMessagesMetric {
				continue
			}
			for _, point := range sum.DataPoints {
				outcome, ok := point.Attributes.Value(attr.OutcomeKey)
				require.True(t, ok)
				if point.Value != 0 {
					outcomes[outcome.AsString()] += point.Value
				}
			}
		}
	}
	return outcomes
}

func historyTexts(history []runtimeMessage) []string {
	texts := make([]string, len(history))
	for i, m := range history {
		texts[i] = m.Content.Text()
	}
	return texts
}

func TestLoadChatHistory_ReadEnabledProjectOpensEncryptedShadows(t *testing.T) {
	t.Parallel()
	f := newShadowHistoryFixture(t, "assistants_shadow_history_open", "plan the launch party", "here is a plan for the launch party")
	messages := f.messages(t)
	f.capture(t, messages[0].ID, messages[1].ID)
	f.core.SetTranscriptShadowReads(f.store, f.projectID)

	history, err := f.core.loadChatHistory(t.Context(), f.chatID, f.projectID)
	require.NoError(t, err)
	require.Equal(t, []string{"plan the launch party", "here is a plan for the launch party"}, historyTexts(history))
	require.Equal(t, map[string]int64{"success": 2}, f.readOutcomes(t))
}

func TestLoadChatHistory_ReadEnabledProjectServesPlaintextWhenNoShadowExists(t *testing.T) {
	t.Parallel()
	f := newShadowHistoryFixture(t, "assistants_shadow_history_fallback", "message written before capture began", "captured reply")
	messages := f.messages(t)
	f.capture(t, messages[1].ID)
	f.core.SetTranscriptShadowReads(f.store, f.projectID)

	history, err := f.core.loadChatHistory(t.Context(), f.chatID, f.projectID)
	require.NoError(t, err)
	require.Equal(t, []string{"message written before capture began", "captured reply"}, historyTexts(history))
	require.Equal(t, map[string]int64{"success": 1, "plaintext_fallback": 1}, f.readOutcomes(t))
}

func TestLoadChatHistory_ReadEnabledProjectFailsOnAStaleShadow(t *testing.T) {
	t.Parallel()
	f := newShadowHistoryFixture(t, "assistants_shadow_history_stale", "original message", "original reply")
	messages := f.messages(t)
	f.capture(t, messages[0].ID, messages[1].ID)
	// A write that skips capture leaves the shadow behind the plaintext.
	_, err := fixturerepo.New(f.conn).ChangeContent(t.Context(), messages[0].ID, uuid.NullUUID{UUID: f.projectID, Valid: true})
	require.NoError(t, err)
	f.core.SetTranscriptShadowReads(f.store, f.projectID)

	_, err = f.core.loadChatHistory(t.Context(), f.chatID, f.projectID)
	require.ErrorIs(t, err, transcriptstore.ErrStale)
	require.Equal(t, map[string]int64{"stale": 2}, f.readOutcomes(t))
}

func TestSelfHealCorruptHistory_StaleShadowLeavesHistoryUntouched(t *testing.T) {
	t.Parallel()
	f := newShadowHistoryFixture(t, "assistants_shadow_self_heal_stale", "original message", "original reply")
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), f.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(t.Context()) })
	f.core.SetChatMessageWriter(chatWriter)
	messages := f.messages(t)
	f.capture(t, messages[0].ID, messages[1].ID)
	_, err := fixturerepo.New(f.conn).ChangeContent(t.Context(), messages[0].ID, uuid.NullUUID{UUID: f.projectID, Valid: true})
	require.NoError(t, err)
	f.core.SetTranscriptShadowReads(f.store, f.projectID)

	err = f.core.selfHealCorruptHistory(t.Context(), f.chatID, f.projectID, f.assistantID)
	require.ErrorIs(t, err, transcriptstore.ErrStale)
	// Self-heal must not write a new generation from plaintext the shadow
	// contradicts.
	after := f.messages(t)
	require.Len(t, after, len(messages))
	for i := range after {
		require.Equal(t, messages[i].ID, after[i].ID)
	}
}

func TestSelfHealCorruptHistory_ReadEnabledProjectOpensEncryptedShadows(t *testing.T) {
	t.Parallel()
	f := newShadowHistoryFixture(t, "assistants_shadow_self_heal_open", "keep this prompt", "dropped reply")
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), f.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(t.Context()) })
	f.core.SetChatMessageWriter(chatWriter)
	messages := f.messages(t)
	f.capture(t, messages[0].ID, messages[1].ID)
	f.core.SetTranscriptShadowReads(f.store, f.projectID)

	require.NoError(t, f.core.selfHealCorruptHistory(t.Context(), f.chatID, f.projectID, f.assistantID))
	// Only the kept user prompt is opened; the dropped reply is not read.
	require.Equal(t, map[string]int64{"success": 1}, f.readOutcomes(t))
	after := f.messages(t)
	require.Len(t, after, 2, "recovery notice plus the one user prompt")
	require.Equal(t, "keep this prompt", after[1].Content)
}

func TestSelfHealCorruptHistory_StaleShadowOnDroppedReplyDoesNotBlockRecovery(t *testing.T) {
	t.Parallel()
	f := newShadowHistoryFixture(t, "assistants_shadow_self_heal_dropped", "keep this prompt", "broken reply")
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), f.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = chatWriterShutdown(t.Context()) })
	f.core.SetChatMessageWriter(chatWriter)
	messages := f.messages(t)
	f.capture(t, messages[0].ID, messages[1].ID)
	// The reply self-heal drops has a stale shadow. Self-heal never reads it,
	// so recovery still runs.
	_, err := fixturerepo.New(f.conn).ChangeContent(t.Context(), messages[1].ID, uuid.NullUUID{UUID: f.projectID, Valid: true})
	require.NoError(t, err)
	f.core.SetTranscriptShadowReads(f.store, f.projectID)

	require.NoError(t, f.core.selfHealCorruptHistory(t.Context(), f.chatID, f.projectID, f.assistantID))
	require.Equal(t, map[string]int64{"success": 1}, f.readOutcomes(t))
	after := f.messages(t)
	require.Len(t, after, 2, "recovery notice plus the one user prompt")
	require.Equal(t, "keep this prompt", after[1].Content)
}

func TestLoadChatHistory_OtherProjectsKeepReadingPlaintext(t *testing.T) {
	t.Parallel()
	f := newShadowHistoryFixture(t, "assistants_shadow_history_other", "never captured", "plaintext reply")
	// The store reads only for another project, so this one has no shadows
	// and still loads without touching the shadow store.
	f.core.SetTranscriptShadowReads(f.store, uuid.New())

	history, err := f.core.loadChatHistory(t.Context(), f.chatID, f.projectID)
	require.NoError(t, err)
	require.Equal(t, []string{"never captured", "plaintext reply"}, historyTexts(history))
	require.Empty(t, f.readOutcomes(t))
}
