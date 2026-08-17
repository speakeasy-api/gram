package hooks

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// enableAsyncChatPersist turns on the flag for the project the test's auth
// context carries. The distinct id is the project id — see asyncChatPersist.
func enableAsyncChatPersist(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext) {
	t.Helper()
	require.NotNil(t, authCtx.ProjectID)
	ti.flags.SetFlag(feature.FlagChatMessageAsyncPersist, authCtx.ProjectID.String(), true)
}

// recordingNotifier stands in for the chat message writer's observer fan-out.
type recordingNotifier struct {
	mu       sync.Mutex
	projects []uuid.UUID
}

func (n *recordingNotifier) NotifyStored(_ context.Context, projectID uuid.UUID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.projects = append(n.projects, projectID)
}

func (n *recordingNotifier) notified() []uuid.UUID {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.projects)
}

func newTestChatPersister(ti *testInstance) *ChatPersister {
	return newTestChatPersisterWithNotifier(ti, nil)
}

func newTestChatPersisterWithNotifier(ti *testInstance, notifier StoredNotifier) *ChatPersister {
	return NewChatPersister(
		ti.service.logger,
		ti.conn,
		cache.NewRedisCacheAdapter(ti.redisClient),
		alwaysEnabledFeatures{},
		notifier,
	)
}

// The point of the change: with the flag on, the hook request writes nothing to
// Postgres. If this ever regresses to a write the latency win is gone, and the
// regression is invisible without asserting on the absence of the row.
func TestIngest_AsyncPersist_PublishesInsteadOfWriting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	enableAsyncChatPersist(t, ti, authCtx)

	sessionID := "async-persist-" + uuid.NewString()
	prompt := "async prompt " + uuid.NewString()
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}

	_, err := ti.service.IngestAuthenticated(t.Context(), authCtx, payload)
	require.NoError(t, err)

	messages, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Empty(t, messages, "async path must not write on the request path")

	published := ti.chatMessages.published()
	require.Len(t, published, 1)

	msg := published[0]
	require.Equal(t, prompt, msg.GetContent())
	require.Equal(t, "user", msg.GetRole())
	require.Equal(t, sessionIDToUUID(sessionID).String(), msg.GetChatId())
	require.Equal(t, authCtx.ProjectID.String(), msg.GetProjectId())
	require.Equal(t, sessionID, msg.GetSession().GetSessionId())
	require.Equal(t, "claude", msg.GetAdapter())

	// The minted id is what makes the consumer's insert idempotent, so an empty
	// or unparseable one silently breaks redelivery safety.
	_, err = uuid.Parse(msg.GetId())
	require.NoError(t, err, "publisher must mint a parseable row id")
}

// The row the handler writes must be the row the synchronous path would have
// written. This is the test that catches the two paths drifting apart.
func TestChatPersister_ProducesSameRowAsSyncPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	prompt := "parity prompt " + uuid.NewString()

	// Synchronous path.
	syncSession := "parity-sync-" + uuid.NewString()
	syncPayload := canonicalIngestPayload("claude", "prompt.submitted", syncSession)
	syncPayload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	_, err := ti.service.IngestAuthenticated(t.Context(), authCtx, syncPayload)
	require.NoError(t, err)

	// Async path: publish, then run the handler over what was published.
	enableAsyncChatPersist(t, ti, authCtx)
	asyncSession := "parity-async-" + uuid.NewString()
	asyncPayload := canonicalIngestPayload("claude", "prompt.submitted", asyncSession)
	asyncPayload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	_, err = ti.service.IngestAuthenticated(t.Context(), authCtx, asyncPayload)
	require.NoError(t, err)

	published := ti.chatMessages.published()
	require.Len(t, published, 1)

	handler := NewHookMessageHandler(ti.service.logger, newTestChatPersister(ti))
	require.NoError(t, handler.Handle(t.Context(), published[0], gcp.MessageMetadata{ID: "parity-1"}))

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

	require.Equal(t, syncRows[0].Role, asyncRows[0].Role)
	require.Equal(t, syncRows[0].Content, asyncRows[0].Content)
	require.Equal(t, syncRows[0].Source, asyncRows[0].Source)
	require.Equal(t, syncRows[0].UserID, asyncRows[0].UserID)
	require.Equal(t, syncRows[0].ExternalUserID, asyncRows[0].ExternalUserID)
	require.Equal(t, syncRows[0].Model, asyncRows[0].Model)
	require.Equal(t, syncRows[0].Replayed, asyncRows[0].Replayed)
	require.Equal(t, syncRows[0].Generation, asyncRows[0].Generation)
}

// Pub/Sub delivers at least once, so the handler must be safe to run twice on
// the same message. Redelivery is the normal case after any nack, not an edge.
func TestChatPersister_RedeliveryWritesOneRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	enableAsyncChatPersist(t, ti, authCtx)

	sessionID := "redelivery-" + uuid.NewString()
	prompt := "redelivered prompt " + uuid.NewString()
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}

	_, err := ti.service.IngestAuthenticated(t.Context(), authCtx, payload)
	require.NoError(t, err)

	published := ti.chatMessages.published()
	require.Len(t, published, 1)

	handler := NewHookMessageHandler(ti.service.logger, newTestChatPersister(ti))
	for range 3 {
		require.NoError(t, handler.Handle(t.Context(), published[0], gcp.MessageMetadata{ID: "redelivered"}))
	}

	messages, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, messages, 1, "redelivery must not duplicate the transcript row")
	require.Equal(t, published[0].GetId(), messages[0].ID.String(),
		"the row must carry the publisher-minted id, not a database-generated one")
}

