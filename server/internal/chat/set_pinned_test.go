package chat_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func chatOverviewIDs(res *gen.ListChatsResult) []string {
	ids := make([]string, 0, len(res.Chats))
	for _, c := range res.Chats {
		ids = append(ids, c.ID)
	}
	return ids
}

// Pinning sets pinned_at; unpinning clears it.
func TestService_SetPinned_PinAndUnpin(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "", "ext-user", "Pin Me")

	require.NoError(t, ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: chatID.String(), Pinned: true}))
	chat, err := repo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.True(t, chat.PinnedAt.Valid)

	require.NoError(t, ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: chatID.String(), Pinned: false}))
	chat, err = repo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.False(t, chat.PinnedAt.Valid)
}

// Re-pinning an already-pinned chat preserves the original pin time (COALESCE).
func TestService_SetPinned_RepinPreservesTime(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "", "ext-user", "Pin Me")
	require.NoError(t, ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: chatID.String(), Pinned: true}))
	first, err := repo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)

	require.NoError(t, ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: chatID.String(), Pinned: true}))
	second, err := repo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, first.PinnedAt.Time, second.PinnedAt.Time)
}

func TestService_SetPinned_InvalidID(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	err := ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: "not-a-uuid", Pinned: true})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// A chat the caller can't see (here: absent) is rejected rather than silently
// no-op'd, so the access check can't be probed by UUID.
func TestService_SetPinned_MissingChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	err := ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: uuid.NewString(), Pinned: true})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// The listChats pinned filter splits chats into pinned and unpinned sets, which
// is how the /chat page renders the Pinned section above Recents.
func TestService_ListChats_PinnedFilter(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	// Org admin sees all project chats (no per-user scoping), so the filter under
	// test is what splits the result rather than the caller's identity.
	ctx := grantOrgAdmin(t, initSessionCtx(t, ti))

	pinnedID := seedChat(t, ctx, ti, "", "ext-user", "Pinned Chat")
	unpinnedID := seedChat(t, ctx, ti, "", "ext-user", "Unpinned Chat")
	require.NoError(t, ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: pinnedID.String(), Pinned: true}))

	pinned := "true"
	p := defaultPayload()
	p.Pinned = &pinned
	res, err := ti.service.ListChats(ctx, p)
	require.NoError(t, err)
	ids := chatOverviewIDs(res)
	require.Contains(t, ids, pinnedID.String())
	require.NotContains(t, ids, unpinnedID.String())

	unpinned := "false"
	p2 := defaultPayload()
	p2.Pinned = &unpinned
	res2, err := ti.service.ListChats(ctx, p2)
	require.NoError(t, err)
	ids2 := chatOverviewIDs(res2)
	require.Contains(t, ids2, unpinnedID.String())
	require.NotContains(t, ids2, pinnedID.String())
}
