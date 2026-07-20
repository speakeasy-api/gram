package chat_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// These tests pin the DNO-536 transcript-order contract: readers and keyset
// pages order by (created_at, seq), so a row persisted late with a backdated
// created_at (offline-spool replay) interleaves at its occurrence position
// instead of appending at its arrival position.

// seedMessageAt inserts one message with an explicit created_at, returning
// its id.
func seedMessageAt(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID, at time.Time) uuid.UUID {
	t.Helper()
	id, err := repo.New(ti.conn).SeedChatMessage(ctx, repo.SeedChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		CreatedAt: conv.ToPGTimestamptz(at),
	})
	require.NoError(t, err)
	return id
}

// TestLoadChat_BackdatedRowInterleavesInTranscriptOrder: a row INSERTED last
// (highest seq) with the OLDEST created_at — the spool-replay shape — must
// read back first, and keyset pages must agree with that display order.
func TestLoadChat_BackdatedRowInterleavesInTranscriptOrder(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "backdated interleave")
	base := time.Now().UTC().Add(-time.Hour)
	midID := seedMessageAt(t, ctx, ti, chatID, base.Add(10*time.Minute))
	newID := seedMessageAt(t, ctx, ti, chatID, base.Add(20*time.Minute))
	// Arrives last (highest seq) but occurred first: the DNO-536 repro.
	oldID := seedMessageAt(t, ctx, ti, chatID, base)

	p := loadPayload(chatID.String())
	p.Limit = 10
	full, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, full.Messages, 3)
	require.Equal(t, oldID.String(), full.Messages[0].ID, "the backdated row must sort first despite arriving last")
	require.Equal(t, midID.String(), full.Messages[1].ID)
	require.Equal(t, newID.String(), full.Messages[2].ID)

	// Newest page of 2 in transcript order is [mid, new] — NOT the two
	// highest seqs (which would be [new, old]).
	p = loadPayload(chatID.String())
	p.Limit = 2
	page1, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, page1.Messages, 2)
	require.Equal(t, midID.String(), page1.Messages[0].ID)
	require.Equal(t, newID.String(), page1.Messages[1].ID)
	require.True(t, page1.HasMoreBefore)

	// Paging before the mid row lands on the backdated row even though its
	// seq is the highest in the chat.
	p = loadPayload(chatID.String())
	p.Limit = 2
	p.BeforeSeq = &page1.Messages[0].Seq
	page2, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, page2.Messages, 1)
	require.Equal(t, oldID.String(), page2.Messages[0].ID)
	require.False(t, page2.HasMoreBefore)

	// And paging after the backdated row returns the rest of the transcript
	// exactly once — no duplicates, no gaps.
	p = loadPayload(chatID.String())
	p.Limit = 10
	p.AfterSeq = &page2.Messages[0].Seq
	rest, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, rest.Messages, 2)
	require.Equal(t, midID.String(), rest.Messages[0].ID)
	require.Equal(t, newID.String(), rest.Messages[1].ID)
}

// TestLoadChat_TiedCreatedAtPagesBySeqWithoutDuplicates: batch-stamped rows
// share one created_at, so the seq tiebreak is what keeps pages disjoint and
// complete. A cursor anchored mid-tie must not skip or repeat rows.
func TestLoadChat_TiedCreatedAtPagesBySeqWithoutDuplicates(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "tied timestamps")
	at := time.Now().UTC().Add(-30 * time.Minute)
	ids := make([]uuid.UUID, 6)
	for i := range ids {
		ids[i] = seedMessageAt(t, ctx, ti, chatID, at)
	}

	seen := make(map[string]int, len(ids))
	p := loadPayload(chatID.String())
	p.Limit = 2
	page, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	for {
		require.NotEmpty(t, page.Messages, "a page in a tied-timestamp chat must never be empty")
		for _, m := range page.Messages {
			seen[m.ID]++
		}
		if !page.HasMoreBefore {
			break
		}
		p = loadPayload(chatID.String())
		p.Limit = 2
		p.BeforeSeq = &page.Messages[0].Seq
		page, err = ti.service.LoadChat(ctx, p)
		require.NoError(t, err)
	}
	require.Len(t, seen, len(ids), "every row must be paged exactly once")
	for id, n := range seen {
		require.Equal(t, 1, n, "row %s paged %d times", id, n)
	}
}

// TestLoadChat_MissingAnchorFallsBackToSeqComparison: a cursor whose seq
// resolves to no row in the chat must not dead-end pagination with a silent
// empty page — the query falls back to the plain seq comparison the
// pre-tuple cursor used.
func TestLoadChat_MissingAnchorFallsBackToSeqComparison(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "missing anchor")
	ids := seedNMessages(t, ctx, ti, chatID, 5)
	seqs := allSeqs(t, ctx, ti, chatID, ids)

	// A before-cursor far above any real seq behaves like "newest page".
	ghostHigh := seqs[len(seqs)-1] + 1_000_000
	p := loadPayload(chatID.String())
	p.Limit = 3
	p.BeforeSeq = &ghostHigh
	page, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, page.Messages, 3, "an unresolvable cursor must still return a page, not dead-end")
	require.Equal(t, seqs[2], page.Messages[0].Seq)
	require.Equal(t, seqs[4], page.Messages[2].Seq)

	// An after-cursor below any real seq behaves like "oldest page".
	require.Positive(t, seqs[0], "sanity: seqs are positive so the ghost cursor sits below them all")
	ghostLow := int64(0)
	p = loadPayload(chatID.String())
	p.Limit = 3
	p.AfterSeq = &ghostLow
	page, err = ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, page.Messages, 3)
	require.Equal(t, seqs[0], page.Messages[0].Seq)
	require.Equal(t, seqs[2], page.Messages[2].Seq)
}

// TestChatMessageWriter_BatchKeepsInsertionOrder: rows written in one batch
// share one stamp, so their relative order under (created_at, seq) is their
// insertion order — the pre-DNO-536 semantics for playground writers whose
// rows may be constructed out of wall-clock order.
func TestChatMessageWriter_BatchKeepsInsertionOrder(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "batch order")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	roles := []string{"user", "assistant", "user", "assistant"}
	params := make([]repo.CreateChatMessageParams, 0, len(roles))
	for _, role := range roles {
		params = append(params, repo.CreateChatMessageParams{
			ChatID:    chatID,
			ProjectID: ti.projectID,
			Role:      role,
			Content:   "batch order " + role,
		})
	}
	_, err := writer.Write(ctx, ti.projectID, params)
	require.NoError(t, err)

	p := loadPayload(chatID.String())
	p.Limit = 10
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, res.Messages, len(roles))
	got := make([]string, len(res.Messages))
	for i, m := range res.Messages {
		got[i] = m.Role
	}
	require.Equal(t, roles, got)
	// All four rows carry the same batch stamp; seq is what ordered them.
	first := res.Messages[0].CreatedAt
	for _, m := range res.Messages[1:] {
		require.Equal(t, first, m.CreatedAt, "batch rows must share one created_at stamp")
	}
}
