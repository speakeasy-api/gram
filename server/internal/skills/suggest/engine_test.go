package suggest

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

var policyNow = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	require.Equal(t, 5, config.MinUnreviewedFeedback)
	require.InDelta(t, 0.10, config.RegressionAbsoluteDelta, 0)
	require.Equal(t, uint64(10), config.MinRegressionScoredSessions)
	require.Equal(t, 90*24*time.Hour, config.TrendWindow)
	require.Equal(t, 7*24*time.Hour, config.ActivityWindow)
	require.Equal(t, 30*24*time.Hour, config.TranscriptWindow)
	require.Equal(t, int32(3), config.MaxTranscripts)
	require.Equal(t, int32(50), config.MaxFeedback)
	require.Equal(t, Model, config.Model)
	require.Equal(t, 120*time.Second, config.Timeout)
}

func TestConfigRejectsInvalidRegressionDelta(t *testing.T) {
	t.Parallel()

	for _, delta := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.1, 0, 1.1} {
		config := DefaultConfig()
		config.RegressionAbsoluteDelta = delta
		require.ErrorContains(t, config.validate(), "regression delta must be between zero and one")
	}
}

func TestFeedbackWakeCountsAllOutcomes(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	baseID := uuid.New()
	wake, reason := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID,
		latest:          nil,
		unreviewedCount: 5, newScoredSessions: 0, trend: trend{},
	})
	require.True(t, wake)
	require.Equal(t, "feedback", reason)
}

func TestRegressionRequiresComparableSessionCountsAndDelta(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	currentID := uuid.New()
	previousID := uuid.New()
	base := TrendBase{BaseVersionID: currentID, PredecessorVersionID: previousID}
	insufficient := EvaluateTrend(config, base, []telemetryrepo.SkillInsightBucket{
		{SkillVersionID: currentID.String(), ScoredSessions: 9, ScoreSum: 4.5},
		{SkillVersionID: previousID.String(), ScoredSessions: 20, ScoreSum: 16},
	})
	require.False(t, insufficient.Comparable)
	require.False(t, insufficient.Regression)

	comparableTrend := EvaluateTrend(config, base, []telemetryrepo.SkillInsightBucket{
		{SkillVersionID: currentID.String(), ScoredSessions: 10, ScoreSum: 6},
		{SkillVersionID: previousID.String(), ScoredSessions: 10, ScoreSum: 7},
	})
	require.True(t, comparableTrend.Comparable)
	require.True(t, comparableTrend.Regression)
	require.InDelta(t, 0.10, comparableTrend.AbsoluteDelta, 0.000001)
}

func TestSameBaseRegressionRequiresScoredAdvance(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	baseID := uuid.New()
	latest := suggestionWatermark(baseID, string(skills.EditSuggestionStatusOpen), 20, policyNow.Add(-time.Hour))
	wake, reason := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID, latest: latest,
		unreviewedCount: 0, newScoredSessions: 4, trend: trend{CurrentCount: 24, Comparable: true, Regression: true},
	})
	require.False(t, wake)
	require.Equal(t, "no_new_evidence", reason)

	wake, reason = shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID, latest: latest,
		unreviewedCount: 0, newScoredSessions: 5, trend: trend{CurrentCount: 20, Comparable: true, Regression: true},
	})
	require.True(t, wake)
	require.Equal(t, "regression", reason)
}

func TestDismissedRegressionRequiresStricterScoredAdvance(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	baseID := uuid.New()
	latest := suggestionWatermark(baseID, string(skills.EditSuggestionStatusDismissed), 20, policyNow.Add(-time.Hour))
	wake, _ := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID, latest: latest,
		unreviewedCount: 0, newScoredSessions: 9, trend: trend{CurrentCount: 29, Comparable: true, Regression: true},
	})
	require.False(t, wake)

	wake, reason := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID, latest: latest,
		unreviewedCount: 0, newScoredSessions: 10, trend: trend{CurrentCount: 20, Comparable: true, Regression: true},
	})
	require.True(t, wake)
	require.Equal(t, "regression", reason)
}

func TestDismissedWeeklyFloorRejectsSubthresholdFeedback(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	baseID := uuid.New()
	latest := suggestionWatermark(baseID, string(skills.EditSuggestionStatusDismissed), 20, policyNow.Add(-config.SuggestionFloor))
	for _, feedbackCount := range []int64{1, 4} {
		wake, reason := shouldWake(config, wakeEvidence{
			now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID, latest: latest,
			unreviewedCount: feedbackCount, newScoredSessions: 0, trend: trend{CurrentCount: 20},
		})
		require.False(t, wake, "feedback count %d", feedbackCount)
		require.Equal(t, "no_new_evidence", reason, "feedback count %d", feedbackCount)
	}
}