// A publish that fails must not fail the hook: the response carries a gating
// decision the agent is waiting on, and the transcript is not worth blocking it
// for. The failure is logged from the detached drain instead.
func TestIngest_AsyncPersist_PublishFailureDoesNotFailTheHook(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	enableAsyncChatPersist(t, ti, authCtx)
	ti.chatMessages.err = errors.New("broker unavailable")

	sessionID := "publish-failure-" + uuid.NewString()
	prompt := "prompt during outage " + uuid.NewString()
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}

	result, err := ti.service.IngestAuthenticated(t.Context(), authCtx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)
}

// The flag decides the path. With it off nothing reaches the topic, which is
// what makes the rollout reversible.
func TestIngest_FlagOff_WritesSynchronouslyAndPublishesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "flag-off-" + uuid.NewString()
	prompt := "synchronous prompt " + uuid.NewString()
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}

	_, err := ti.service.IngestAuthenticated(t.Context(), authCtx, payload)
	require.NoError(t, err)

	require.Empty(t, ti.chatMessages.published())

	messages, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, messages, 1)
}

// A prompt carrying an agent-prompt correlation id must take the correlated
// upsert, not a plain insert. The correlation id is what collapses the LiteLLM
// proxy's observation of a turn and the agent's own into one row; insert it
// plainly and the transcript shows the turn twice.
func TestChatPersister_CorrelatedPromptPromotesProxiedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "correlated-" + uuid.NewString()
	// The encoded form is what lets the proxy and the agent derive the SAME
	// correlation id for one turn; a bare turn id gives litellm none at all.
	turnID := agentTurnPrefix + "codex:" + uuid.NewString()
	prompt := "correlated prompt " + uuid.NewString()

	// The proxy observes the turn first and writes it synchronously.
	proxied := canonicalIngestPayload("litellm", "prompt.submitted", sessionID)
	proxied.Session.TurnID = &turnID
	proxied.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	_, err := ti.service.IngestAuthenticated(t.Context(), authCtx, proxied)
	require.NoError(t, err)

	rows, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "litellm", rows[0].Source.String)

	// The agent's own hook reports the same turn, now over the topic.
	enableAsyncChatPersist(t, ti, authCtx)
	native := canonicalIngestPayload("codex", "prompt.submitted", sessionID)
	native.Session.TurnID = &turnID
	native.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	_, err = ti.service.IngestAuthenticated(t.Context(), authCtx, native)
	require.NoError(t, err)

	published := ti.chatMessages.published()
	require.Len(t, published, 1)
	require.True(t,
		strings.HasPrefix(published[0].GetMessageId(), agentPromptCorrelationPrefix),
		"a correlated turn must carry its correlation id onto the topic")

	handler := NewHookMessageHandler(ti.service.logger, newTestChatPersister(ti))
	require.NoError(t, handler.Handle(t.Context(), published[0], gcp.MessageMetadata{ID: "correlated-1"}))

	rows, err = chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "the correlated turn must promote the proxied row, not add a second one")
	require.Equal(t, "codex", rows[0].Source.String, "the native source must win the promotion")

	// Redelivery of the same correlated message must stay a no-op.
	require.NoError(t, handler.Handle(t.Context(), published[0], gcp.MessageMetadata{ID: "correlated-1"}))
	rows, err = chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

// The wake, not the row, is what makes a message get analysed: the risk
// analysis coordinator sleeps until signalled and completes when no signal is
// pending, with no sweep behind it. A handler that writes the row and skips the
// wake produces transcripts that look perfectly correct and are never scanned —
// which is why this is asserted directly rather than left to the row tests.
func TestChatPersister_WakesCoordinatorsAfterDurableWrite(t *testing.T) {
	t.Parallel()

	// Both insert branches must wake. "claude" uses the native transcript
	// fallback, which routes an uncorrelated prompt through the lock-taking
	// insert; a custom adapter has neither that fallback nor a turn id to
	// correlate on, so it takes the plain insert. Covering only one leaves the
	// other free to lose its wake silently.
	for _, tt := range []struct {
		name    string
		adapter string
	}{
		{name: "uncorrelated prompt", adapter: "claude"},
		{name: "plain insert", adapter: "openclaw"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestHooksService(t)
			ti.service.productFeatures = alwaysEnabledFeatures{}
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			enableAsyncChatPersist(t, ti, authCtx)

			sessionID := "wake-" + uuid.NewString()
			prompt := "wake prompt " + uuid.NewString()
			payload := canonicalIngestPayload(tt.adapter, "prompt.submitted", sessionID)
			payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}

			_, err := ti.service.IngestAuthenticated(t.Context(), authCtx, payload)
			require.NoError(t, err)

			published := ti.chatMessages.published()
			require.Len(t, published, 1)

			notifier := &recordingNotifier{}
			handler := NewHookMessageHandler(ti.service.logger, newTestChatPersisterWithNotifier(ti, notifier))
			require.NoError(t, handler.Handle(t.Context(), published[0], gcp.MessageMetadata{ID: "wake-1"}))

			require.Equal(t, []uuid.UUID{*authCtx.ProjectID}, notifier.notified(),
				"a durable write must wake the coordinators that consume the row")

			// A redelivery stores nothing, so it must not wake anything either:
			// the wake is spent when the coordinator polls, and spending one on
			// a row already analysed is how a later real row goes unnoticed.
			require.NoError(t, handler.Handle(t.Context(), published[0], gcp.MessageMetadata{ID: "wake-1"}))
			require.Len(t, notifier.notified(), 1, "a redelivery that stored nothing must not wake")
		})
	}
}
