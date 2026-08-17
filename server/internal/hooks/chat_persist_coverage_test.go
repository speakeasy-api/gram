package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	chatv1 "github.com/speakeasy-api/gram/infra/gen/gram/chat/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type disabledFeatures struct{}

func (disabledFeatures) IsFeatureEnabled(_ context.Context, _ string, _ productfeatures.Feature) (bool, error) {
	return false, nil
}

// newTestHookMessage builds a message the way the publisher would, so a test
// can exercise the persister on a shape the ingest path cannot easily produce.
func newTestHookMessage(sessionID, orgID string, chatID, projectID uuid.UUID, role, content string) *chatv1.HookMessage {
	id := uuid.Must(uuid.NewV7()).String()
	chat := chatID.String()
	project := projectID.String()
	createdAt := time.Now().UTC().Format(time.RFC3339)
	empty := ""

	return chatv1.HookMessage_builder{
		Id:        &id,
		ChatId:    &chat,
		ProjectId: &project,
		Role:      &role,
		Content:   &content,
		CreatedAt: &createdAt,
		Session: chatv1.HookMessage_SessionRef_builder{
			SessionId:      &sessionID,
			OrganizationId: &orgID,
			UserId:         &empty,
			UserEmail:      &empty,
			UserAccountId:  &empty,
		}.Build(),
	}.Build()
}

// The session-capture entitlement moved off the request path and onto the
// handler. If it stops being enforced there, an org that never bought session
// capture starts accumulating transcript rows, and nothing upstream is checking
// any more.
func TestChatPersister_SkipsWriteWhenSessionCaptureDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "capture-disabled-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	msg := newTestHookMessage(sessionID, authCtx.ActiveOrganizationID, chatID, *authCtx.ProjectID, "user", "should not be stored")

	notifier := &recordingNotifier{}
	persister := NewChatPersister(
		ti.service.logger,
		ti.conn,
		cache.NewRedisCacheAdapter(ti.redisClient),
		disabledFeatures{},
		notifier,
	)

	stored, err := persister.Persist(t.Context(), msg)
	require.NoError(t, err, "a disabled entitlement is not an error; it must ack")
	require.False(t, stored)

	messages, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Empty(t, messages, "session capture is off; the handler must write nothing")
	require.Empty(t, notifier.notified(), "nothing was stored, so nothing may wake")
}

// created_at is not decoration: the transcript index is
// (chat_id, generation, created_at, seq) and readers order on it. The
// synchronous path stores the event time at full precision. If the async path
// truncates it, every row in the same second ties on created_at and falls back
// to seq — which on this path is Pub/Sub delivery order, not event order.
func TestChatPersister_PreservesEventTimeAtFullPrecision(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Inside the backdate clamp, and deliberately not on a second boundary.
	occurred := time.Now().UTC().Add(-5 * time.Second).Truncate(time.Microsecond)
	require.NotZero(t, occurred.Nanosecond(), "the fixture must carry sub-second precision")
	occurredRaw := occurred.Format(time.RFC3339Nano)

	prompt := "precision prompt " + uuid.NewString()

	// Synchronous path, same event time.
	syncSession := "precision-sync-" + uuid.NewString()
	syncPayload := canonicalIngestPayload("claude", "prompt.submitted", syncSession)
	syncPayload.Event.OccurredAt = &occurredRaw
	syncPayload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	_, err := ti.service.IngestAuthenticated(t.Context(), authCtx, syncPayload)
	require.NoError(t, err)

	// Async path, same event time.
	enableAsyncChatPersist(t, ti, authCtx)
	asyncSession := "precision-async-" + uuid.NewString()
	asyncPayload := canonicalIngestPayload("claude", "prompt.submitted", asyncSession)
	asyncPayload.Event.OccurredAt = &occurredRaw
	asyncPayload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	_, err = ti.service.IngestAuthenticated(t.Context(), authCtx, asyncPayload)
	require.NoError(t, err)

	published := ti.chatMessages.published()
	require.Len(t, published, 1)

	handler := NewHookMessageHandler(ti.service.logger, newTestChatPersister(ti))
	require.NoError(t, handler.Handle(t.Context(), published[0], gcp.MessageMetadata{ID: "precision-1"}))

	syncRows, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(syncSession),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, syncRows, 1)

	asyncRows, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(asyncSession),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, asyncRows, 1)

	require.Equal(t, occurred, syncRows[0].CreatedAt.Time.UTC(),
		"the synchronous path stores the event time as sent")
	require.Equal(t, syncRows[0].CreatedAt.Time.UTC(), asyncRows[0].CreatedAt.Time.UTC(),
		"the async path must store the same instant the synchronous path does")
}

