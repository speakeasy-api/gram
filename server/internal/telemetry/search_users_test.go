package telemetry_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestSearchUsers_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    50,
		Sort:     "desc",
	})

	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "logs are not enabled")
}

func TestSearchUsers_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    50,
		Sort:     "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Enabled)
	require.Empty(t, result.Users)
	require.Nil(t, result.NextCursor)
}

func TestSearchUsers_GroupByInternalUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	user1 := "user-a-" + uuid.New().String()
	user2 := "user-b-" + uuid.New().String()
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()

	// User 1: 2 completions in 1 chat + 1 successful tool call
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, 100, 50, 150, 1.5, "stop", "gpt-4", "openai", user1, "")
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID1, 200, 100, 300, 2.0, "tool_calls", "gpt-4", "openai", user1, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), "tools:http:petstore:listPets", 200, 0.5, user1, "")

	// User 2: 1 completion in 1 chat + 1 failed tool call
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), chatID2, 150, 75, 225, 1.8, "stop", "claude-3", "anthropic", user2, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), "tools:http:petstore:getPet", 500, 1.0, user2, "")

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Enabled)
	require.Len(t, result.Users, 2)

	// Index by user ID
	byUser := make(map[string]*gen.UserSummary)
	for _, u := range result.Users {
		byUser[u.UserID] = u
	}

	// User 1
	u1 := byUser[user1]
	require.NotNil(t, u1)
	require.Equal(t, int64(1), u1.TotalChats)
	require.Equal(t, int64(2), u1.TotalChatRequests)
	require.Equal(t, int64(300), u1.TotalInputTokens)  // 100 + 200
	require.Equal(t, int64(150), u1.TotalOutputTokens) // 50 + 100
	require.Equal(t, int64(450), u1.TotalTokens)       // 150 + 300
	require.Equal(t, int64(1), u1.TotalToolCalls)
	require.Equal(t, int64(1), u1.ToolCallSuccess)
	require.Equal(t, int64(0), u1.ToolCallFailure)
	require.Len(t, u1.Tools, 1)
	require.Equal(t, "tools:http:petstore:listPets", u1.Tools[0].Urn)
	require.Equal(t, int64(1), u1.Tools[0].Count)
	require.Equal(t, int64(1), u1.Tools[0].SuccessCount)
	require.Equal(t, int64(0), u1.Tools[0].FailureCount)

	// User 2
	u2 := byUser[user2]
	require.NotNil(t, u2)
	require.Equal(t, int64(1), u2.TotalChats)
	require.Equal(t, int64(1), u2.TotalChatRequests)
	require.Equal(t, int64(150), u2.TotalInputTokens)
	require.Equal(t, int64(75), u2.TotalOutputTokens)
	require.Equal(t, int64(225), u2.TotalTokens)
	require.Equal(t, int64(1), u2.TotalToolCalls)
	require.Equal(t, int64(0), u2.ToolCallSuccess)
	require.Equal(t, int64(1), u2.ToolCallFailure)
	require.Len(t, u2.Tools, 1)
	require.Equal(t, "tools:http:petstore:getPet", u2.Tools[0].Urn)
	require.Equal(t, int64(1), u2.Tools[0].Count)
	require.Equal(t, int64(0), u2.Tools[0].SuccessCount)
	require.Equal(t, int64(1), u2.Tools[0].FailureCount)
}

func TestSearchUsers_GroupByExternalUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	extUser := "ext-user-" + uuid.New().String()
	chatID := uuid.New().String()

	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, 100, 50, 150, 1.5, "stop", "gpt-4", "openai", "", extUser)
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), "tools:http:api:call", 200, 0.5, "", extUser)

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "external",
		Limit:    100,
		Sort:     "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Users, 1)

	u := result.Users[0]
	require.Equal(t, extUser, u.UserID)
	require.Equal(t, int64(1), u.TotalChats)
	require.Equal(t, int64(1), u.TotalChatRequests)
	require.Equal(t, int64(100), u.TotalInputTokens)
	require.Equal(t, int64(50), u.TotalOutputTokens)
	require.Equal(t, int64(150), u.TotalTokens)
	require.Equal(t, int64(1), u.TotalToolCalls)
	require.Equal(t, int64(1), u.ToolCallSuccess)
	require.Equal(t, int64(0), u.ToolCallFailure)
}

func TestSearchUsers_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	// Create 5 distinct users with staggered timestamps so last_seen differs
	for i := range 5 {
		userID := "paginated-user-" + uuid.New().String()
		ts := now.Add(-time.Duration(50-i*10) * time.Minute)
		insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, ts, uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", userID, "")
	}

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Page 1: limit 2
	page1, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    2,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, page1.Users, 2)
	require.NotNil(t, page1.NextCursor)

	// Page 2
	page2, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Cursor:   page1.NextCursor,
		Limit:    2,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, page2.Users, 2)
	require.NotNil(t, page2.NextCursor)

	// Page 3: remaining
	page3, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Cursor:   page2.NextCursor,
		Limit:    2,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, page3.Users, 1)
	require.Nil(t, page3.NextCursor)

	// Verify no duplicate user IDs across pages
	seen := make(map[string]bool)
	allUsers := append(append(page1.Users, page2.Users...), page3.Users...)
	for _, u := range allUsers {
		require.False(t, seen[u.UserID], "duplicate user ID across pages: %s", u.UserID)
		seen[u.UserID] = true
	}
	require.Len(t, seen, 5)
}

