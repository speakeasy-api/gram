package chat_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/memorygate"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestChatMemoryLifecycleForOwner(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx, userID := memberSessionCtx(t, ti)
	id := seedChat(t, ctx, ti, userID, "", "Fix staging deploys")

	got, err := ti.service.GetMemory(ctx, &gen.GetMemoryPayload{ID: id.String()})
	require.NoError(t, err)
	require.Equal(t, "not_summarized", got.State)
	require.Nil(t, got.Summary)
	require.True(t, got.CanExclude)
	require.True(t, got.CanInclude)
	require.Empty(t, got.Memos)

	at := time.Date(2026, time.October, 28, 9, 0, 0, 0, time.UTC)
	require.NoError(t, testrepo.New(ti.conn).SetChatMemorySummaryFixture(ctx, testrepo.SetChatMemorySummaryFixtureParams{
		ID: id, Summary: conv.ToPGText("Rotated the staging deploy key."), SummaryGeneratedAt: conv.ToPGTimestamptz(at),
	}))
	_, err = testrepo.New(ti.conn).InsertMemoryMemoAtFixture(ctx, testrepo.InsertMemoryMemoAtFixtureParams{
		OrganizationID: ti.orgID, ProjectID: ti.projectID, ChatID: uuid.NullUUID{UUID: id, Valid: true},
		Content: "Staging key rotates every 30 days.", CreatedAt: conv.ToPGTimestamptz(at),
	})
	require.NoError(t, err)

	got, err = ti.service.GetMemory(ctx, &gen.GetMemoryPayload{ID: id.String()})
	require.NoError(t, err)
	require.Equal(t, "remembered", got.State)
	require.Equal(t, "Rotated the staging deploy key.", *got.Summary)
	require.Len(t, got.Memos, 1)
	require.Nil(t, got.DayDigestID)

	excluded, err := ti.service.SetMemoryExcluded(ctx, &gen.SetMemoryExcludedPayload{ID: id.String(), Excluded: true})
	require.NoError(t, err)
	require.Equal(t, "excluded", excluded.State)
	require.NotNil(t, excluded.ExcludedAt)

	list, err := ti.service.ListChats(ctx, &gen.ListChatsPayload{Limit: 10, SortBy: "last_message_timestamp", SortOrder: "desc"})
	require.NoError(t, err)
	require.Len(t, list.Chats, 1)
	require.Equal(t, "excluded", *list.Chats[0].MemoryState)

	included, err := ti.service.SetMemoryExcluded(ctx, &gen.SetMemoryExcludedPayload{ID: id.String(), Excluded: false})
	require.NoError(t, err)
	require.Equal(t, "remembered", included.State)
	require.Nil(t, included.ExcludedAt)
}

// A session reviewer may keep someone else's session out of memory, but
// putting it back shares it with the team again and needs chat:write.
func TestChatMemoryReviewerCanExcludeButNotInclude(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	seedCtx := initSessionCtx(t, ti)
	id := seedChat(t, seedCtx, ti, "someone-else", "", "Their session")

	reviewer := authztest.WithExactGrants(t, seedCtx, authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource))
	got, err := ti.service.SetMemoryExcluded(reviewer, &gen.SetMemoryExcludedPayload{ID: id.String(), Excluded: true})
	require.NoError(t, err)
	require.Equal(t, "excluded", got.State)
	require.False(t, got.CanInclude)

	_, err = ti.service.SetMemoryExcluded(reviewer, &gen.SetMemoryExcludedPayload{ID: id.String(), Excluded: false})
	requireOopsCode(t, err, oops.CodeForbidden)

	writer := authztest.WithExactGrants(t, seedCtx, authz.NewGrant(authz.ScopeChatWrite, authz.WildcardResource))
	got, err = ti.service.SetMemoryExcluded(writer, &gen.SetMemoryExcludedPayload{ID: id.String(), Excluded: false})
	require.NoError(t, err)
	require.Equal(t, "not_summarized", got.State)
	require.True(t, got.CanInclude)
}

func TestChatMemoryNotFoundWhenRolloutOff(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx, userID := memberSessionCtx(t, ti)
	id := seedChat(t, ctx, ti, userID, "", "Mine")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NoError(t, memorygate.Set(ctx, ti.conn, authCtx.ActiveOrganizationID, false))

	_, err := ti.service.GetMemory(ctx, &gen.GetMemoryPayload{ID: id.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}
