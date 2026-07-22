package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	gentypes "github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type stubSkillInsightsReader struct {
	params telemetryrepo.QuerySkillInsightsParams
	rows   []telemetryrepo.SkillInsightBucket
}

func (s *stubSkillInsightsReader) QuerySkillInsights(_ context.Context, params telemetryrepo.QuerySkillInsightsParams) ([]telemetryrepo.SkillInsightBucket, error) {
	s.params = params
	return s.rows, nil
}

func TestInsightsToolUsesAuthenticatedProjectAndDegradesWithoutScores(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	skillID := uuid.NewString()
	versionID := uuid.NewString()
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	svc := &stubSkillsService{listResult: &genskills.ListSkillsResult{Skills: []*gentypes.Skill{{ID: skillID, Name: "verification", DisplayName: "Verification"}}, NextCursor: nil}}
	reader := &stubSkillInsightsReader{rows: []telemetryrepo.SkillInsightBucket{{
		SkillID:                  skillID,
		SkillVersionID:           versionID,
		BucketTimeUnixNano:       from.UnixNano(),
		ActivationCount:          3,
		ActivatedSessions:        2,
		TotalSessionCost:         1.25,
		ScoredSessions:           0,
		ScoreSum:                 0,
		EstimatedTurnsSavedSum:   0,
		EstimatedTurnsSamples:    0,
		EstimatedMinutesSavedSum: 0,
		EstimatedMinutesSamples:  0,
		ROIConfidenceLow:         0,
		ROIConfidenceMed:         0,
		ROIConfidenceHigh:        0,
		IgnoredCount:             0,
		MisappliedCount:          0,
		PartiallyFollowedCount:   0,
		HarmfulCount:             0,
	}}}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-test", ProjectID: &projectID, ProjectSlug: nil})
	var out bytes.Buffer

	err := NewInsightsTool(svc, reader).Call(ctx, skillToolCallEnv(""), bytes.NewBufferString(`{"from":"2026-07-01T00:00:00Z","to":"2026-07-02T00:00:00Z"}`), &out)
	require.NoError(t, err)
	require.Equal(t, "org-test", reader.params.OrganizationID)
	require.Equal(t, projectID.String(), reader.params.ProjectID)
	require.Equal(t, []string{skillID}, reader.params.SkillIDs)
	require.Equal(t, from, reader.params.From)
	require.Equal(t, to, reader.params.To)
	require.Nil(t, svc.listPayload.SessionToken)
	require.Nil(t, svc.listPayload.ApikeyToken)
	require.Nil(t, svc.listPayload.ProjectSlugInput)

	var result insightsResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.False(t, result.ScoresAvailable)
	require.Len(t, result.Skills, 1)
	require.EqualValues(t, 3, result.Skills[0].Metrics.Activations)
	require.EqualValues(t, 2, result.Skills[0].Metrics.ActivatedSessions)
	require.InDelta(t, 1.25, result.Skills[0].Metrics.SessionCostUSD, 0)
	require.InDelta(t, 0.625, *result.Skills[0].Metrics.AverageSessionCostUSD, 0)
	require.Nil(t, result.Skills[0].Metrics.Efficacy)
}

func TestBuildInsightsResultCombinesWeightedScoreAndROI(t *testing.T) {
	t.Parallel()

	skillID := uuid.NewString()
	versionID := uuid.NewString()
	rows := []telemetryrepo.SkillInsightBucket{
		{SkillID: skillID, SkillVersionID: versionID, BucketTimeUnixNano: time.Now().UnixNano(), ActivationCount: 2, ActivatedSessions: 2, ScoredSessions: 2, ScoreSum: 1, EstimatedMinutesSavedSum: 10, EstimatedMinutesSamples: 1},
		{SkillID: skillID, SkillVersionID: versionID, BucketTimeUnixNano: time.Now().Add(24 * time.Hour).UnixNano(), ActivationCount: 4, ActivatedSessions: 3, ScoredSessions: 1, ScoreSum: 1, EstimatedMinutesSavedSum: 20, EstimatedMinutesSamples: 1},
	}

	result := buildInsightsResult(rows, map[string]*gentypes.Skill{skillID: {ID: skillID, Name: "verification", DisplayName: "Verification"}}, nil, "efficacy", 10)

	require.True(t, result.ScoresAvailable)
	require.Len(t, result.Skills, 1)
	metrics := result.Skills[0].Metrics
	require.EqualValues(t, 6, metrics.Activations)
	require.EqualValues(t, 5, metrics.ActivatedSessions)
	require.NotNil(t, metrics.Efficacy)
	require.InDelta(t, 2.0/3.0, metrics.Efficacy.AverageScore, 0.0001)
	require.InDelta(t, 30, metrics.Efficacy.EstimatedMinutesSavedTotal, 0)
	require.InDelta(t, 15, *metrics.Efficacy.EstimatedMinutesSavedAverage, 0)
}

func TestInsightWindowRejectsMoreThanNinetyDays(t *testing.T) {
	t.Parallel()

	from := "2026-01-01T00:00:00Z"
	to := "2026-05-01T00:00:00Z"
	_, _, err := insightWindow(&from, &to)
	require.EqualError(t, err, "insight window cannot exceed 90 days")
}