func TestSearchUsers_PaginationAscOrder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	for i := range 5 {
		userID := "asc-user-" + uuid.New().String()
		ts := now.Add(-time.Duration(50-i*10) * time.Minute)
		insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, ts, uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", userID, "")
	}

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Page 1
	page1, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    2,
		Sort:     "asc",
	})
	require.NoError(t, err)
	require.Len(t, page1.Users, 2)
	require.NotNil(t, page1.NextCursor)

	// Page 2
	page2, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Cursor:   page1.NextCursor,
		Limit:    2,
		Sort:     "asc",
	})
	require.NoError(t, err)
	require.Len(t, page2.Users, 2)
	require.NotNil(t, page2.NextCursor)

	// Page 3
	page3, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Cursor:   page2.NextCursor,
		Limit:    2,
		Sort:     "asc",
	})
	require.NoError(t, err)
	require.Len(t, page3.Users, 1)
	require.Nil(t, page3.NextCursor)

	// Verify no duplicates
	seen := make(map[string]bool)
	allUsers := append(append(page1.Users, page2.Users...), page3.Users...)
	for _, u := range allUsers {
		require.False(t, seen[u.UserID], "duplicate user ID across pages: %s", u.UserID)
		seen[u.UserID] = true
	}
	require.Len(t, seen, 5)
}

func TestSearchUsers_FilterByDeploymentID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deployment1 := uuid.New().String()
	deployment2 := uuid.New().String()

	now := time.Now().UTC()
	user1 := "deploy-user-1-" + uuid.New().String()
	user2 := "deploy-user-2-" + uuid.New().String()

	// User 1 in deployment 1
	insertChatCompletionLogWithUser(t, ctx, projectID, deployment1, now.Add(-10*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", user1, "")
	// User 2 in deployment 2
	insertChatCompletionLogWithUser(t, ctx, projectID, deployment2, now.Add(-9*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", user2, "")

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From:         from,
			To:           to,
			DeploymentID: &deployment1,
		},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})

	require.NoError(t, err)
	require.Len(t, result.Users, 1)
	require.Equal(t, user1, result.Users[0].UserID)
}

func TestSearchUsers_ToolsBreakdown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userID := "tools-user-" + uuid.New().String()

	// 3 calls to listPets (2 success, 1 failure) + 2 calls to getPet (both success)
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:http:petstore:listPets", 200, 0.5, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), "tools:http:petstore:listPets", 200, 0.4, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), "tools:http:petstore:listPets", 500, 1.0, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), "tools:http:petstore:getPet", 200, 0.3, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), "tools:http:petstore:getPet", 200, 0.2, userID, "")

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})

	require.NoError(t, err)
	require.Len(t, result.Users, 1)

	u := result.Users[0]
	require.Equal(t, userID, u.UserID)
	require.Equal(t, int64(5), u.TotalToolCalls)
	require.Equal(t, int64(4), u.ToolCallSuccess) // 2+2
	require.Equal(t, int64(1), u.ToolCallFailure) // 1

	// Per-tool breakdown
	require.Len(t, u.Tools, 2)
	toolStats := make(map[string]*gen.ToolUsage)
	for _, tool := range u.Tools {
		toolStats[tool.Urn] = tool
	}

	listPets := toolStats["tools:http:petstore:listPets"]
	require.NotNil(t, listPets)
	require.Equal(t, int64(3), listPets.Count)
	require.Equal(t, int64(2), listPets.SuccessCount)
	require.Equal(t, int64(1), listPets.FailureCount)

	getPet := toolStats["tools:http:petstore:getPet"]
	require.NotNil(t, getPet)
	require.Equal(t, int64(2), getPet.Count)
	require.Equal(t, int64(2), getPet.SuccessCount)
	require.Equal(t, int64(0), getPet.FailureCount)
}

func TestSearchUsers_ScopedByProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	otherProjectID := uuid.New().String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	sharedUserID := "shared-user-" + uuid.New().String()

	// Insert logs for the same user in both projects
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", sharedUserID, "")
	insertChatCompletionLogWithUser(t, ctx, otherProjectID, deploymentID, now.Add(-9*time.Minute), uuid.New().String(), 500, 250, 750, 2.0, "stop", "gpt-4", "openai", sharedUserID, "")

	// Insert a different user only in the other project
	otherUser := "other-project-user-" + uuid.New().String()
	insertChatCompletionLogWithUser(t, ctx, otherProjectID, deploymentID, now.Add(-8*time.Minute), uuid.New().String(), 200, 100, 300, 1.0, "stop", "gpt-4", "openai", otherUser, "")

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})

	require.NoError(t, err)
	require.Len(t, result.Users, 1, "should only return users from the queried project")
	require.Equal(t, sharedUserID, result.Users[0].UserID)

	// Metrics should only reflect the current project's data
	require.Equal(t, int64(100), result.Users[0].TotalInputTokens, "should not include tokens from other project")
	require.Equal(t, int64(150), result.Users[0].TotalTokens, "should not include tokens from other project")
}
