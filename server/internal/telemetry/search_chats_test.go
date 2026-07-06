package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchChats_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
		Filter: &gen.SearchChatsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 50,
		Sort:  "desc",
	})

	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "logs are not enabled")
}

func TestSearchChats_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
		Filter: &gen.SearchChatsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 50,
		Sort:  "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Chats)
	require.Nil(t, result.NextCursor)
}

func TestSearchChats_AggregatesByChatID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()

	// Chat 1: 2 completion messages + 1 tool call
	insertChatLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, 100, 50, 150, 1.5, "stop", "gpt-4", "openai")
	insertChatLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID1, 200, 100, 300, 2.0, "tool_calls", "gpt-4", "openai")
	insertToolCallLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID1, "tools:http:petstore:listPets", 200, 0.5)

	// Chat 2: 1 completion message + 1 failed tool call
	insertChatLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), chatID2, 150, 75, 225, 1.8, "stop", "claude-3", "anthropic")
	insertToolCallLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), chatID2, "tools:http:petstore:getPet", 500, 1.0)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
			Filter: &gen.SearchChatsFilter{
				From: &from,
				To:   &to,
			},
			Limit: 100,
			Sort:  "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Chats, 2)

		// Find both chats
		chatsByID := make(map[string]*gen.ChatSummary)
		for _, chat := range res.Chats {
			chatsByID[chat.GramChatID] = chat
		}

		// Chat 1 assertions
		c1 := chatsByID[chatID1]
		if !assert.NotNil(c, c1) {
			return
		}
		assert.Equal(c, uint64(3), c1.LogCount)      // 2 completions + 1 tool call
		assert.Equal(c, uint64(1), c1.ToolCallCount) // 1 tool call
		assert.Equal(c, uint64(2), c1.MessageCount)  // 2 completions
		assert.Greater(c, c1.DurationSeconds, float64(0))
		assert.Equal(c, "success", c1.Status) // no failed tool calls
		assert.NotNil(c, c1.Model)
		assert.Equal(c, "gpt-4", *c1.Model)
		assert.Equal(c, int64(300), c1.TotalInputTokens)  // 100 + 200
		assert.Equal(c, int64(150), c1.TotalOutputTokens) // 50 + 100
		assert.Equal(c, int64(450), c1.TotalTokens)       // 150 + 300
		assert.Positive(c, c1.StartTimeUnixNano)
		assert.Positive(c, c1.EndTimeUnixNano)

		// Chat 2 assertions
		c2 := chatsByID[chatID2]
		if !assert.NotNil(c, c2) {
			return
		}
		assert.Equal(c, uint64(2), c2.LogCount) // 1 completion + 1 tool call
		assert.Equal(c, uint64(1), c2.ToolCallCount)
		assert.Equal(c, uint64(1), c2.MessageCount)
		assert.Equal(c, "error", c2.Status) // failed tool call (status 500)
		assert.NotNil(c, c2.Model)
		assert.Equal(c, "claude-3", *c2.Model)
		assert.Equal(c, int64(150), c2.TotalInputTokens)
		assert.Equal(c, int64(75), c2.TotalOutputTokens)
		assert.Equal(c, int64(225), c2.TotalTokens)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchChats_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	// Create 5 distinct chats
	for i := range 5 {
		chatID := uuid.New().String()
		ts := now.Add(-time.Duration(50-i*10) * time.Minute)
		insertChatLogWithChatID(t, ctx, projectID, deploymentID, ts, chatID, 100, 50, 150, 1.0, "stop", "gpt-4", "openai")
	}

	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Page 1: limit 2
	var page1 *gen.SearchChatsResult
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
			Filter: &gen.SearchChatsFilter{
				From: &from,
				To:   &to,
			},
			Limit: 2,
			Sort:  "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Chats, 2)
		assert.NotNil(c, res.NextCursor)
		page1 = res
	}, 10*time.Second, 200*time.Millisecond)

	// Page 2
	page2, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
		Filter: &gen.SearchChatsFilter{
			From: &from,
			To:   &to,
		},
		Cursor: page1.NextCursor,
		Limit:  2,
		Sort:   "desc",
	})
	require.NoError(t, err)
	require.Len(t, page2.Chats, 2)
	require.NotNil(t, page2.NextCursor)

	// Page 3: remaining
	page3, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
		Filter: &gen.SearchChatsFilter{
			From: &from,
			To:   &to,
		},
		Cursor: page2.NextCursor,
		Limit:  2,
		Sort:   "desc",
	})
	require.NoError(t, err)
	require.Len(t, page3.Chats, 1)
	require.Nil(t, page3.NextCursor)

	// Verify no duplicate chat IDs across pages
	seen := make(map[string]bool)
	allChats := append(append(page1.Chats, page2.Chats...), page3.Chats...)
	for _, chat := range allChats {
		require.False(t, seen[chat.GramChatID], "duplicate chat ID across pages: %s", chat.GramChatID)
		seen[chat.GramChatID] = true
	}
}

