package chat_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
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
	require.NoError(t, store.ResealChatMessages(t.Context(), tx, ti.orgID, ti.projectID, messageIDs))
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

func TestLoadChat_SearchScansEncryptedShadowsInChunks(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "chunked search")
	// More than one chunk of messages; positions 13 and 215 carry the needle,
	// one in each chunk.
	total := transcriptstore.SearchChunkMessages + 30
	ids := make([]uuid.UUID, total)
	for i := range total {
		content := "ordinary message"
		if i+1 == 13 || i+1 == 215 {
			content = "this line has a NEEDLE in it"
		}
		ids[i] = seedMessageContent(t, ctx, ti, chatID, content)
	}
	store, reader := newMeteredTranscriptStore(t, ti)
	sealTranscripts(t, ti, store, ids...)
	ti.service.WithTranscripts(transcriptstore.NewTranscripts(store, ti.projectID))

	query := "needle"
	p := loadPayload(chatID.String())
	p.Query = &query
	first, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.NotNil(t, first.ScanHasMore)
	require.True(t, *first.ScanHasMore, "the first request scans one chunk")
	require.NotNil(t, first.NextScanCursor)
	require.EqualValues(t, transcriptstore.SearchChunkMessages, *first.ScannedMessages)
	require.Len(t, first.MatchSegments, 1)
	require.Len(t, first.Messages, 11)
	require.Equal(t, ids[12].String(), first.Messages[5].ID)
	var text string
	require.NoError(t, json.Unmarshal(first.Messages[5].Content, &text))
	require.Equal(t, "this line has a NEEDLE in it", text)

	p.ScanCursor = first.NextScanCursor
	generation := first.Generation
	p.Generation = &generation
	second, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.False(t, *second.ScanHasMore, "the second chunk reaches the end")
	require.Nil(t, second.NextScanCursor)
	require.EqualValues(t, total, *second.ScannedMessages)
	require.Len(t, second.MatchSegments, 1)
	require.Len(t, second.Messages, 11)
	require.Equal(t, ids[214].String(), second.Messages[5].ID)
	// Every message scanned and every message shown was opened from its
	// shadow, with no plaintext fallback.
	require.Equal(t, map[string]int64{"success": int64(total + 22)}, readMessagesByOutcome(t, reader))

	bad := "not a cursor"
	p.ScanCursor = &bad
	_, err = ti.service.LoadChat(ctx, p)
	require.ErrorIs(t, err, transcriptstore.ErrInvalidCursor)

	// A search continuation reuses the open's audit record, but a scan_cursor
	// on a read that is not a search does not skip it.
	audited := func() int64 {
		t.Helper()
		n, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
		require.NoError(t, err)
		return n
	}
	before := audited()
	p.ScanCursor = first.NextScanCursor
	_, err = ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Equal(t, before, audited(), "a search continuation is not logged again")
	plain := loadPayload(chatID.String())
	plain.ScanCursor = first.NextScanCursor
	_, err = ti.service.LoadChat(ctx, plain)
	require.NoError(t, err)
	require.Equal(t, before+1, audited(), "a transcript read with a stray scan_cursor is logged")
}
