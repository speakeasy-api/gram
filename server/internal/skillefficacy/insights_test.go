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
		IncludeVersions: &include, IncludeScoredSessions: &include, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20,
	})
	require.NoError(t, err)
	require.True(t, reader.queryParams.IncludeSessionUsage)
	require.Equal(t, []string{skillID}, reader.queryParams.SkillIDs)
	require.EqualValues(t, 5, result.Insights[0].Metrics.Activations)
	require.EqualValues(t, 3, result.Insights[0].Metrics.ActivatedSessions)
	require.InDelta(t, 1, result.Insights[0].Metrics.SessionCostUsd, 0)
	require.NotNil(t, result.Insights[0].Metrics.Efficacy)
	require.InDelta(t, 0.8, result.Insights[0].Metrics.Efficacy.AverageScore, 0)
	require.Len(t, result.Insights[0].Versions, 2)
	require.Less(t, result.Insights[0].Versions[0].SkillVersionID, result.Insights[0].Versions[1].SkillVersionID)
	require.Equal(t, chatID, *result.ScoredSessions[0].GramChatID)
	require.EqualValues(t, 21, reader.sessionParams.Limit)
	require.Nil(t, result.NextCursor)
}

func TestQueryInsightsPaginatesScoredSessions(t *testing.T) {
	skillID := uuid.NewString()
	from := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	to := from.Add(24 * time.Hour)
	firstID := uuid.NewString()
	reader := &insightsReaderStub{sessions: []telemetryrepo.SkillEfficacyScoreSession{
		{ID: firstID, SkillID: skillID, SkillVersionID: uuid.NewString(), Surface: "assistant", ActivatedAt: from, ScoredAt: to.Add(-time.Minute), Score: 0.8, Rationale: "First", EstimatedTurnsSaved: nil, EstimatedMinutesSaved: nil, ROIConfidence: nil, Flags: []string{}, GramChatID: ""},
		{ID: uuid.NewString(), SkillID: skillID, SkillVersionID: uuid.NewString(), Surface: "assistant", ActivatedAt: from, ScoredAt: to.Add(-2 * time.Minute), Score: 0.7, Rationale: "Second", EstimatedTurnsSaved: nil, EstimatedMinutesSaved: nil, ROIConfidence: nil, Flags: []string{}, GramChatID: ""},
	}}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead, authz.ScopeChatRead)
	include := true

	first, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{skillID}, From: new(from.Format(time.RFC3339)), To: new(to.Format(time.RFC3339)),
		IncludeVersions: nil, IncludeScoredSessions: &include, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, first.ScoredSessions, 1)
	require.NotNil(t, first.NextCursor)
	require.EqualValues(t, 2, reader.sessionParams.Limit)

	_, err = ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{skillID}, From: new(from.Format(time.RFC3339)), To: new(to.Format(time.RFC3339)),
		IncludeVersions: nil, IncludeScoredSessions: &include, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: first.NextCursor, Limit: 1,
	})
	require.NoError(t, err)
	require.Equal(t, firstID, reader.sessionParams.CursorID)
	require.Equal(t, to.Add(-time.Minute), reader.sessionParams.CursorScoredAt)
}

func TestQueryInsightsRequiresChatReadForScoredSessions(t *testing.T) {
	reader := &insightsReaderStub{}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead)
	include := true

	_, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{uuid.NewString()},
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: &include, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20,
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
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20,
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
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20,
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
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: &include, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20,
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
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20,
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	from := time.Now().UTC()
	to := from.Add(-time.Hour)
	_, err = ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{uuid.NewString()},
		From: new(from.Format(time.RFC3339)), To: new(to.Format(time.RFC3339)),
		IncludeVersions: nil, IncludeScoredSessions: nil, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestQueryInsightsRejectsInvalidScoredSessionsPagination(t *testing.T) {
	reader := &insightsReaderStub{}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead, authz.ScopeChatRead)
	include := true
	badCursor := "not-a-cursor"

	_, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{uuid.NewString()}, From: nil, To: nil,
		IncludeVersions: nil, IncludeScoredSessions: &include, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: &badCursor, Limit: 20,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Zero(t, reader.sessionCalls)

	_, err = ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{uuid.NewString()}, From: nil, To: nil,
		IncludeVersions: nil, IncludeScoredSessions: &include, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 101,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Zero(t, reader.sessionCalls)
}