func TestDismissedFeedbackThresholdWakesImmediately(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	baseID := uuid.New()
	wake, reason := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID,
		latest:          suggestionWatermark(baseID, string(skills.EditSuggestionStatusDismissed), 20, policyNow),
		unreviewedCount: 5, newScoredSessions: 0, trend: trend{CurrentCount: 20},
	})
	require.True(t, wake)
	require.Equal(t, "feedback", reason)
}

func TestDismissedWeeklyFloorWakesAfterScoredAdvance(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	baseID := uuid.New()
	wake, reason := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID,
		latest:          suggestionWatermark(baseID, string(skills.EditSuggestionStatusDismissed), 20, policyNow.Add(-config.SuggestionFloor)),
		unreviewedCount: 0, newScoredSessions: 10, trend: trend{CurrentCount: 30},
	})
	require.True(t, wake)
	require.Equal(t, "weekly_floor", reason)
}

func TestWeeklyFloorRequiresElapsedTimeAndNewEvidence(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	baseID := uuid.New()
	latest := suggestionWatermark(baseID, string(skills.EditSuggestionStatusSuperseded), 20, policyNow.Add(-config.SuggestionFloor))
	wake, reason := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID, latest: latest,
		unreviewedCount: 1, newScoredSessions: 0, trend: trend{CurrentCount: 20},
	})
	require.True(t, wake)
	require.Equal(t, "weekly_floor", reason)

	wake, reason = shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: baseID, latest: latest,
		unreviewedCount: 0, newScoredSessions: 0, trend: trend{CurrentCount: 20},
	})
	require.False(t, wake)
	require.Equal(t, "no_new_evidence", reason)
}

func TestFirstRegressionWakesImmediately(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	wake, reason := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow, baseVersionID: uuid.New(), latest: nil,
		unreviewedCount: 0, newScoredSessions: 0, trend: trend{CurrentCount: 10, Comparable: true, Regression: true},
	})
	require.True(t, wake)
	require.Equal(t, "regression", reason)
}

func TestBaseResetWeakFeedbackWaitsForWeeklyFloor(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	baseID := uuid.New()
	latest := suggestionWatermark(uuid.New(), string(skills.EditSuggestionStatusDismissed), 100, policyNow)
	wake, reason := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow.Add(-config.SuggestionFloor + time.Second),
		baseVersionID: baseID, latest: latest, unreviewedCount: 1, newScoredSessions: 0, trend: trend{},
	})
	require.False(t, wake)
	require.Equal(t, "weekly_floor", reason)

	wake, reason = shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow.Add(-config.SuggestionFloor), baseVersionID: baseID,
		latest:          latest,
		unreviewedCount: 1, newScoredSessions: 0, trend: trend{},
	})
	require.True(t, wake)
	require.Equal(t, "base_reset", reason)
}

func TestFirstNonRegressionScoresWaitForWeeklyFloor(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	baseID := uuid.New()
	wake, reason := shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow.Add(-config.SuggestionFloor + time.Second),
		baseVersionID: baseID, latest: nil, unreviewedCount: 0, newScoredSessions: 0, trend: trend{CurrentCount: 5},
	})
	require.False(t, wake)
	require.Equal(t, "weekly_floor", reason)

	wake, reason = shouldWake(config, wakeEvidence{
		now: policyNow, lastSeenAt: policyNow, floorReferenceAt: policyNow.Add(-config.SuggestionFloor),
		baseVersionID: baseID, latest: nil, unreviewedCount: 0, newScoredSessions: 0, trend: trend{CurrentCount: 5},
	})
	require.True(t, wake)
	require.Equal(t, "base_reset", reason)
}

func TestClampRationaleTrimsWithoutSplittingRunes(t *testing.T) {
	t.Parallel()

	rationale := clampRationale(120, "  "+strings.Repeat("界", 200)+"  ")
	require.Equal(t, strings.Repeat("界", 120), rationale)
	require.LessOrEqual(t, utf8.RuneCountInString(rationale), 120)
}

