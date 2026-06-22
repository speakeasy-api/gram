package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// loadPayload returns a fully-populated LoadChatPayload (exhaustruct-friendly)
// for the latest generation, newest page. Tweak the returned struct per test.
func loadPayload(id string) *gen.LoadChatPayload {
	return &gen.LoadChatPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ID:                id,
		Generation:        nil,
		Limit:             50,
		BeforeSeq:         nil,
		AfterSeq:          nil,
		RiskOnly:          false,
	}
}

// seedNMessages inserts n minimal messages into a chat and returns their ids in
// insertion order (position 1..n by ascending seq).
func seedNMessages(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	r := repo.New(ti.conn)
	ids := make([]uuid.UUID, n)
	for i := range n {
		id, err := r.SeedChatMessage(ctx, repo.SeedChatMessageParams{
			ChatID:    chatID,
			ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		})
		require.NoError(t, err)
		ids[i] = id
	}
	return ids
}

// attachRiskTo creates a risk policy (once) and attaches an active finding to
// each of the given message ids.
func attachRiskTo(t *testing.T, ctx context.Context, ti *chatTestInstance, msgIDs ...uuid.UUID) {
	t.Helper()
	r := repo.New(ti.conn)
	policyID, err := r.SeedRiskPolicy(ctx, repo.SeedRiskPolicyParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	for _, msgID := range msgIDs {
		require.NoError(t, r.SeedRiskResult(ctx, repo.SeedRiskResultParams{
			ProjectID:      ti.projectID,
			OrganizationID: ti.orgID,
			RiskPolicyID:   policyID,
			ChatMessageID:  msgID,
			Found:          true,
		}))
	}
}

// allSeqs loads the whole chat ascending and returns the seq for each position
// (index i == position i+1). Also asserts ascending order and id alignment.
func allSeqs(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID, ids []uuid.UUID) []int64 {
	t.Helper()
	p := loadPayload(chatID.String())
	p.Limit = 200
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, res.Messages, len(ids))
	seqs := make([]int64, len(res.Messages))
	for i, m := range res.Messages {
		require.Equal(t, ids[i].String(), m.ID, "message %d out of order", i)
		seqs[i] = m.Seq
		if i > 0 {
			require.Greater(t, seqs[i], seqs[i-1], "seq must be strictly ascending")
		}
	}
	return seqs
}

// TestLoadChat_KeysetPagination walks the transcript newest-first with before_seq
// and forward with after_seq, asserting page contents and has_more flags.
func TestLoadChat_KeysetPagination(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "paged chat")
	ids := seedNMessages(t, ctx, ti, chatID, 25)
	seqs := allSeqs(t, ctx, ti, chatID, ids)

	// Page 1: newest 10 (positions 16..25), nothing newer, older remain.
	p := loadPayload(chatID.String())
	p.Limit = 10
	page1, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, page1.Messages, 10)
	require.Equal(t, seqs[15], page1.Messages[0].Seq)
	require.Equal(t, seqs[24], page1.Messages[9].Seq)
	require.True(t, page1.HasMoreBefore)
	require.False(t, page1.HasMoreAfter)
	require.Equal(t, 25, page1.NumMessages)

	// Page 2: before the oldest of page 1 → positions 6..15. Both directions have more.
	p = loadPayload(chatID.String())
	p.Limit = 10
	p.BeforeSeq = &page1.Messages[0].Seq
	page2, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, page2.Messages, 10)
	require.Equal(t, seqs[5], page2.Messages[0].Seq)
	require.Equal(t, seqs[14], page2.Messages[9].Seq)
	require.True(t, page2.HasMoreBefore)
	require.True(t, page2.HasMoreAfter)

	// Page 3: positions 1..5, nothing older left.
	p = loadPayload(chatID.String())
	p.Limit = 10
	p.BeforeSeq = &page2.Messages[0].Seq
	page3, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, page3.Messages, 5)
	require.Equal(t, seqs[0], page3.Messages[0].Seq)
	require.False(t, page3.HasMoreBefore)
	require.True(t, page3.HasMoreAfter)

	// Forward paging: after position 20 → positions 21..25, nothing newer.
	p = loadPayload(chatID.String())
	p.Limit = 10
	p.AfterSeq = &seqs[19]
	fwd, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, fwd.Messages, 5)
	require.Equal(t, seqs[20], fwd.Messages[0].Seq)
	require.Equal(t, seqs[24], fwd.Messages[4].Seq)
	require.False(t, fwd.HasMoreAfter)
	require.True(t, fwd.HasMoreBefore)
}