func TestQueryInsightsRegressionSignalUsesSuggestionPolicyAndEffectiveVersions(t *testing.T) {
	reader := &insightsReaderStub{}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := skillsrepo.New(ti.conn)
	skill, err := queries.CreateSkill(ctx, skillsrepo.CreateSkillParams{ProjectID: *authCtx.ProjectID, Name: "regression", DisplayName: "Regression", Summary: pgtype.Text{}})
	require.NoError(t, err)
	createVersion := func(content string) skillsrepo.SkillVersion {
		version, err := queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
			Content: content, CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(), Description: pgtype.Text{},
			Metadata: []byte(`{}`), SpecValid: true, ValidationErrors: []byte(`[]`), CreatedByUserID: authCtx.UserID,
			ProjectID: *authCtx.ProjectID, SkillID: skill.ID,
		})
		require.NoError(t, err)
		return version
	}
	predecessor := createVersion("predecessor")
	current := createVersion("current")
	reader.rows = []telemetryrepo.SkillInsightBucket{
		{SkillID: skill.ID.String(), SkillVersionID: predecessor.ID.String(), ScoredSessions: 10, ScoreSum: 8},
		{SkillID: skill.ID.String(), SkillVersionID: current.ID.String(), ScoredSessions: 10, ScoreSum: 6},
	}

	result, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{skill.ID.String()}, From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20})
	require.NoError(t, err)
	signal := result.Insights[0].RegressionSignal
	require.NotNil(t, signal)
	require.True(t, signal.Comparable)
	require.True(t, signal.Regression)
	require.Equal(t, current.ID.String(), signal.CurrentVersionID)
	require.Equal(t, predecessor.ID.String(), *signal.PredecessorVersionID)
	require.InDelta(t, 0.6, signal.CurrentAverageScore, 0.000001)
	require.InDelta(t, 0.8, signal.PredecessorAverageScore, 0.000001)
	require.EqualValues(t, 10, signal.CurrentScoredSessions)
	require.EqualValues(t, 10, signal.PredecessorScoredSessions)

	_, err = queries.PromoteSkillVersion(ctx, skillsrepo.PromoteSkillVersionParams{ProjectID: *authCtx.ProjectID, SkillID: skill.ID, SkillVersionID: predecessor.ID})
	require.NoError(t, err)
	result, err = ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{skill.ID.String()}, From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20})
	require.NoError(t, err)
	signal = result.Insights[0].RegressionSignal
	require.NotNil(t, signal)
	require.True(t, signal.Comparable)
	require.False(t, signal.Regression)
	require.Equal(t, predecessor.ID.String(), signal.CurrentVersionID)
	require.Equal(t, current.ID.String(), *signal.PredecessorVersionID)
}

func TestQueryInsightsRegressionSignalIsNoncomparableBelowPolicyMinimum(t *testing.T) {
	reader := &insightsReaderStub{}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := skillsrepo.New(ti.conn)
	skill, err := queries.CreateSkill(ctx, skillsrepo.CreateSkillParams{ProjectID: *authCtx.ProjectID, Name: "noncomparable", DisplayName: "Noncomparable", Summary: pgtype.Text{}})
	require.NoError(t, err)
	versions := make([]skillsrepo.SkillVersion, 2)
	for i := range versions {
		versions[i], err = queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
			Content: uuid.NewString(), CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(), Description: pgtype.Text{},
			Metadata: []byte(`{}`), SpecValid: true, ValidationErrors: []byte(`[]`), CreatedByUserID: authCtx.UserID,
			ProjectID: *authCtx.ProjectID, SkillID: skill.ID,
		})
		require.NoError(t, err)
	}
	reader.rows = []telemetryrepo.SkillInsightBucket{
		{SkillID: skill.ID.String(), SkillVersionID: versions[0].ID.String(), ScoredSessions: 20, ScoreSum: 18},
		{SkillID: skill.ID.String(), SkillVersionID: versions[1].ID.String(), ScoredSessions: 9, ScoreSum: 1},
	}

	result, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{skill.ID.String()}, From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil, IncludeSessionCost: nil, IncludeRegressionSignal: nil, Cursor: nil, Limit: 20})
	require.NoError(t, err)
	require.NotNil(t, result.Insights[0].RegressionSignal)
	require.False(t, result.Insights[0].RegressionSignal.Comparable)
	require.False(t, result.Insights[0].RegressionSignal.Regression)
}

func TestQueryInsightsCanSkipSessionCostAndRegression(t *testing.T) {
	reader := &insightsReaderStub{}
	ctx, ti := newTestServiceWithInsights(t, reader)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withProjectGrants(t, ctx, authz.ScopeSkillRead)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := skillsrepo.New(ti.conn)
	skill, err := queries.CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID: *authCtx.ProjectID, Name: "summary-only", DisplayName: "Summary Only", Summary: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content: uuid.NewString(), CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(), Description: pgtype.Text{},
		Metadata: []byte(`{}`), SpecValid: true, ValidationErrors: []byte(`[]`), CreatedByUserID: authCtx.UserID,
		ProjectID: *authCtx.ProjectID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	include := false

	result, err := ti.service.QueryInsights(ctx, &gen.QueryInsightsPayload{
		SessionToken: nil, ProjectSlugInput: nil, SkillIds: []string{skill.ID.String()},
		From: nil, To: nil, IncludeVersions: nil, IncludeScoredSessions: nil,
		IncludeSessionCost: &include, IncludeRegressionSignal: &include, Cursor: nil, Limit: 20,
	})
	require.NoError(t, err)
	require.Equal(t, 1, reader.queryCalls)
	require.False(t, reader.queryParams.IncludeSessionUsage)
	require.Nil(t, result.Insights[0].RegressionSignal)
}
