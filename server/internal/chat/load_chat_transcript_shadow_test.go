package chat_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

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
	keys := orgencryptiontest.New(t, ti.conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), ti.orgID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	store, err := transcriptstore.New(ti.conn, keys.Service, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	return store
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

	store := newTranscriptShadowStore(t, ti)
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
	seedMessageContent(t, ctx, ti, chatID, "never captured")

	// The store reads only for another project, so this one has no shadows
	// and still loads.
	ti.service.WithTranscriptShadowReads(newTranscriptShadowStore(t, ti), uuid.New())

	result, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	var text string
	require.NoError(t, json.Unmarshal(result.Messages[0].Content, &text))
	require.Equal(t, "never captured", text)
}
