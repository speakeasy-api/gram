package hooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type failingFeatures struct{}

func (failingFeatures) IsFeatureEnabled(_ context.Context, _ string, _ productfeatures.Feature) (bool, error) {
	return false, errors.New("feature lookup unavailable")
}

// seedCaptureSession wires up an attributed Claude session for capture tests:
// a connected user plus cached session metadata so resolveClaudeSessionMetadata
// resolves the project/org.
func seedCaptureSession(t *testing.T, ctx context.Context, ti *testInstance, sessionID, userID, userEmail string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   userEmail,
		UserID:      userID,
		ClaudeOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}, time.Hour))
}

// TestClaudeMessages_DeliveredTwiceStoredOnce is the core dedup guarantee: two
// plugin installations firing the same Stop send the same transcript-uuid batch,
// and the server stores each message exactly once (ON CONFLICT on
// external_message_id).
func TestClaudeMessages_DeliveredTwiceStoredOnce(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	seedCaptureSession(t, ctx, ti, sessionID, "capture-user", "capture@example.com")

	userContent := "hello from the user"
	asstContent := "the assistant reply"
	model := "claude-opus-4-8"
	batch := func() *gen.ClaudeMessagesPayload {
		return &gen.ClaudeMessagesPayload{
			SessionID: sessionID,
			Messages: []*gen.ClaudeCapturedMessage{
				{ExternalID: uuid.NewString(), Role: "user", Content: &userContent},
				{ExternalID: uuid.NewString(), Role: "assistant", Content: &asstContent, Model: &model},
			},
		}
	}
	// Both installs observe the same transcript, so the external_ids are identical
	// across deliveries — reuse one batch value rather than minting new uuids.
	payload := batch()

	require.NoError(t, ti.service.ClaudeMessages(ctx, payload))

	var msgs []chatRepo.ChatMessage
	require.Eventually(t, func() bool {
		var err error
		msgs, err = chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(msgs) == 2
	}, 2*time.Second, 25*time.Millisecond, "first batch should persist two rows")

	// Second plugin install fires the identical batch.
	require.NoError(t, ti.service.ClaudeMessages(ctx, payload))

	require.Never(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) != 2
	}, 500*time.Millisecond, 50*time.Millisecond, "re-delivery must dedup on external_message_id")

	for _, msg := range msgs {
		require.True(t, msg.ExternalMessageID.Valid, "captured messages must carry external_message_id for dedup")
	}
}

func TestClaudeMessages_BufferedUntilSessionMetadataResolves(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	userContent := "captured after metadata arrives"
	payload := &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{
			{ExternalID: uuid.NewString(), Role: "user", Content: &userContent},
		},
	}

	require.NoError(t, ti.service.ClaudeMessages(otelOnlyCtx(ctx), payload))

	var buffered []gen.ClaudeMessagesPayload
	require.NoError(t, ti.service.cache.ListRange(ctx, claudeMessagesPendingCacheKey(sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1, "batch should be buffered while metadata is unavailable")

	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "buffered-capture-user", "buffered-capture@example.com")
	metadata := SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   "buffered-capture@example.com",
		UserID:      "buffered-capture-user",
		ClaudeOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), metadata, time.Hour))

	ti.service.flushPendingHooks(ctx, sessionID, &metadata)

	require.Eventually(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) == 1 && got[0].Content == userContent
	}, 2*time.Second, 25*time.Millisecond, "buffered batch should persist after metadata is cached")

	var after []gen.ClaudeMessagesPayload
	require.NoError(t, ti.service.cache.ListRange(ctx, claudeMessagesPendingCacheKey(sessionID), 0, -1, &after))
	require.Empty(t, after, "buffer should be deleted after flush")
}

func TestClaudeMessages_PendingBufferSurvivesReplayFailure(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = failingFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	userContent := "keep me buffered"
	payload := &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{
			{ExternalID: uuid.NewString(), Role: "user", Content: &userContent},
		},
	}
	require.NoError(t, ti.service.bufferClaudeMessages(ctx, sessionID, payload))

	metadata := SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   "buffered-capture@example.com",
		UserID:      "buffered-capture-user",
		ClaudeOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}

	ti.service.flushPendingClaudeMessages(ctx, sessionID, &metadata)

	var buffered []gen.ClaudeMessagesPayload
	require.NoError(t, ti.service.cache.ListRange(ctx, claudeMessagesPendingCacheKey(sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1, "failed replay must keep the pending batch for a later flush")
}

func TestClaudeMessages_SkipsToolMessageWithoutToolCallID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	seedCaptureSession(t, ctx, ti, sessionID, "tool-user", "tool@example.com")

	content := "orphaned tool result"
	require.NoError(t, ti.service.ClaudeMessages(ctx, &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{
			{ExternalID: uuid.NewString(), Role: "tool", Content: &content},
		},
	}))

	require.Never(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) > 0
	}, 500*time.Millisecond, 50*time.Millisecond, "tool messages without tool_call_id must be skipped")
}

// TestClaudeHookVersion_StopCollectionSkipsPerEventPersist verifies the version
// gate: a post-Stop-collection plugin (sends X-Gram-Hook-Version) must NOT
// persist chat_messages on the per-event handlers — capture comes from the Stop
// batch instead, so persisting here would double-write.
func TestClaudeHookVersion_StopCollectionSkipsPerEventPersist(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	seedCaptureSession(t, ctx, ti, sessionID, "gate-user", "gate@example.com")

	version := claudeHookStopCollectionVersion
	prompt := "should not persist on the per-event handler"
	_, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
		HookVersion:   &version,
	})
	require.NoError(t, err)

	require.Never(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) > 0
	}, 500*time.Millisecond, 50*time.Millisecond, "Stop-collection UserPromptSubmit must not persist per-event")
}
