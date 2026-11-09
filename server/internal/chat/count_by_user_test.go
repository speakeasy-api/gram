package chat_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCountByUser_NoAuthContext(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)

	_, err := ti.service.CountByUser(t.Context(), &gen.CountByUserPayload{Limit: 10})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// Each user's count must equal what chat.list returns when filtered to that
// user, and the total must equal the unfiltered chat.list total: the dashboard
// card drills into that list, so the numbers have to line up.
func TestCountByUser_MatchesListChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedChat(t, ctx, ti, authCtx.UserID, "", "own 1")
	seedChat(t, ctx, ti, authCtx.UserID, "", "own 2")
	seedChat(t, ctx, ti, authCtx.UserID, "", "own 3")
	seedChat(t, ctx, ti, "user-bbb", "", "other 1")
	seedChat(t, ctx, ti, "", "ext-ccc", "external")

	result, err := ti.service.CountByUser(ctx, &gen.CountByUserPayload{Limit: 10})
	require.NoError(t, err)

	all, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Equal(t, all.Total, result.Total)
	require.Equal(t, 5, result.Total)

	require.Len(t, result.Users, 3)
	require.Equal(t, authCtx.UserID, result.Users[0].UserID)
	require.Equal(t, 3, result.Users[0].Count)
	require.NotEmpty(t, result.Users[0].Email)

	for _, u := range result.Users {
		if u.UserID == "" {
			continue
		}
		payload := defaultPayload()
		payload.UserID = &u.UserID
		filtered, err := ti.service.ListChats(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, filtered.Total, u.Count, "user %s", u.UserID)
	}
}

func TestCountByUser_LimitTruncatesUsersNotTotal(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))

	seedChat(t, ctx, ti, "user-aaa", "", "a1")
	seedChat(t, ctx, ti, "user-aaa", "", "a2")
	seedChat(t, ctx, ti, "user-bbb", "", "b1")

	result, err := ti.service.CountByUser(ctx, &gen.CountByUserPayload{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, 3, result.Total)
	require.Len(t, result.Users, 1)
	require.Equal(t, "user-aaa", result.Users[0].UserID)
	require.Equal(t, 2, result.Users[0].Count)
}

func TestCountByUser_RegularUser_CountsOnlyOwnChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithExactGrants(t, initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedChat(t, ctx, ti, authCtx.UserID, "", "own chat")
	seedChat(t, ctx, ti, "other-user-id", "", "other users chat")

	result, err := ti.service.CountByUser(ctx, &gen.CountByUserPayload{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Users, 1)
	require.Equal(t, authCtx.UserID, result.Users[0].UserID)
}

func TestCountByUser_DateRangeMatchesListChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))

	now := time.Now().UTC()
	seedChatAtTime(t, ctx, ti, "ext-old", now.Add(-30*24*time.Hour))
	seedChatAtTime(t, ctx, ti, "ext-new", now.Add(-time.Hour))

	from := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	result, err := ti.service.CountByUser(ctx, &gen.CountByUserPayload{From: &from, To: &to, Limit: 10})
	require.NoError(t, err)

	payload := defaultPayload()
	payload.From = &from
	payload.To = &to
	listed, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)

	require.Equal(t, 1, result.Total)
	require.Equal(t, listed.Total, result.Total)
}

func TestCountByUser_InvalidFrom(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	bad := "not-a-time"
	_, err := ti.service.CountByUser(ctx, &gen.CountByUserPayload{From: &bad, Limit: 10})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
