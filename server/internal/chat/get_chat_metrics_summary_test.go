package chat_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

func TestGetChatMetricsSummaryUsesLatestResolution(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ti := newTestChatService(t)
	queries := repo.New(ti.conn)
	chatID := seedChat(t, ctx, ti, "user-1", "", "resolved chat")

	for _, role := range []string{"user", "assistant"} {
		err := queries.CreateChatMessageWithToolCalls(ctx, repo.CreateChatMessageWithToolCallsParams{
			ChatID:     chatID,
			ProjectID:  uuid.NullUUID{UUID: ti.projectID, Valid: true},
			Role:       role,
			Content:    role + " message",
			ToolCalls:  nil,
			ToolCallID: pgtype.Text{},
			Generation: 0,
		})
		require.NoError(t, err)
	}

	_, err := queries.InsertChatResolution(ctx, repo.InsertChatResolutionParams{
		ProjectID:       ti.projectID,
		ChatID:          chatID,
		UserGoal:        "test",
		Resolution:      "failure",
		ResolutionNotes: "first result",
		Score:           0,
	})
	require.NoError(t, err)
	_, err = queries.InsertChatResolution(ctx, repo.InsertChatResolutionParams{
		ProjectID:       ti.projectID,
		ChatID:          chatID,
		UserGoal:        "test",
		Resolution:      "success",
		ResolutionNotes: "latest result",
		Score:           100,
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	result, err := queries.GetChatMetricsSummary(ctx, repo.GetChatMetricsSummaryParams{
		ProjectID: ti.projectID,
		TimeStart: pgtype.Timestamptz{Time: now.Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		TimeEnd:   pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.TotalChats)
	require.Equal(t, int64(1), result.ResolvedChats)
	require.Zero(t, result.FailedChats)

	emptyResult, err := queries.GetChatMetricsSummary(ctx, repo.GetChatMetricsSummaryParams{
		ProjectID: ti.projectID,
		TimeStart: pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		TimeEnd:   pgtype.Timestamptz{Time: now.Add(2 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	require.Zero(t, emptyResult.TotalChats)
	require.Zero(t, emptyResult.ResolvedChats)
	require.Zero(t, emptyResult.FailedChats)
	require.Zero(t, emptyResult.AvgSessionDurationMs)
	require.Zero(t, emptyResult.AvgResolutionTimeMs)
}