// TestLoadChat_RiskOnly_Window verifies a single finding yields one ±5 segment.
func TestLoadChat_RiskOnly_Window(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "risk chat")
	ids := seedNMessages(t, ctx, ti, chatID, 30)
	seqs := allSeqs(t, ctx, ti, chatID, ids)

	// Risk on position 13 (index 12) → window covers positions 8..18.
	attachRiskTo(t, ctx, ti, ids[12])

	p := loadPayload(chatID.String())
	p.RiskOnly = true
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, res.Messages, 11)
	require.Equal(t, seqs[7], res.Messages[0].Seq)
	require.Equal(t, seqs[17], res.Messages[10].Seq)

	require.Len(t, res.RiskSegments, 1)
	seg := res.RiskSegments[0]
	require.Equal(t, seqs[7], seg.FirstSeq)
	require.Equal(t, seqs[17], seg.LastSeq)
	require.True(t, seg.HasMoreBefore)
	require.True(t, seg.HasMoreAfter)
}

// TestLoadChat_RiskOnly_MultipleSegments verifies disjoint findings produce
// separate segments and an edge finding reports has_more_after=false.
func TestLoadChat_RiskOnly_MultipleSegments(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "risk chat")
	ids := seedNMessages(t, ctx, ti, chatID, 30)
	seqs := allSeqs(t, ctx, ti, chatID, ids)

	// Findings on positions 13 and 25 → windows 8..18 and 20..30 (disjoint).
	attachRiskTo(t, ctx, ti, ids[12], ids[24])

	p := loadPayload(chatID.String())
	p.RiskOnly = true
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, res.Messages, 22)
	require.Len(t, res.RiskSegments, 2)

	seg1, seg2 := res.RiskSegments[0], res.RiskSegments[1]
	require.Equal(t, seqs[7], seg1.FirstSeq)
	require.Equal(t, seqs[17], seg1.LastSeq)
	require.True(t, seg1.HasMoreBefore)
	require.True(t, seg1.HasMoreAfter)

	require.Equal(t, seqs[19], seg2.FirstSeq)
	require.Equal(t, seqs[29], seg2.LastSeq)
	require.True(t, seg2.HasMoreBefore)
	require.False(t, seg2.HasMoreAfter) // segment reaches the last message
}

// TestLoadChat_RiskOnly_OverlapMerges verifies nearby findings merge into one
// contiguous segment instead of two overlapping windows.
func TestLoadChat_RiskOnly_OverlapMerges(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "risk chat")
	ids := seedNMessages(t, ctx, ti, chatID, 30)
	seqs := allSeqs(t, ctx, ti, chatID, ids)

	// Findings on positions 13 and 16 → windows 8..18 and 11..21 overlap → 8..21.
	attachRiskTo(t, ctx, ti, ids[12], ids[15])

	p := loadPayload(chatID.String())
	p.RiskOnly = true
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, res.RiskSegments, 1)
	require.Equal(t, seqs[7], res.RiskSegments[0].FirstSeq)
	require.Equal(t, seqs[20], res.RiskSegments[0].LastSeq)
	require.Len(t, res.Messages, 14)
}

// TestLoadChat_RiskOnly_Empty verifies a chat with no findings returns nothing
// in risk-only mode.
func TestLoadChat_RiskOnly_Empty(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "clean chat")
	seedNMessages(t, ctx, ti, chatID, 10)

	p := loadPayload(chatID.String())
	p.RiskOnly = true
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Empty(t, res.Messages)
	require.Empty(t, res.RiskSegments)
	require.False(t, res.HasMoreBefore)
	require.False(t, res.HasMoreAfter)
}
