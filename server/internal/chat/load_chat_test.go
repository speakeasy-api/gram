package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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
		FromStart:         false,
		RiskOnly:          false,
		Query:             nil,
	}
}

// seedMessageContent inserts one user message with explicit content (generation
// 0) and returns its id, for exercising the text-search windowed view.
func seedMessageContent(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID, content string) uuid.UUID {
	t.Helper()
	r := repo.New(ti.conn)
	id, err := r.SeedChatMessageContent(ctx, repo.SeedChatMessageContentParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		Content:   content,
	})
	require.NoError(t, err)
	return id
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

// seedTypedMessage inserts one message with an explicit role, generation, and
// optional tool_calls JSON (nil = none), for exercising trace-entry
// classification in the entry-total queries.
func seedTypedMessage(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID, role string, generation int32, toolCalls []byte) {
	t.Helper()
	r := repo.New(ti.conn)
	require.NoError(t, r.CreateChatMessageWithToolCalls(ctx, repo.CreateChatMessageWithToolCallsParams{
		ChatID:     chatID,
		ProjectID:  uuid.NullUUID{UUID: ti.projectID, Valid: true},
		Role:       role,
		Content:    "test message",
		ToolCalls:  toolCalls,
		ToolCallID: pgtype.Text{String: "", Valid: false},
		Generation: generation,
	}))
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

// TestLoadChat_FromStart loads the oldest page (start of the thread) ascending,
// reports nothing older, and pages forward from there with after_seq.
func TestLoadChat_FromStart(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "from-start chat")
	ids := seedNMessages(t, ctx, ti, chatID, 25)
	seqs := allSeqs(t, ctx, ti, chatID, ids)

	// First page: oldest 10 (positions 1..10), nothing older, newer remain.
	p := loadPayload(chatID.String())
	p.FromStart = true
	p.Limit = 10
	page1, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, page1.Messages, 10)
	require.Equal(t, seqs[0], page1.Messages[0].Seq)
	require.Equal(t, seqs[9], page1.Messages[9].Seq)
	require.False(t, page1.HasMoreBefore, "start of thread has nothing older")
	require.True(t, page1.HasMoreAfter)
	require.Equal(t, 25, page1.NumMessages)

	// Scroll forward from the from-start page → positions 11..20.
	p = loadPayload(chatID.String())
	p.Limit = 10
	p.AfterSeq = &page1.Messages[9].Seq
	page2, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, page2.Messages, 10)
	require.Equal(t, seqs[10], page2.Messages[0].Seq)
	require.True(t, page2.HasMoreBefore)
	require.True(t, page2.HasMoreAfter)

	// from_start with a limit covering everything: whole thread, both edges done.
	p = loadPayload(chatID.String())
	p.FromStart = true
	p.Limit = 200
	all, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, all.Messages, 25)
	require.Equal(t, seqs[0], all.Messages[0].Seq)
	require.False(t, all.HasMoreBefore)
	require.False(t, all.HasMoreAfter)

	// before_seq takes precedence over from_start (documented): with both set, we
	// page backward from the cursor (newest page below it), so older may remain.
	p = loadPayload(chatID.String())
	p.FromStart = true
	p.Limit = 10
	p.BeforeSeq = &seqs[24]
	pref, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, pref.Messages, 10)
	require.Equal(t, seqs[14], pref.Messages[0].Seq, "before_seq path returns the page below the cursor, not the thread start")
	require.True(t, pref.HasMoreBefore)
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

// TestLoadChat_Search_Window seeds one matching message among context and
// verifies the query view returns a single ±5 window, the right match seqs, and
// case-insensitive matching.
func TestLoadChat_Search_Window(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "search chat")
	// 30 messages; only position 13 (index 12) carries the needle.
	var needleSeq int64
	for i := 1; i <= 30; i++ {
		content := "ordinary message"
		if i == 13 {
			content = "this line has a NEEDLE in it"
		}
		seedMessageContent(t, ctx, ti, chatID, content)
	}

	// Resolve the needle's seq by loading everything ascending.
	allP := loadPayload(chatID.String())
	allP.Limit = 200
	all, err := ti.service.LoadChat(ctx, allP)
	require.NoError(t, err)
	require.Len(t, all.Messages, 30)
	needleSeq = all.Messages[12].Seq

	// Query is case-insensitive (ILIKE): "needle" matches "NEEDLE".
	q := "needle"
	p := loadPayload(chatID.String())
	p.Query = &q
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)

	// Window covers positions 8..18 (11 messages).
	require.Len(t, res.Messages, 11)
	require.Equal(t, all.Messages[7].Seq, res.Messages[0].Seq)
	require.Equal(t, all.Messages[17].Seq, res.Messages[10].Seq)

	require.Len(t, res.MatchSegments, 1)
	seg := res.MatchSegments[0]
	require.Equal(t, all.Messages[7].Seq, seg.FirstSeq)
	require.Equal(t, all.Messages[17].Seq, seg.LastSeq)
	require.True(t, seg.HasMoreBefore)
	require.True(t, seg.HasMoreAfter)

	// Only the needle is a match; context rows are not listed.
	require.Equal(t, []int64{needleSeq}, res.MatchSeqs)
	// Risk segments are absent in query mode.
	require.Empty(t, res.RiskSegments)
}

