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

func TestGetChatMetricsSummaryMatchesReferenceQuery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ti := newTestChatService(t)
	queries := repo.New(ti.conn)

	windowStart := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Minute)
	at := func(offset time.Duration) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: windowStart.Add(offset), InfinityModifier: pgtype.Finite, Valid: true}
	}
	seedMessages := func(chatID uuid.UUID, offsets ...time.Duration) {
		t.Helper()
		for _, offset := range offsets {
			_, err := queries.SeedChatMessage(ctx, repo.SeedChatMessageParams{
				ChatID:    chatID,
				ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
				CreatedAt: at(offset),
			})
			require.NoError(t, err)
		}
	}
	seedResolutions := func(chatID uuid.UUID, resolutions ...string) {
		t.Helper()
		for _, resolution := range resolutions {
			_, err := queries.InsertChatResolution(ctx, repo.InsertChatResolutionParams{
				ProjectID:       ti.projectID,
				ChatID:          chatID,
				UserGoal:        "test",
				Resolution:      resolution,
				ResolutionNotes: "",
				Score:           0,
			})
			require.NoError(t, err)
		}
	}

	// Messages straddle the window; only the in-window pair (30m apart)
	// counts. The later resolution wins.
	straddling := seedChat(t, ctx, ti, "user-1", "", "straddling")
	seedMessages(straddling, -30*time.Minute, 10*time.Minute, 40*time.Minute, 2*time.Hour)
	seedResolutions(straddling, "failure", "success")

	// Resolved then re-judged as failed: counts as failed.
	refailed := seedChat(t, ctx, ti, "user-1", "", "refailed")
	seedMessages(refailed, 5*time.Minute, 20*time.Minute)
	seedResolutions(refailed, "success", "failure")

	// Single in-window message and no resolution.
	unresolved := seedChat(t, ctx, ti, "user-2", "", "unresolved")
	seedMessages(unresolved, 30*time.Minute)

	// Neither success nor failure, but still counts towards resolution time.
	partial := seedChat(t, ctx, ti, "user-2", "", "partial")
	seedMessages(partial, 0, 50*time.Minute)
	seedResolutions(partial, "partial")

	// Only out-of-window messages: excluded even though it is resolved.
	outside := seedChat(t, ctx, ti, "user-3", "", "outside")
	seedMessages(outside, -2*time.Hour, 90*time.Minute)
	seedResolutions(outside, "success")

	// Soft-deleted chats are excluded.
	deleted := seedChat(t, ctx, ti, "user-3", "", "deleted")
	seedMessages(deleted, 15*time.Minute, 25*time.Minute)
	seedResolutions(deleted, "failure")
	_, err := queries.SoftDeleteChat(ctx, repo.SoftDeleteChatParams{ID: deleted, ProjectID: ti.projectID})
	require.NoError(t, err)

	result, err := queries.GetChatMetricsSummary(ctx, repo.GetChatMetricsSummaryParams{
		ProjectID: ti.projectID,
		TimeStart: at(0),
		TimeEnd:   at(time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, repo.GetChatMetricsSummaryRow{
		TotalChats:    4,
		ResolvedChats: 1,
		FailedChats:   1,
		// (30m + 15m + 0m + 50m) / 4 chats.
		AvgSessionDurationMs: float64((95 * time.Minute / 4).Milliseconds()),
		// (30m + 15m + 50m) / 3 chats with a resolution.
		AvgResolutionTimeMs: float64(95*time.Minute.Milliseconds()) / 3,
	}, result)

	windows := []struct {
		name  string
		start time.Duration
		end   time.Duration
	}{
		{name: "window", start: 0, end: time.Hour},
		{name: "wide", start: -3 * time.Hour, end: 3 * time.Hour},
		{name: "narrow", start: 10 * time.Minute, end: 20 * time.Minute},
		{name: "empty", start: 3 * time.Hour, end: 4 * time.Hour},
	}
	for _, w := range windows {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()

			params := repo.GetChatMetricsSummaryParams{ProjectID: ti.projectID, TimeStart: at(w.start), TimeEnd: at(w.end)}

			got, err := queries.GetChatMetricsSummary(t.Context(), params)
			require.NoError(t, err)

			want, err := queries.ReferenceChatMetricsSummary(t.Context(), repo.ReferenceChatMetricsSummaryParams(params))
			require.NoError(t, err)
			require.Equal(t, repo.GetChatMetricsSummaryRow(want), got)
		})
	}
}
