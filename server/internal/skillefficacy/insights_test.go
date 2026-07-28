//nolint:paralleltest // Tests share the seeded organization's product feature cache.
package skillefficacy_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skill_efficacy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type insightsReaderStub struct {
	rows          []telemetryrepo.SkillInsightBucket
	sessions      []telemetryrepo.SkillEfficacyScoreSession
	queryParams   telemetryrepo.QuerySkillInsightsParams
	sessionParams telemetryrepo.ListSkillEfficacyScoreSessionsParams
	queryCalls    int
	sessionCalls  int
}

func (s *insightsReaderStub) QuerySkillInsights(_ context.Context, params telemetryrepo.QuerySkillInsightsParams) ([]telemetryrepo.SkillInsightBucket, error) {
	s.queryCalls++
	s.queryParams = params
	return s.rows, nil
}

func (s *insightsReaderStub) ListSkillEfficacyScoreSessions(_ context.Context, params telemetryrepo.ListSkillEfficacyScoreSessionsParams) ([]telemetryrepo.SkillEfficacyScoreSession, error) {
	s.sessionCalls++
	s.sessionParams = params
	return s.sessions, nil
}

func TestQueryInsightsAggregatesVersionsAndReturnsScoredSessions(t *testing.T) {
	skillID := uuid.NewString()
	firstVersionID := uuid.NewString()
	secondVersionID := uuid.NewString()
	chatID := uuid.NewString()
	from := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	to := from.Add(24 * time.Hour)
	reader := &insightsReaderStub{
		rows: []telemetryrepo.SkillInsightBucket{
			{SkillID: skillID, SkillVersionID: secondVersionID, BucketTimeUnixNano: from.UnixNano(), ActivationCount: 2, ActivatedSessions: 1, TotalSessionCost: 0.4},
			{SkillID: skillID, SkillVersionID: firstVersionID, BucketTimeUnixNano: from.UnixNano(), ActivationCount: 3, ActivatedSessions: 2, TotalSessionCost: 0.6, ScoredSessions: 1, ScoreSum: 0.8, EstimatedMinutesSavedSum: 10, EstimatedMinutesSamples: 1, ROIConfidenceHigh: 1},
		},
		sessions: []telemetryrepo.SkillEfficacyScoreSession{{
			ID: uuid.NewString(), SkillID: skillID, SkillVersionID: firstVersionID, Surface: "assistant",
			ActivatedAt: from, ScoredAt: to, Score: 0.8, Rationale: "The skill shortened the session.",
			EstimatedTurnsSaved: nil, EstimatedMinutesSaved: nil, ROIConfidence: nil, Flags: []string{}, GramChatID: chatID,
		}},
	}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead, authz.ScopeChatRead)
	include := true

	result, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{skillID, skillID},
		From: new(from.Format(time.RFC3339)), To: new(to.Format(time.RFC3339)),
		IncludeVersions: &include, IncludeScoredSessions: &include,
	})
	require.NoError(t, err)
	require.Equal(t, []string{skillID}, reader.queryParams.SkillIDs)
	require.EqualValues(t, 5, result.Insights[0].Metrics.Activations)
	require.EqualValues(t, 3, result.Insights[0].Metrics.ActivatedSessions)
	require.InDelta(t, 1, result.Insights[0].Metrics.SessionCostUsd, 0)
	require.NotNil(t, result.Insights[0].Metrics.Efficacy)
	require.InDelta(t, 0.8, result.Insights[0].Metrics.Efficacy.AverageScore, 0)
	require.Len(t, result.Insights[0].Versions, 2)
	require.Less(t, result.Insights[0].Versions[0].SkillVersionID, result.Insights[0].Versions[1].SkillVersionID)
	require.Equal(t, chatID, *result.ScoredSessions[0].GramChatID)
	require.EqualValues(t, 100, reader.sessionParams.Limit)
}

func TestQueryInsightsRequiresChatReadForScoredSessions(t *testing.T) {
	reader := &insightsReaderStub{}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead)
	include := true

	_, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{uuid.NewString()},
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: &include,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Zero(t, reader.queryCalls)
	require.Zero(t, reader.sessionCalls)
}

func TestQueryInsightsWithoutSkillIDsReturnsActiveProjectSkills(t *testing.T) {
	reader := &insightsReaderStub{}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	first, err := skillsrepo.New(ti.conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID: *authCtx.ProjectID, Name: "first", DisplayName: "First", Summary: pgtype.Text{},
	})
	require.NoError(t, err)
	second, err := skillsrepo.New(ti.conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID: *authCtx.ProjectID, Name: "second", DisplayName: "Second", Summary: pgtype.Text{},
	})
	require.NoError(t, err)
	reader.rows = []telemetryrepo.SkillInsightBucket{{SkillID: first.ID.String(), SkillVersionID: uuid.NewString(), ActivationCount: 3}}

	result, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: nil,
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil,
	})
	require.NoError(t, err)
	require.Empty(t, reader.queryParams.SkillIDs)
	require.Len(t, result.Insights, 2)
	require.Equal(t, first.ID.String(), result.Insights[0].SkillID)
	require.EqualValues(t, 3, result.Insights[0].Metrics.Activations)
	require.Equal(t, second.ID.String(), result.Insights[1].SkillID)
	require.Zero(t, result.Insights[1].Metrics.Activations)
	require.Nil(t, result.Insights[1].Metrics.Efficacy)
}

func TestQueryInsightsSkipsClickHouseForProjectWithoutSkills(t *testing.T) {
	reader := &insightsReaderStub{}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead)

	result, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: nil,
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Insights)
	require.Zero(t, reader.queryCalls)
}

func TestQueryInsightsRequiresSkillIDsBeforeScoredSessionQueries(t *testing.T) {
	reader := &insightsReaderStub{}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead, authz.ScopeChatRead)
	include := true

	_, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: nil,
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: &include,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Zero(t, reader.queryCalls)
	require.Zero(t, reader.sessionCalls)
}

func TestQueryInsightsValidatesIDsAndWindow(t *testing.T) {
	ctx, ti := newTestServiceWithInsights(t, &insightsReaderStub{})
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead)

	_, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{"not-a-uuid"},
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	from := time.Now().UTC()
	to := from.Add(-time.Hour)
	_, err = ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{uuid.NewString()},
		From: new(from.Format(time.RFC3339)), To: new(to.Format(time.RFC3339)),
		IncludeVersions: nil, IncludeScoredSessions: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}