func TestSearchChats_FilterByDeploymentID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deployment1 := uuid.New().String()
	deployment2 := uuid.New().String()

	now := time.Now().UTC()

	insertChatLogWithChatID(t, ctx, projectID, deployment1, now.Add(-10*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai")
	insertChatLogWithChatID(t, ctx, projectID, deployment2, now.Add(-9*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai")
	insertChatLogWithChatID(t, ctx, projectID, deployment1, now.Add(-8*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
			Filter: &gen.SearchChatsFilter{
				From:         &from,
				To:           &to,
				DeploymentID: &deployment1,
			},
			Limit: 100,
			Sort:  "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Chats, 2)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchChats_PaginationAscOrder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	// Create 5 distinct chats with ascending timestamps
	for i := range 5 {
		chatID := uuid.New().String()
		ts := now.Add(-time.Duration(50-i*10) * time.Minute)
		insertChatLogWithChatID(t, ctx, projectID, deploymentID, ts, chatID, 100, 50, 150, 1.0, "stop", "gpt-4", "openai")
	}

	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Page 1: limit 2 with ascending sort
	var page1 *gen.SearchChatsResult
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
			Filter: &gen.SearchChatsFilter{
				From: &from,
				To:   &to,
			},
			Limit: 2,
			Sort:  "asc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Chats, 2)
		assert.NotNil(c, res.NextCursor)
		page1 = res
	}, 10*time.Second, 200*time.Millisecond)

	// Page 2
	page2, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
		Filter: &gen.SearchChatsFilter{
			From: &from,
			To:   &to,
		},
		Cursor: page1.NextCursor,
		Limit:  2,
		Sort:   "asc",
	})
	require.NoError(t, err)
	require.Len(t, page2.Chats, 2)
	require.NotNil(t, page2.NextCursor)

	// Page 3: remaining
	page3, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
		Filter: &gen.SearchChatsFilter{
			From: &from,
			To:   &to,
		},
		Cursor: page2.NextCursor,
		Limit:  2,
		Sort:   "asc",
	})
	require.NoError(t, err)
	require.Len(t, page3.Chats, 1)
	require.Nil(t, page3.NextCursor)

	// Verify ascending order across pages
	allChats := append(append(page1.Chats, page2.Chats...), page3.Chats...)
	for i := 0; i < len(allChats)-1; i++ {
		require.Less(t, allChats[i].StartTimeUnixNano, allChats[i+1].StartTimeUnixNano,
			"chats should be sorted ascending by start time across pages")
	}

	// Verify no duplicates
	seen := make(map[string]bool)
	for _, chat := range allChats {
		require.False(t, seen[chat.GramChatID], "duplicate chat ID across pages: %s", chat.GramChatID)
		seen[chat.GramChatID] = true
	}
}