// TestLoadChat_Search_Empty verifies a query with no matches returns nothing.
func TestLoadChat_Search_Empty(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "search chat")
	for range 10 {
		seedMessageContent(t, ctx, ti, chatID, "ordinary message")
	}

	q := "zzz-no-such-token"
	p := loadPayload(chatID.String())
	p.Query = &q
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Empty(t, res.Messages)
	require.Empty(t, res.MatchSegments)
	require.Empty(t, res.MatchSeqs)
	require.False(t, res.HasMoreBefore)
	require.False(t, res.HasMoreAfter)
}

// TestLoadChat_Search_MultipleSegments verifies disjoint matches produce
// separate segments, each with its own cursors.
func TestLoadChat_Search_MultipleSegments(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "search chat")
	// Matches on positions 13 and 25 → windows 8..18 and 20..30 (disjoint).
	for i := 1; i <= 30; i++ {
		content := "ordinary message"
		if i == 13 || i == 25 {
			content = "contains needle here"
		}
		seedMessageContent(t, ctx, ti, chatID, content)
	}

	q := "needle"
	p := loadPayload(chatID.String())
	p.Query = &q
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, res.MatchSegments, 2)
	require.Len(t, res.MatchSeqs, 2)
	require.False(t, res.MatchSegments[1].HasMoreAfter, "second window reaches the last message")
}

// TestLoadChat_Search_RiskOnlyMutuallyExclusive verifies the two windowed modes
// can't be combined.
func TestLoadChat_Search_RiskOnlyMutuallyExclusive(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "search chat")
	seedMessageContent(t, ctx, ti, chatID, "ordinary message")

	q := "ordinary"
	p := loadPayload(chatID.String())
	p.Query = &q
	p.RiskOnly = true
	_, err := ti.service.LoadChat(ctx, p)
	require.Error(t, err)
}

// TestLoadChat_EntryTotals verifies the whole-generation trace-entry totals are
// independent of the paginated page and classify each message into exactly one
// bucket: a message carrying a non-empty tool_calls array is a tool call
// regardless of role, an empty array stays an assistant message, and system
// rows are excluded from every bucket (and from the total).
func TestLoadChat_EntryTotals(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "totals chat")

	toolCalls := []byte(`[{"id":"call_1","type":"function","function":{"name":"x"}}]`)
	for range 3 {
		seedTypedMessage(t, ctx, ti, chatID, "user", 0, nil)
	}
	seedTypedMessage(t, ctx, ti, chatID, "assistant", 0, nil)
	seedTypedMessage(t, ctx, ti, chatID, "assistant", 0, []byte("[]")) // empty array → assistant
	for range 4 {
		seedTypedMessage(t, ctx, ti, chatID, "assistant", 0, toolCalls) // tool_calls → tool_call
	}
	seedTypedMessage(t, ctx, ti, chatID, "tool", 0, nil)
	seedTypedMessage(t, ctx, ti, chatID, "system", 0, nil) // excluded from totals

	// Tiny page so the loaded message slice can't accidentally satisfy the
	// assertions — totals must come from the whole-generation query.
	p := loadPayload(chatID.String())
	p.Limit = 2
	res, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.Len(t, res.Messages, 2)

	require.NotNil(t, res.Totals)
	require.Equal(t, int64(3), res.Totals.UserMessages)
	require.Equal(t, int64(2), res.Totals.AssistantMessages)
	require.Equal(t, int64(4), res.Totals.ToolCalls)
	require.Equal(t, int64(1), res.Totals.ToolResults)
	require.Equal(t, int64(0), res.Totals.RiskOnly)
	require.Equal(t, int64(10), res.Totals.Total, "total sums the four entry types and excludes the system row")
	require.Equal(t, 11, res.NumMessages, "chat-wide message count still includes the system row")
}

// TestLoadChat_Totals_GenerationScoped verifies totals (entry types and risk)
// describe the requested generation, not the whole chat, so a compacted chat's
// latest generation doesn't inherit an older generation's findings.
func TestLoadChat_Totals_GenerationScoped(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "u", "", "gen totals chat")

	// Generation 0: 5 user messages, two carrying an active risk finding.
	gen0 := seedNMessages(t, ctx, ti, chatID, 5) // SeedChatMessage → role=user, generation=0
	attachRiskTo(t, ctx, ti, gen0[0], gen0[1])

	// Generation 1 (the latest): 3 user messages, no findings.
	for range 3 {
		seedTypedMessage(t, ctx, ti, chatID, "user", 1, nil)
	}

	// Default load resolves the latest generation.
	latest, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.NotNil(t, latest.Totals)
	require.Equal(t, 1, latest.Generation)
	require.Equal(t, int64(3), latest.Totals.Total)
	require.Equal(t, int64(3), latest.Totals.UserMessages)
	require.Equal(t, int64(0), latest.Totals.RiskOnly, "findings live on gen 0, not the latest generation")

	// Explicit older generation reports its own totals, findings included.
	gen0Num := 0
	p := loadPayload(chatID.String())
	p.Generation = &gen0Num
	older, err := ti.service.LoadChat(ctx, p)
	require.NoError(t, err)
	require.NotNil(t, older.Totals)
	require.Equal(t, int64(5), older.Totals.Total)
	require.Equal(t, int64(5), older.Totals.UserMessages)
	require.Equal(t, int64(2), older.Totals.RiskOnly)
}
