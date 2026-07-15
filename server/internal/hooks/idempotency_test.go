package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	cache "github.com/speakeasy-api/gram/server/internal/cache"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// TestClaudeHookIdempotency_DeliveredTwiceStoredOnce asserts that re-delivering
// the same hook (same idempotency token, as a retry would) persists exactly one
// chat_messages row. This is the core resilience guarantee: retrying a transient
// connection reset must not double-store the event.
func TestClaudeHookIdempotency_DeliveredTwiceStoredOnce(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	prompt := "deliver me exactly once"
	const userID = "idempotency-user"
	const userEmail = "idempotency@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)

	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     userEmail,
		UserID:        userID,
		ExternalOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}, time.Hour))

	token := uuid.NewString()
	payload := func() *gen.ClaudePayload {
		p := prompt
		s := sessionID
		k := token
		return &gen.ClaudePayload{
			HookEventName:  "UserPromptSubmit",
			SessionID:      &s,
			Prompt:         &p,
			IdempotencyKey: &k,
		}
	}

	// First delivery persists.
	_, err := ti.service.Claude(ctx, payload())
	require.NoError(t, err)

	var msgs []chatRepo.ChatMessage
	require.Eventually(t, func() bool {
		var err error
		msgs, err = chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(msgs) == 1
	}, 2*time.Second, 25*time.Millisecond, "first delivery should persist exactly one row")

	// Second delivery with the same token is a no-op.
	_, err = ti.service.Claude(ctx, payload())
	require.NoError(t, err)

	// Give any (erroneously spawned) persistence goroutine time to run, then
	// assert the count is still one — the duplicate was dropped.
	require.Never(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) != 1
	}, 500*time.Millisecond, 50*time.Millisecond, "redelivery with the same token must not add a second row")
}

// TestClaimHookIdempotency_Guard exercises the set-if-absent guard directly:
// the first claim on a token wins, repeats lose, and an empty token always
// wins (older plugins / OTEL-only flows carry no token to dedupe on).
func TestClaimHookIdempotency_Guard(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	token := uuid.NewString()
	assert.True(t, ti.service.claimHookIdempotency(ctx, token, false), "first claim wins")
	assert.False(t, ti.service.claimHookIdempotency(ctx, token, false), "repeat claim loses")
	assert.False(t, ti.service.claimHookIdempotency(ctx, "  "+token+"  ", false), "whitespace-padded repeat still loses")

	assert.True(t, ti.service.claimHookIdempotency(ctx, "", false), "empty token always proceeds")
	assert.True(t, ti.service.claimHookIdempotency(ctx, "   ", false), "blank token always proceeds")
}

// TestHookDuplicateContextFlag verifies the flag that gates the block-path
// write side-effects (block-reason telemetry, shadow-MCP findings) on a
// redelivery: untagged contexts are live, tagged ones are duplicates.
func TestHookDuplicateContextFlag(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)

	assert.False(t, ti.service.isHookDuplicate(context.Background()), "untagged context is a live delivery")
	assert.True(t, ti.service.isHookDuplicate(withHookDuplicate(context.Background())), "tagged context is a duplicate")
}

// ttlRecordingCache records the TTL each Add claim was made with, so the
// replayed-vs-live window selection is pinned without inspecting Redis.
type ttlRecordingCache struct {
	cache.Cache
	ttls []time.Duration
}

func (c *ttlRecordingCache) Add(_ context.Context, _ string, ttl time.Duration) (bool, error) {
	c.ttls = append(c.ttls, ttl)
	return true, nil
}

// TestClaimHookIdempotency_ReplayedClaimsLongWindow pins the DNO-498 replay
// contract: a live delivery's claim only needs to outlive a retry burst, but
// a spool replay (X-Gram-Replayed) can be redelivered hours later by a
// competing drain trigger — its claim must hold for the long window.
func TestClaimHookIdempotency_ReplayedClaimsLongWindow(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)

	rec := &ttlRecordingCache{Cache: ti.service.cache, ttls: nil}
	svc := *ti.service
	svc.cache = rec

	require.True(t, svc.claimHookIdempotency(context.Background(), uuid.NewString(), false))
	require.True(t, svc.claimHookIdempotency(context.Background(), uuid.NewString(), true))

	require.Len(t, rec.ttls, 2)
	assert.Equal(t, hookIdempotencyTTL, rec.ttls[0], "a live delivery claims the retry-burst window")
	assert.Equal(t, hookReplayIdempotencyTTL, rec.ttls[1], "a replayed delivery must claim the drain-cycle window")
	assert.Greater(t, hookReplayIdempotencyTTL, hookIdempotencyTTL)
}
