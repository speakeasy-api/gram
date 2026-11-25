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

func newTranscriptShadowStore(t *testing.T, ti *chatTestInstance) *transcriptstore.Store {
	t.Helper()
	store, _ := newMeteredTranscriptShadowStore(t, ti)
	return store
}

// newMeteredTranscriptShadowStore also returns a reader for the store's
// metrics, so a test can prove which messages were served from shadows.
func newMeteredTranscriptShadowStore(t *testing.T, ti *chatTestInstance) (*transcriptstore.Store, *sdkmetric.ManualReader) {
	t.Helper()
	keys := orgencryptiontest.New(t, ti.conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), ti.orgID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	reader := sdkmetric.NewManualReader()
	store, err := transcriptstore.New(ti.conn, keys.Service, testenv.NewTracerProvider(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
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

func captureTranscriptShadows(t *testing.T, ti *chatTestInstance, store *transcriptstore.Store, messageIDs ...uuid.UUID) {
	t.Helper()
	tx, err := ti.conn.Begin(t.Context()) //nolint:glint // notestingrawsql: Capture runs inside the caller's message transaction; message rows come from generated seed queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, store.Capture(t.Context(), tx, ti.orgID, ti.projectID, messageIDs))
	require.NoError(t, tx.Commit(t.Context()))
}

func TestLoadChat_ReadEnabledProjectOpensEncryptedShadows(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "shadow reads")
	first := seedMessageContent(t, ctx, ti, chatID, "first sealed message")
	second := seedMessageContent(t, ctx, ti, chatID, "second sealed message")

	store, reader := newMeteredTranscriptShadowStore(t, ti)
	captureTranscriptShadows(t, ti, store, first, second)
	ti.service.WithTranscriptShadowReads(store, ti.projectID)

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
	// A current shadow opens to the same text as the plaintext, so the body
	// alone cannot show where it came from. The read metric proves both
	// messages were opened from shadows, with no plaintext fallback.
	require.Equal(t, map[string]int64{"success": 2}, readMessagesByOutcome(t, reader))
}

func TestLoadChat_ReadEnabledProjectServesPlaintextWhenNoShadowExists(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "partly captured")
	captured := seedMessageContent(t, ctx, ti, chatID, "captured message")
	uncaptured := seedMessageContent(t, ctx, ti, chatID, "message written before capture began")

	store := newTranscriptShadowStore(t, ti)
	captureTranscriptShadows(t, ti, store, captured)
	ti.service.WithTranscriptShadowReads(store, ti.projectID)

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

func TestLoadChat_ReadEnabledProjectFailsOnAStaleShadow(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "stale shadow")
	edited := seedMessageContent(t, ctx, ti, chatID, "original message")

	store := newTranscriptShadowStore(t, ti)
	captureTranscriptShadows(t, ti, store, edited)
	// A write that skips capture leaves the shadow behind the plaintext.
	_, err := fixturerepo.New(ti.conn).ChangeContent(t.Context(), edited, uuid.NullUUID{UUID: ti.projectID, Valid: true})
	require.NoError(t, err)
	ti.service.WithTranscriptShadowReads(store, ti.projectID)

	_, err = ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.ErrorIs(t, err, transcriptstore.ErrStale)
	// An existing shadow that fails verification never falls back to plaintext.
	require.NotContains(t, err.Error(), "changed")
}

func TestLoadChat_OtherProjectsKeepReadingPlaintext(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "plaintext reads")
	edited := seedMessageContent(t, ctx, ti, chatID, "original message")

	// The message has a shadow that is now stale. A project that captures
	// shadows would fail this load; this project does not, so it must
	// keep serving plaintext without touching the shadow.
	store, reader := newMeteredTranscriptShadowStore(t, ti)
	captureTranscriptShadows(t, ti, store, edited)
	_, err := fixturerepo.New(ti.conn).ChangeContent(t.Context(), edited, uuid.NullUUID{UUID: ti.projectID, Valid: true})
	require.NoError(t, err)
	ti.service.WithTranscriptShadowReads(store, uuid.New())

	result, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	var text string
	require.NoError(t, json.Unmarshal(result.Messages[0].Content, &text))
	require.Equal(t, "changed", text)
	require.Empty(t, readMessagesByOutcome(t, reader))
}