// The proxy observes the same assistant turn the agent's own hook stream
// reports, and assistant turns carry no correlation id to collapse them. The
// duplicate check moved to the handler with the rest of the database work; if
// it stops firing there, every natively captured session that also routes
// through LiteLLM shows each assistant turn twice.
func TestChatPersister_ProxiedAssistantTurnSuppressedForNativeSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "proxied-dupe-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	projectID := *authCtx.ProjectID

	// The session is already owned by a hook stream that reports its own
	// assistant turns.
	require.NoError(t, ti.service.cache.Set(t.Context(),
		sessionNativeHooksCacheKey(projectID.String(), sessionID), "claude", time.Hour))

	msg := newTestHookMessage(sessionID, authCtx.ActiveOrganizationID, chatID, projectID, "assistant", "proxied reply")
	msg.SetHookSource("litellm")
	msg.SetSource("litellm")

	notifier := &recordingNotifier{}
	persister := NewChatPersister(ti.service.logger, ti.conn, cache.NewRedisCacheAdapter(ti.redisClient), alwaysEnabledFeatures{}, notifier)

	stored, err := persister.Persist(t.Context(), msg)
	require.NoError(t, err)
	require.False(t, stored)

	messages, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Empty(t, messages, "a proxied assistant turn must not duplicate the native one")
	require.Empty(t, notifier.notified())
}

// The LiteLLM marker is the only durable trace that a natively captured session
// was routed through the proxy, because the proxied rows themselves get
// suppressed. The synchronous path set it with a defer on the request path; the
// handler now owns it.
func TestChatPersister_MarksChatLiteLLMProxied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "proxied-mark-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	projectID := *authCtx.ProjectID

	msg := newTestHookMessage(sessionID, authCtx.ActiveOrganizationID, chatID, projectID, "user", "proxied prompt")
	msg.SetHookSource("litellm")
	msg.SetSource("litellm")

	persister := NewChatPersister(ti.service.logger, ti.conn, cache.NewRedisCacheAdapter(ti.redisClient), alwaysEnabledFeatures{}, nil)
	_, err := persister.Persist(t.Context(), msg)
	require.NoError(t, err)

	chatRow, err := chatRepo.New(ti.conn).GetChat(t.Context(), chatRepo.GetChatParams{
		ID:        chatID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.True(t, chatRow.LitellmProxied, "a proxied row must flag the chat even when the row itself is kept")
}

// A message the handler can never write must nack so it reaches the DLQ. Acking
// it would drop a customer's transcript row with only a log line to show for it,
// which is exactly what the handler's own doc comment says must not happen.
func TestChatPersister_MalformedMessageErrorsSoItDeadLetters(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "malformed-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	projectID := *authCtx.ProjectID
	persister := NewChatPersister(ti.service.logger, ti.conn, cache.NewRedisCacheAdapter(ti.redisClient), alwaysEnabledFeatures{}, nil)
	handler := NewHookMessageHandler(ti.service.logger, persister)

	t.Run("no session", func(t *testing.T) {
		t.Parallel()
		msg := newTestHookMessage(sessionID, authCtx.ActiveOrganizationID, chatID, projectID, "user", "x")
		msg.SetSession(nil)
		require.Error(t, handler.Handle(t.Context(), msg, gcp.MessageMetadata{ID: "no-session"}))
	})

	t.Run("unparseable row id", func(t *testing.T) {
		t.Parallel()
		msg := newTestHookMessage(sessionID, authCtx.ActiveOrganizationID, chatID, projectID, "user", "x")
		msg.SetId("not-a-uuid")
		require.Error(t, handler.Handle(t.Context(), msg, gcp.MessageMetadata{ID: "bad-id"}))
	})

	t.Run("unparseable created_at", func(t *testing.T) {
		t.Parallel()
		msg := newTestHookMessage(sessionID, authCtx.ActiveOrganizationID, chatID, projectID, "user", "x")
		msg.SetCreatedAt("yesterday")
		require.Error(t, handler.Handle(t.Context(), msg, gcp.MessageMetadata{ID: "bad-time"}))
	})
}

// A proxied event that produces no transcript row still has to flag the chat.
// The marker is the only durable trace that a natively captured session was
// routed through LiteLLM, and moving the insert to the handler moved the mark
// with it — but the handler only ever sees events that published a row, so the
// filtered ones would otherwise lose the mark that the synchronous path sets.
func TestIngest_AsyncPersist_FilteredProxiedEventStillMarksChat(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "filtered-proxied-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)

	// The session's chat already exists, created by the native hook stream.
	prompt := "native prompt " + uuid.NewString()
	native := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	native.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	_, err := ti.service.IngestAuthenticated(t.Context(), authCtx, native)
	require.NoError(t, err)

	// A proxied event carrying nothing persistable, with the async path on.
	enableAsyncChatPersist(t, ti, authCtx)
	filtered := canonicalIngestPayload("litellm", "tool.requested", sessionID)
	_, err = ti.service.IngestAuthenticated(t.Context(), authCtx, filtered)
	require.NoError(t, err)

	require.Empty(t, ti.chatMessages.published(), "a filtered event has no row to publish")

	chatRow, err := chatRepo.New(ti.conn).GetChat(t.Context(), chatRepo.GetChatParams{
		ID:        chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.True(t, chatRow.LitellmProxied,
		"a proxied event must mark the chat even when it writes no transcript row")
}