func TestBuildPromptBoundsNotesDropsOldestEvidenceAndOmitsSessionIDs(t *testing.T) {
	t.Parallel()

	feedback := make([]repo.SkillFeedback, 50)
	for i := range feedback {
		feedback[i] = repo.SkillFeedback{
			Outcome: fmt.Sprintf("outcome-%d", i), Source: "test",
			Note:      pgtype.Text{String: strings.Repeat("\x00", skills.MaxFeedbackNoteRunes+1000), Valid: true},
			CreatedAt: pgtype.Timestamptz{Time: policyNow.Add(time.Duration(i) * time.Second), Valid: true},
		}
	}
	transcript := func(surface string) EvidenceTranscript {
		return EvidenceTranscript{
			Surface: surface, SkillVersionID: uuid.New(), ScoredAt: policyNow,
			Transcript: efficacy.Transcript{Omitted: "", Messages: []efficacy.TranscriptMessage{{Index: 1, Role: "user", Content: strings.Repeat("x", 100000)}}},
		}
	}
	prompt, err := BuildPrompt(DefaultConfig(), GenerateInput{
		OrganizationID: "unused", ProjectID: uuid.New(), SkillName: "bounded",
		Base: repo.ResolveSkillSuggestionBaseRow{BaseContent: "base"}, Feedback: feedback, Trend: trend{},
		Transcripts: []EvidenceTranscript{transcript("newest"), transcript("middle"), transcript("oldest")}, ValidationError: "",
	})
	require.NoError(t, err)
	require.LessOrEqual(t, utf8.RuneCount(prompt), maxPromptRunes)
	require.NotContains(t, string(prompt), "session_id")

	var payload promptPayload
	require.NoError(t, json.Unmarshal(prompt, &payload))
	require.GreaterOrEqual(t, len(payload.Feedback), DefaultConfig().MinUnreviewedFeedback)
	require.Less(t, len(payload.Feedback), len(feedback))
	require.Equal(t, "outcome-49", payload.Feedback[len(payload.Feedback)-1].Outcome)
	for _, item := range payload.Feedback {
		require.LessOrEqual(t, utf8.RuneCountInString(item.Note), skills.MaxFeedbackNoteRunes)
	}
	for _, item := range payload.Transcripts {
		require.NotEqual(t, "oldest", item.Surface)
	}
}

func TestBuildPromptRejectsOversizedBaseWithoutCallingModel(t *testing.T) {
	t.Parallel()

	_, err := BuildPrompt(DefaultConfig(), GenerateInput{
		OrganizationID: "unused", ProjectID: uuid.New(), SkillName: "oversized",
		Base:     repo.ResolveSkillSuggestionBaseRow{BaseContent: strings.Repeat("\x00", 50000)},
		Feedback: nil, Trend: trend{}, Transcripts: nil, ValidationError: "",
	})
	require.ErrorIs(t, err, ErrModelFailure)
}

func TestValidateGenerationRejectsEmptyRationale(t *testing.T) {
	t.Parallel()

	generation := Generation{Decision: DecisionDecline, Changes: nil, Rationale: "  "}
	err := ValidateGeneration(&generation, 0)
	require.ErrorIs(t, err, ErrModelFailure)
	require.ErrorContains(t, err, "rationale is empty")
}

func TestValidateGenerationClearsDeclinedProposal(t *testing.T) {
	t.Parallel()

	generation := Generation{
		Decision:  DecisionDecline,
		Changes:   []GeneratedChange{{Find: "a", Replace: "b", Rationale: "ignored", Evidence: nil}},
		Rationale: "The skill already covers this.",
	}
	require.NoError(t, ValidateGeneration(&generation, 1))
	require.Empty(t, generation.Changes)
}

func TestValidateGenerationDropsEvidenceOutsideTheSuppliedFeedback(t *testing.T) {
	t.Parallel()

	generation := Generation{
		Decision: DecisionPropose,
		Changes: []GeneratedChange{{
			Find: "a", Replace: "b", Rationale: "why", Evidence: []int{2, 0, 9, 2, 1},
		}},
		Rationale: "why",
	}
	require.NoError(t, ValidateGeneration(&generation, 2))
	require.Equal(t, []int{2, 1}, generation.Changes[0].Evidence)
}

func suggestionWatermark(baseID uuid.UUID, status string, scored int64, updated time.Time) *repo.SkillEditSuggestion {
	return &repo.SkillEditSuggestion{
		ID: uuid.New(), ProjectID: uuid.Nil, SkillID: uuid.Nil, BaseVersionID: baseID,
		Rationale: "", Status: status, ScoredSessionCount: scored,
		ApprovedByUserID: pgtype.Text{}, ApprovedAt: pgtype.Timestamptz{},
		CreatedAt: pgtype.Timestamptz{}, UpdatedAt: pgtype.Timestamptz{Time: updated, Valid: true},
	}
}
