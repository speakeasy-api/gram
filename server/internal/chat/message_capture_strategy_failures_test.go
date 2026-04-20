package chat_test

import (
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// If StartOrResumeChat's upfront user-message persistence fails, the session
// carries the pending rows. CaptureMessage must flush them atomically
// alongside the assistant row so the chat history lands as a consistent unit
// — no orphan assistant with a stale parent hash.
func TestCaptureMessage_FlushesPendingRowsAtomically(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	// Seed the chat so UpsertChat has run — CaptureMessage expects the chats
	// row to exist (foreign key).
	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("Hello")),
		makeResponse("Hi there"),
	)

	before := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, before, 2)
	tipHash := before[len(before)-1].ContentHash

	// Simulate the "StartOrResumeChat persisted nothing this turn" state: a
	// poisoned session whose pendingRows is the new tail the client sent.
	pending := []or.ChatMessages{openrouter.CreateMessageUser("How are you?")}
	sess := chat.TestingNewPoisonedSession(
		chatID, projectID, "", "", "test-model",
		billing.ModelUsageSourcePlayground,
		0,       // generation
		tipHash, // parent hash = previous assistant tip
		pending,
	)

	req := makeRequest(chatID, projectID, orgID,
		openrouter.CreateMessageUser("Hello"),
		openrouter.CreateMessageAssistant("Hi there"),
		openrouter.CreateMessageUser("How are you?"),
	)

	require.NoError(t, s.CaptureMessage(ctx, sess, req, makeResponse("Doing well")))

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 4, "pending user row + assistant row written together")
	require.Equal(t, []string{"user", "assistant", "user", "assistant"}, roles(rows))
	for i, r := range rows {
		require.Equal(t, int32(0), r.Generation, "row %d stays on gen 0", i)
		require.NotEmpty(t, r.ContentHash, "row %d hashed", i)
	}

	// Follow-up turn: walk finds full history matching, no divergence, stays on
	// gen 0. Self-heals cleanly.
	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID,
			openrouter.CreateMessageUser("Hello"),
			openrouter.CreateMessageAssistant("Hi there"),
			openrouter.CreateMessageUser("How are you?"),
			openrouter.CreateMessageAssistant("Doing well"),
			openrouter.CreateMessageUser("cool"),
		),
		makeResponse("indeed"),
	)
	rows = listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 6)
	for _, r := range rows {
		require.Equal(t, int32(0), r.Generation, "still gen 0 — no divergence triggered by catch-up")
	}
}

// Mimics the "CaptureMessage persist failed" failure mode: user row persisted
// by StartOrResumeChat, but the assistant row never landed. On the next turn
// the client resends the full history including the just-streamed assistant
// content. The walk should append the missing assistant + next user message
// in the current generation — no divergence, no orphan.
func TestCaptureMessage_AssistantWriteFailure_SelfHealsOnNextTurn(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	// Round 1 persists the user message only (simulate assistant write failure
	// by skipping CaptureMessage entirely).
	req1 := makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("hi"))
	_, err := s.StartOrResumeChat(ctx, req1)
	require.NoError(t, err)

	mid := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, mid, 1, "only the user row persisted so far")
	require.Equal(t, "user", mid[0].Role)

	// Round 2: client resends full history + a new user message. The assistant
	// from round 1 appears in the request payload; the walk should match the
	// existing user row and append the assistant + new user + round-2 assistant.
	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID,
			openrouter.CreateMessageUser("hi"),
			openrouter.CreateMessageAssistant("hello"),
			openrouter.CreateMessageUser("follow up"),
		),
		makeResponse("response 2"),
	)

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 4)
	require.Equal(t, []string{"user", "assistant", "user", "assistant"}, roles(rows))
	for i, r := range rows {
		require.Equal(t, int32(0), r.Generation, "row %d stays on gen 0 — no divergence", i)
	}
}

// If both writes failed in a turn (full DB outage, for example), nothing from
// that turn landed. The next turn's walk sees whatever state existed before
// the outage and appends the newly-arrived messages without divergence.
func TestCaptureMessage_BothWritesFailed_SelfHealsOnNextTurn(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	// First successful turn.
	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("hi")),
		makeResponse("hello"),
	)
	require.Len(t, listAllMessages(t, ctx, conn, chatID, projectID), 2)

	// Turn 2 is effectively lost (we simulate by not calling StartOrResumeChat
	// or CaptureMessage). The client retries the whole sequence — walk should
	// match the 2 stored rows and append the new user + assistant as a clean
	// extension of gen 0.
	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID,
			openrouter.CreateMessageUser("hi"),
			openrouter.CreateMessageAssistant("hello"),
			openrouter.CreateMessageUser("retry"),
		),
		makeResponse("ok"),
	)

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 4)
	require.Equal(t, []string{"user", "assistant", "user", "assistant"}, roles(rows))
	for i, r := range rows {
		require.Equal(t, int32(0), r.Generation, "row %d stays on gen 0", i)
	}
}

// CaptureMessage called with a nil session falls back to a chain-tip lookup
// and still chains the assistant correctly. Covers callers that predate the
// session threading.
func TestCaptureMessage_NilSessionFallsBackToChainTip(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	// First turn via session (normal path).
	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("hi")),
		makeResponse("hello"),
	)

	// Round 2 user row persisted through StartOrResumeChat, but the caller does
	// not forward the session to CaptureMessage (simulates an older code path).
	req2 := makeRequest(chatID, projectID, orgID,
		openrouter.CreateMessageUser("hi"),
		openrouter.CreateMessageAssistant("hello"),
		openrouter.CreateMessageUser("again"),
	)
	_, err := s.StartOrResumeChat(ctx, req2)
	require.NoError(t, err)
	require.NoError(t, s.CaptureMessage(ctx, nil, req2, makeResponse("sure")))

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 4)
	require.Equal(t, []string{"user", "assistant", "user", "assistant"}, roles(rows))

	// The assistant row should chain off the previous row's hash.
	var chatRepo = repo.New(conn)
	tip, err := chatRepo.GetChatChainTip(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, rows[len(rows)-1].ContentHash, tip.ContentHash)
}
