package hooks

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// Edge cases for the DNO-536 occurred_at persistence contract, beyond the
// happy-path ordering pinned by TestIngest_ReplayedMessageSortsByOccurredAt.

// TestIngest_MissingOccurredAtUsesArrivalTime: an envelope without an
// occurred_at must persist at arrival time, never at the zero time (which
// would sort it before every row in the chat forever).
func TestIngest_MissingOccurredAtUsesArrivalTime(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "occurred-at-missing-" + uuid.NewString()
	prompt := "no occurred_at at all"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	require.Nil(t, payload.Event.OccurredAt, "fixture sanity: no occurred_at supplied")
	res, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.WithinDuration(t, time.Now(), msgs[0].CreatedAt.Time, 30*time.Second)
}

// TestIngest_MalformedOccurredAtUsesArrivalTime: a garbage occurred_at string
// must not fail ingest and must not corrupt ordering — it falls back to
// arrival time.
func TestIngest_MalformedOccurredAtUsesArrivalTime(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "occurred-at-malformed-" + uuid.NewString()
	prompt := "garbage occurred_at"
	garbage := "not-a-timestamp"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	payload.Event.OccurredAt = &garbage
	res, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.WithinDuration(t, time.Now(), msgs[0].CreatedAt.Time, 30*time.Second)
}

// TestIngest_EqualOccurredAtBreaksTiesByArrival: two events sharing one
// occurred_at (coarse client clocks emit these) must read back in arrival
// order — the seq tiebreak, not an arbitrary shuffle.
func TestIngest_EqualOccurredAtBreaksTiesByArrival(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "occurred-at-tie-" + uuid.NewString()
	at := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339Nano)
	for _, text := range []string{"tie first", "tie second", "tie third"} {
		payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
		payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &text}}
		payload.Event.OccurredAt = &at
		res, err := ti.service.Ingest(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, "allow", res.Decision)
	}

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	require.Equal(t, "tie first", msgs[0].Content)
	require.Equal(t, "tie second", msgs[1].Content)
	require.Equal(t, "tie third", msgs[2].Content)
}

// TestIngest_DuplicateDeliveryKeepsFirstRowAndTimestamp: a redelivery under
// the same Idempotency-Key — the spool replaying an event the server already
// stored — must not mint a second row or rewrite the first one's timestamp,
// even when the replay carries a different occurred_at.
func TestIngest_DuplicateDeliveryKeepsFirstRowAndTimestamp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "occurred-at-dup-" + uuid.NewString()
	idemKey := uuid.NewString()
	prompt := "delivered twice"
	firstAt := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339Nano)
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	payload.Event.OccurredAt = &firstAt
	payload.IdempotencyKey = &idemKey
	res, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	// The replayed duplicate claims an older occurred_at; the original row
	// must win.
	replayed := true
	olderAt := time.Now().UTC().Add(-90 * time.Minute).Format(time.RFC3339Nano)
	dup := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	dup.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	dup.Event.OccurredAt = &olderAt
	dup.IdempotencyKey = &idemKey
	dup.Replayed = &replayed
	res, err = ti.service.Ingest(ctx, dup)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1, "a duplicate delivery must not mint a second row")
	wantFirstAt, err := time.Parse(time.RFC3339Nano, firstAt)
	require.NoError(t, err)
	require.WithinDuration(t, wantFirstAt, msgs[0].CreatedAt.Time, time.Second, "the original delivery's timestamp must survive the duplicate")
}