func TestSearchChats_ChatWithOnlyTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID := uuid.New().String()

	// Insert only tool calls, no completion messages
	insertToolCallLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, "tools:http:petstore:listPets", 200, 0.5)
	insertToolCallLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID, "tools:http:petstore:getPet", 200, 0.3)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
			Filter: &gen.SearchChatsFilter{
				From: &from,
				To:   &to,
			},
			Limit: 100,
			Sort:  "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Chats, 1)
		if len(res.Chats) != 1 {
			return
		}

		chat := res.Chats[0]
		assert.Equal(c, chatID, chat.GramChatID)
		assert.Equal(c, uint64(2), chat.LogCount)
		assert.Equal(c, uint64(2), chat.ToolCallCount)
		assert.Equal(c, uint64(0), chat.MessageCount) // No completion messages
		// Model is nil or empty string when no completions (anyIf returns empty string)
		if chat.Model != nil {
			assert.Empty(c, *chat.Model, "Model should be empty when no completions")
		}
		assert.Equal(c, int64(0), chat.TotalInputTokens)
		assert.Equal(c, int64(0), chat.TotalOutputTokens)
		assert.Equal(c, int64(0), chat.TotalTokens)
		assert.Equal(c, "success", chat.Status) // All tools succeeded (200)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchChats_ChatWithOnlyMessages(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID := uuid.New().String()

	// Insert only completion messages, no tool calls
	insertChatLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, 100, 50, 150, 1.5, "stop", "gpt-4", "openai")
	insertChatLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID, 200, 100, 300, 2.0, "stop", "gpt-4", "openai")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
			Filter: &gen.SearchChatsFilter{
				From: &from,
				To:   &to,
			},
			Limit: 100,
			Sort:  "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Chats, 1)
		if len(res.Chats) != 1 {
			return
		}

		chat := res.Chats[0]
		assert.Equal(c, chatID, chat.GramChatID)
		assert.Equal(c, uint64(2), chat.LogCount)
		assert.Equal(c, uint64(0), chat.ToolCallCount) // No tool calls
		assert.Equal(c, uint64(2), chat.MessageCount)
		assert.NotNil(c, chat.Model)
		assert.Equal(c, "gpt-4", *chat.Model)
		assert.Equal(c, int64(300), chat.TotalInputTokens)  // 100 + 200
		assert.Equal(c, int64(150), chat.TotalOutputTokens) // 50 + 100
		assert.Equal(c, int64(450), chat.TotalTokens)       // 150 + 300
		assert.Equal(c, "success", chat.Status)             // No failed tools
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchChats_FilterByGramURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	// Chat 1: petstore tools
	chatID1 := uuid.New().String()
	insertToolCallLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, "tools:http:petstore:listPets", 200, 0.5)

	// Chat 2: weather tools
	chatID2 := uuid.New().String()
	insertToolCallLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID2, "tools:http:weather:forecast", 200, 0.3)

	// Chat 3: another petstore tool
	chatID3 := uuid.New().String()
	insertToolCallLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID3, "tools:http:petstore:getPet", 200, 0.4)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Filter by "petstore" substring - should match 2 chats
	gramURN := "petstore"
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
			Filter: &gen.SearchChatsFilter{
				From:    &from,
				To:      &to,
				GramUrn: &gramURN,
			},
			Limit: 100,
			Sort:  "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Chats, 2, "should match 2 chats with petstore in gram_urn")

		// Verify the matched chats are the petstore ones
		chatIDs := make(map[string]bool)
		for _, chat := range res.Chats {
			chatIDs[chat.GramChatID] = true
		}
		assert.True(c, chatIDs[chatID1], "should include chat with listPets")
		assert.True(c, chatIDs[chatID3], "should include chat with getPet")
		assert.False(c, chatIDs[chatID2], "should not include chat with weather")
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchChats_PaginationCursorScopedByProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	otherProjectID := uuid.New().String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	// Create 3 chats in the user's project with distinct timestamps
	chatIDs := make([]string, 3)
	for i := range 3 {
		chatIDs[i] = uuid.New().String()
		ts := now.Add(-time.Duration(30-i*10) * time.Minute)
		insertChatLogWithChatID(t, ctx, projectID, deploymentID, ts, chatIDs[i], 100, 50, 150, 1.0, "stop", "gpt-4", "openai")
	}

	// Insert a chat in a different project using the same chat ID as the cursor
	// candidate (chatIDs[0]) but with a very different timestamp. If the cursor
	// subquery is not scoped by project, it would pick up this row's timestamp
	// and corrupt pagination.
	insertChatLogWithChatID(t, ctx, otherProjectID, deploymentID, now.Add(-5*time.Hour), chatIDs[0], 100, 50, 150, 1.0, "stop", "gpt-4", "openai")

	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Page 1: get first 2 chats
	var page1 *gen.SearchChatsResult
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
			Filter: &gen.SearchChatsFilter{
				From: &from,
				To:   &to,
			},
			Limit: 2,
			Sort:  "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Chats, 2)
		assert.NotNil(c, res.NextCursor)
		page1 = res
	}, 10*time.Second, 200*time.Millisecond)

	// Page 2: should return exactly the remaining 1 chat from this project
	page2, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
		Filter: &gen.SearchChatsFilter{
			From: &from,
			To:   &to,
		},
		Cursor: page1.NextCursor,
		Limit:  2,
		Sort:   "desc",
	})
	require.NoError(t, err)
	require.Len(t, page2.Chats, 1, "should only see chats from the queried project")
	require.Nil(t, page2.NextCursor)

	// Verify all 3 chats from our project are returned and no duplicates
	seen := make(map[string]bool)
	allChats := make([]*gen.ChatSummary, 0, len(page1.Chats)+len(page2.Chats))
	allChats = append(allChats, page1.Chats...)
	allChats = append(allChats, page2.Chats...)
	for _, chat := range allChats {
		require.False(t, seen[chat.GramChatID], "duplicate chat ID across pages: %s", chat.GramChatID)
		seen[chat.GramChatID] = true
	}
	require.Len(t, seen, 3)
}

