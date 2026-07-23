package telemetry_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetChatAnalysisVerdictsByChatIDs covers the read path's three guarantees:
// duplicate publications of the same evaluation id collapse to one row, the
// newest verdict per chat wins, and rows from other judges or scopes never
// leak in.
func TestGetChatAnalysisVerdictsByChatIDs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	scoredChatID := uuid.NewString()
	unscoredChatID := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Second)

	duplicatedID := uuid.New()
	newest := repo.ChatAnalysisScore{
		ID:                 duplicatedID,
		CreatedAt:          now,
		OrganizationID:     ti.orgID,
		ProjectID:          ti.projectID,
		ChatID:             scoredChatID,
		Judge:              repo.ChatAnalysisJudgeWorkUnits,
		Score:              42,
		Detail:             `{"tasks":[],"session_units":42,"flags":[]}`,
		JudgeModel:         "test-model",
		JudgePromptVersion: "v1",
	}
	superseded := newest
	superseded.ID = uuid.New()
	superseded.CreatedAt = now.Add(-time.Hour)
	superseded.Score = 7
	superseded.Detail = `{"tasks":[],"session_units":7,"flags":[]}`
	otherJudge := newest
	otherJudge.ID = uuid.New()
	otherJudge.Judge = "other_judge"
	otherJudge.Score = 99
	otherOrg := newest
	otherOrg.ID = uuid.New()
	otherOrg.OrganizationID = uuid.NewString()
	otherOrg.Score = 17

	// The newest row is inserted twice to simulate an at-least-once retry.
	rows := []repo.ChatAnalysisScore{newest, newest, superseded, otherJudge, otherOrg}
	require.NoError(t, ti.chClient.InsertChatAnalysisScores(ctx, rows))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		got, err := ti.chClient.GetChatAnalysisVerdictsByChatIDs(ctx, repo.GetChatAnalysisVerdictsByChatIDsParams{
			OrganizationID: ti.orgID,
			ProjectID:      ti.projectID,
			Judge:          repo.ChatAnalysisJudgeWorkUnits,
			ChatIDs:        []string{scoredChatID, unscoredChatID},
		})
		require.NoError(c, err)
		require.Len(c, got, 1)

		verdict, ok := got[scoredChatID]
		require.True(c, ok)
		require.Equal(c, scoredChatID, verdict.ChatID)
		require.InDelta(c, 42.0, verdict.Score, 1e-9)
		require.Equal(c, newest.Detail, verdict.Detail)
		require.WithinDuration(c, now, verdict.ScoredAt, time.Second)
	}, 10*time.Second, 200*time.Millisecond)

	empty, err := ti.chClient.GetChatAnalysisVerdictsByChatIDs(ctx, repo.GetChatAnalysisVerdictsByChatIDsParams{
		OrganizationID: ti.orgID,
		ProjectID:      ti.projectID,
		Judge:          repo.ChatAnalysisJudgeWorkUnits,
		ChatIDs:        nil,
	})
	require.NoError(t, err)
	require.Empty(t, empty)
}
