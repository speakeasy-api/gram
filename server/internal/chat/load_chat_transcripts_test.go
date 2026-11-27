package chat_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newTranscriptStore(t *testing.T, ti *chatTestInstance) *transcriptstore.Store {
	t.Helper()
	store, _ := newMeteredTranscriptStore(t, ti)
	return store
}

// newMeteredTranscriptStore also returns a reader for the store's
// metrics, so a test can prove which messages were served from transcripts.
func newMeteredTranscriptStore(t *testing.T, ti *chatTestInstance) (*transcriptstore.Store, *sdkmetric.ManualReader) {
	t.Helper()
	keys := orgencryptiontest.New(t, ti.conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), ti.orgID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	reader := sdkmetric.NewManualReader()
	store, err := transcriptstore.New(ti.conn, keys.Service, testenv.NewLogger(t), testenv.NewTracerProvider(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	require.NoError(t, err)
	return store, reader
}

// readMessagesByOutcome sums the chat.transcript_shadow.read.messages counter
// by outcome.
func readMessagesByOutcome(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	counts := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "chat.transcript_shadow.read.messages" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				outcome, ok := point.Attributes.Value(attr.OutcomeKey)
				require.True(t, ok)
				counts[outcome.AsString()] += point.Value
			}
		}
	}
	return counts
}

func sealTranscripts(t *testing.T, ti *chatTestInstance, store *transcriptstore.Store, messageIDs ...uuid.UUID) {
	t.Helper()
	require.NoError(t, store.Prepare(t.Context(), ti.orgID))
	tx, err := ti.conn.Begin(t.Context()) //nolint:glint // notestingrawsql: Capture runs inside the caller's message transaction; message rows come from generated seed queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, store.Capture(t.Context(), tx, ti.orgID, ti.projectID, messageIDs))
	require.NoError(t, tx.Commit(t.Context()))
}

func TestLoadChat_ReadEnabledProjectOpensTranscripts(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "transcript reads")
	first := seedMessageContent(t, ctx, ti, chatID, "first sealed message")
	second := seedMessageContent(t, ctx, ti, chatID, "second sealed message")

	store, reader := newMeteredTranscriptStore(t, ti)
	sealTranscripts(t, ti, store, first, second)
	ti.service.WithTranscripts(transcriptstore.NewTranscripts(store, ti.projectID))

	result, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	contents := make(map[string]string, len(result.Messages))
	for _, m := range result.Messages {
		var text string
		require.NoError(t, json.Unmarshal(m.Content, &text))
		contents[m.ID] = text
	}
	require.Equal(t, map[string]string{
		first.String():  "first sealed message",
		second.String(): "second sealed message",
	}, contents)
	// A current transcript opens to the same text as the plaintext, so the body
	// alone cannot show where it came from. The read metric proves both
	// messages were opened from transcripts, with no plaintext fallback.
	require.Equal(t, map[string]int64{"success": 2}, readMessagesByOutcome(t, reader))
}

func TestLoadChat_ReadEnabledProjectServesPlaintextWhenNoTranscriptExists(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "partly captured")
	captured := seedMessageContent(t, ctx, ti, chatID, "captured message")
	uncaptured := seedMessageContent(t, ctx, ti, chatID, "message written before capture began")

	store := newTranscriptStore(t, ti)
	sealTranscripts(t, ti, store, captured)
	ti.service.WithTranscripts(transcriptstore.NewTranscripts(store, ti.projectID))

	result, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	contents := make(map[string]string, len(result.Messages))
	for _, m := range result.Messages {
		var text string
		require.NoError(t, json.Unmarshal(m.Content, &text))
		contents[m.ID] = text
	}
	require.Equal(t, map[string]string{
		captured.String():   "captured message",
		uncaptured.String(): "message written before capture began",
	}, contents)
}

func TestLoadChat_ReadEnabledProjectServesPlaintextForAStaleTranscript(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "stale transcript")
	edited := seedMessageContent(t, ctx, ti, chatID, "original message")

	store := newTranscriptStore(t, ti)
	sealTranscripts(t, ti, store, edited)
	// A write that skips capture leaves the transcript behind the plaintext.
	_, err := fixturerepo.New(ti.conn).ChangeContent(t.Context(), edited, uuid.NullUUID{UUID: ti.projectID, Valid: true})
	require.NoError(t, err)
	ti.service.WithTranscripts(transcriptstore.NewTranscripts(store, ti.projectID))

	// A disagreeing transcript is logged and the plaintext row is served.
	result, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	var text string
	require.NoError(t, json.Unmarshal(result.Messages[0].Content, &text))
	require.Equal(t, "changed", text)
}

func TestLoadChat_OtherProjectsKeepReadingPlaintext(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "plaintext reads")
	edited := seedMessageContent(t, ctx, ti, chatID, "original message")

	// The message has a transcript that is now stale. A project the service reads
	// transcripts for would fail this load; the service reads transcripts only for
	// another project, so this one keeps serving plaintext without touching
	// the transcript.
	store, reader := newMeteredTranscriptStore(t, ti)
	sealTranscripts(t, ti, store, edited)
	_, err := fixturerepo.New(ti.conn).ChangeContent(t.Context(), edited, uuid.NullUUID{UUID: ti.projectID, Valid: true})
	require.NoError(t, err)
	ti.service.WithTranscripts(transcriptstore.NewTranscripts(store, uuid.New()))

	result, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	var text string
	require.NoError(t, json.Unmarshal(result.Messages[0].Content, &text))
	require.Equal(t, "changed", text)
	require.Empty(t, readMessagesByOutcome(t, reader))
}