func TestSearchLogs_FilterByGramChatID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID := uuid.New().String()

	// Insert 2 logs with chatID and 1 without
	insertChatLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, 100, 50, 150, 1.0, "stop", "gpt-4", "openai")
	insertToolCallLogWithChatID(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID, "tools:http:test:op", 200, 0.5)
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), nil, "urn:gram:other", "INFO")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
			Filter: &gen.SearchLogsFilter{
				From:       &from,
				To:         &to,
				GramChatID: &chatID,
			},
			Limit: 100,
			Sort:  "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Logs, 2, "should only return logs matching gram_chat_id")
	}, 10*time.Second, 200*time.Millisecond)
}

// TestSearchChats_DerivesTotalTokensWhenProviderOmitsTotal reproduces DNO-323:
// AI-coding providers like Claude Code report gen_ai.usage.input_tokens and
// gen_ai.usage.output_tokens but never emit gen_ai.usage.total_tokens (see
// writeMetricsToClickHouse in internal/hooks). A session built solely from these
// rows must still surface a non-zero total — derived from input + output —
// instead of showing "0 tokens".
func TestSearchChats_DerivesTotalTokensWhenProviderOmitsTotal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID := uuid.New().String()

	// Two Claude Code metric rows: input/output tokens present, total omitted.
	insertClaudeCodeMetricLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, 100, 50, "claude-sonnet-4")
	insertClaudeCodeMetricLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID, 200, 80, "claude-sonnet-4")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchChats(ctx, &gen.SearchChatsPayload{
		Filter: &gen.SearchChatsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 100,
		Sort:  "desc",
	})

	require.NoError(t, err)
	require.Len(t, result.Chats, 1)

	chat := result.Chats[0]
	require.Equal(t, chatID, chat.GramChatID)
	require.Equal(t, int64(300), chat.TotalInputTokens)  // 100 + 200
	require.Equal(t, int64(130), chat.TotalOutputTokens) // 50 + 80
	// Provider never emitted gen_ai.usage.total_tokens; the list must derive it
	// from input + output rather than report 0.
	require.Equal(t, int64(430), chat.TotalTokens) // 300 + 130
}

// insertClaudeCodeMetricLog inserts a Claude Code usage-metric row that mirrors
// writeMetricsToClickHouse: input/output tokens and cost are reported but
// gen_ai.usage.total_tokens is deliberately absent, and the row carries
// hook_source=claude-code rather than a tool URN.
func insertClaudeCodeMetricLog(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID string, inputTokens, outputTokens int, model string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":     chatID,
		"gen_ai.usage.input_tokens":  inputTokens,
		"gen_ai.usage.output_tokens": outputTokens,
		"gen_ai.usage.cost":          0.42,
		"gen_ai.response.model":      model,
		"gram.hook.source":           "claude-code",
		// Note: gen_ai.usage.total_tokens is intentionally omitted.
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name,
			gram_chat_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "claude code metric",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "", "gram-hooks",
		chatID)
	require.NoError(t, err)
}

// insertChatLogWithChatID inserts a chat completion log with the gram_chat_id column set.
func insertChatLogWithChatID(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID string, inputTokens, outputTokens, totalTokens int, durationSec float64, finishReason, model, provider string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":         chatID,
		"gen_ai.conversation.duration":   durationSec,
		"gen_ai.response.finish_reasons": "['" + finishReason + "']",
		"gen_ai.response.id":             uuid.New().String(),
		"gen_ai.usage.input_tokens":      inputTokens,
		"gen_ai.usage.output_tokens":     outputTokens,
		"gen_ai.usage.total_tokens":      totalTokens,
		"gen_ai.response.model":          model,
		"gen_ai.provider.name":           provider,
		"gram.resource.urn":              "chat:completion",
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name,
			gram_chat_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat completion",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "chat:completion", "gram-server",
		chatID)
	require.NoError(t, err)
}

// insertToolCallLogWithChatID inserts a tool call log with the gram_chat_id column set.
func insertToolCallLogWithChatID(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID, toolURN string, statusCode int32, durationSec float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.tool.urn":                toolURN,
		"http.server.request.duration": durationSec,
		"http.response.status_code":    statusCode,
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name,
			gram_chat_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "tool call",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, toolURN, "gram-tools",
		chatID)
	require.NoError(t, err)
}
