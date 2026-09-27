package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	testInsightSkillA        = "aaaaaaaa-1111-4111-8111-111111111111"
	testInsightSkillB        = "bbbbbbbb-2222-4222-8222-222222222222"
	testInsightSkillC        = "cccccccc-3333-4333-8333-333333333333"
	testInsightVersionAOld   = "aaaaaaaa-0000-4000-8000-000000000001"
	testInsightVersionANew   = "aaaaaaaa-0000-4000-8000-000000000002"
	testInsightVersionB      = "bbbbbbbb-0000-4000-8000-000000000001"
	testInsightVersionC      = "cccccccc-0000-4000-8000-000000000001"
	testInsightProjectID     = "44444444-4444-4444-8444-444444444444"
	testInsightVersionOldAt  = "2026-07-01T00:00:00Z"
	testInsightVersionNewAt  = "2026-08-01T00:00:00Z"
	testInsightVersionOtherA = "2026-07-15T00:00:00Z"
)

var testInsightNow = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

// stubSkillInsightsReader records what the service asked ClickHouse for and
// answers with canned buckets. The mutex is for the vertical tests, where the
// server reads it on another goroutine than the one seeding it.
type stubSkillInsightsReader struct {
	mu        sync.Mutex
	params    *telemetryrepo.QuerySkillInsightsParams
	rows      []telemetryrepo.SkillInsightBucket
	watermark int64
	err       error
}

func (s *stubSkillInsightsReader) QuerySkillInsights(_ context.Context, params telemetryrepo.QuerySkillInsightsParams) ([]telemetryrepo.SkillInsightBucket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.params = &params
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

func (s *stubSkillInsightsReader) GetTelemetryWatermark(context.Context, telemetryrepo.GetTelemetryWatermarkParams) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watermark, nil
}

// SetRows replaces the canned buckets once the test knows the real IDs.
func (s *stubSkillInsightsReader) SetRows(rows []telemetryrepo.SkillInsightBucket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = rows
}

// LastParams is the most recent ClickHouse read, or nil when none happened.
func (s *stubSkillInsightsReader) LastParams() *telemetryrepo.QuerySkillInsightsParams {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.params
}

// registrySkillsManagement is a skills registry with several skills, each with
// its own version list. It records which skills had their versions read so a
// test can prove the service only pages versions for the skills it ranks.
type registrySkillsManagement struct {
	recordingSkillsManagement
	skills           []*types.Skill
	versionsBySkill  map[string]*genskills.ListSkillVersionsResult
	moreSkills       bool
	listCalls        int
	getCalls         []string
	listVersionCalls []string
}

func (s *registrySkillsManagement) List(_ context.Context, _ *genskills.ListPayload) (*genskills.ListSkillsResult, error) {
	s.listCalls++
	var next *string
	if s.moreSkills {
		cursor := "more"
		next = &cursor
	}
	return &genskills.ListSkillsResult{Skills: s.skills, TotalCount: int64(len(s.skills)), NextCursor: next}, nil
}

func (s *registrySkillsManagement) Get(_ context.Context, payload *genskills.GetPayload) (*genskills.GetSkillResult, error) {
	s.getCalls = append(s.getCalls, payload.ID)
	for _, skill := range s.skills {
		if skill.ID == payload.ID {
			return &genskills.GetSkillResult{Skill: skill, LatestVersion: nil, Adoption: nil, SightingTimeline: nil, Drift: nil, AssistantCount: 0, PromptInjectionFindings: nil}, nil
		}
	}
	return nil, fmt.Errorf("skill %s not in fixture", payload.ID)
}

func (s *registrySkillsManagement) ListVersions(_ context.Context, payload *genskills.ListVersionsPayload) (*genskills.ListSkillVersionsResult, error) {
	s.listVersionCalls = append(s.listVersionCalls, payload.ID)
	if versions, ok := s.versionsBySkill[payload.ID]; ok {
		return versions, nil
	}
	return &genskills.ListSkillVersionsResult{Versions: nil, NextCursor: nil}, nil
}

func insightSkill(id, name string) *types.Skill {
	return &types.Skill{ID: id, Name: name, DisplayName: strings.ToUpper(name[:1]) + name[1:], Summary: nil, Tags: nil, LatestVersionID: nil, VersionCount: 1, HasValidVersion: true, UpdatedAt: testInsightVersionNewAt}
}

func insightVersions(versions ...*types.SkillVersion) *genskills.ListSkillVersionsResult {
	return &genskills.ListSkillVersionsResult{Versions: versions, NextCursor: nil}
}

func insightVersion(id, skillID, createdAt string) *types.SkillVersion {
	return &types.SkillVersion{ID: id, SkillID: skillID, Content: "", CanonicalSha256: "", SpecValid: true, CreatedAt: createdAt}
}

func testInsightRegistry() *registrySkillsManagement {
	return &registrySkillsManagement{
		skills: []*types.Skill{
			insightSkill(testInsightSkillA, "verification"),
			insightSkill(testInsightSkillB, "release-notes"),
			insightSkill(testInsightSkillC, "triage"),
		},
		versionsBySkill: map[string]*genskills.ListSkillVersionsResult{
			testInsightSkillA: insightVersions(insightVersion(testInsightVersionAOld, testInsightSkillA, testInsightVersionOldAt), insightVersion(testInsightVersionANew, testInsightSkillA, testInsightVersionNewAt)),
			testInsightSkillB: insightVersions(insightVersion(testInsightVersionB, testInsightSkillB, testInsightVersionOtherA)),
			testInsightSkillC: insightVersions(insightVersion(testInsightVersionC, testInsightSkillC, testInsightVersionOtherA)),
		},
	}
}

// testInsightRows: skill A is scored across two versions and saved the most
// time, skill B is the most expensive but unscored, skill C is scored with a
// higher average score but less time saved.
func testInsightRows() []telemetryrepo.SkillInsightBucket {
	return []telemetryrepo.SkillInsightBucket{
		{SkillID: testInsightSkillA, SkillVersionID: testInsightVersionAOld, BucketTimeUnixNano: testInsightNow.Add(-20 * 24 * time.Hour).UnixNano(), ActivationCount: 4, ActivatedSessions: 2, TotalSessionCost: 1.0, ScoredSessions: 1, ScoreSum: 0.5, EstimatedTurnsSavedSum: 2, EstimatedTurnsSamples: 1, EstimatedMinutesSavedSum: 10, EstimatedMinutesSamples: 1, ROIConfidenceLow: 1, ROIConfidenceMed: 0, ROIConfidenceHigh: 0, IgnoredCount: 0, MisappliedCount: 0, PartiallyFollowedCount: 1, HarmfulCount: 0},
		{SkillID: testInsightSkillA, SkillVersionID: testInsightVersionANew, BucketTimeUnixNano: testInsightNow.Add(-2 * 24 * time.Hour).UnixNano(), ActivationCount: 6, ActivatedSessions: 4, TotalSessionCost: 2.0, ScoredSessions: 2, ScoreSum: 1.6, EstimatedTurnsSavedSum: 6, EstimatedTurnsSamples: 2, EstimatedMinutesSavedSum: 30, EstimatedMinutesSamples: 2, ROIConfidenceLow: 0, ROIConfidenceMed: 1, ROIConfidenceHigh: 1, IgnoredCount: 0, MisappliedCount: 0, PartiallyFollowedCount: 0, HarmfulCount: 0},
		{SkillID: testInsightSkillB, SkillVersionID: testInsightVersionB, BucketTimeUnixNano: testInsightNow.Add(-2 * 24 * time.Hour).UnixNano(), ActivationCount: 9, ActivatedSessions: 8, TotalSessionCost: 12.0, ScoredSessions: 0, ScoreSum: 0, EstimatedTurnsSavedSum: 0, EstimatedTurnsSamples: 0, EstimatedMinutesSavedSum: 0, EstimatedMinutesSamples: 0, ROIConfidenceLow: 0, ROIConfidenceMed: 0, ROIConfidenceHigh: 0, IgnoredCount: 0, MisappliedCount: 0, PartiallyFollowedCount: 0, HarmfulCount: 0},
		{SkillID: testInsightSkillC, SkillVersionID: testInsightVersionC, BucketTimeUnixNano: testInsightNow.Add(-2 * 24 * time.Hour).UnixNano(), ActivationCount: 2, ActivatedSessions: 2, TotalSessionCost: 0.5, ScoredSessions: 2, ScoreSum: 1.9, EstimatedTurnsSavedSum: 1, EstimatedTurnsSamples: 1, EstimatedMinutesSavedSum: 5, EstimatedMinutesSamples: 1, ROIConfidenceLow: 0, ROIConfidenceMed: 0, ROIConfidenceHigh: 2, IgnoredCount: 0, MisappliedCount: 0, PartiallyFollowedCount: 0, HarmfulCount: 0},
	}
}

type insightsFixture struct {
	service     *SkillsService
	registry    *registrySkillsManagement
	reader      *stubSkillInsightsReader
	skillsLane  *recordingOperationLimiter
	insightLane *recordingOperationLimiter
}

func newInsightsFixture(t *testing.T, registry *registrySkillsManagement, reader *stubSkillInsightsReader) insightsFixture {
	t.Helper()
	skillsLane := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	insightLane := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	service := NewSkillsService(
		registry,
		stubSkillTargets{targets: testTargets()},
		stubSkillProjects{},
		passthroughGrants{},
		stubSkillsGate{enabled: true},
		OperationBudget{Connection: skillsLane, Organization: skillsLane},
	).WithInsights(reader, OperationBudget{Connection: insightLane, Organization: insightLane})
	service.now = func() time.Time { return testInsightNow }
	return insightsFixture{service: service, registry: registry, reader: reader, skillsLane: skillsLane, insightLane: insightLane}
}

func TestGetSkillInsightsRanksProjectSkillsByEstimatedMinutesSaved(t *testing.T) {
	t.Parallel()

	reader := &stubSkillInsightsReader{rows: testInsightRows(), watermark: testInsightNow.Add(-time.Minute).UnixNano()}
	fixture := newInsightsFixture(t, testInsightRegistry(), reader)

	output, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})
	require.NoError(t, err)

	require.Equal(t, SkillInsightsModeRankSkills, output.Mode)
	require.Equal(t, SkillInsightsSortEstimatedMinutesSaved, output.SortBy)
	require.True(t, output.ScoresAvailable)
	require.False(t, output.Truncated)
	require.Equal(t, testSkillProjectSlug, output.ProjectSlug)
	require.True(t, strings.HasPrefix(output.CostAttribution, "full_session:"), "costs must be labelled as full-session attribution")
	require.True(t, strings.HasPrefix(output.ScoreCoverage, "sampled:"))

	// Scored skills rank by time saved; the unscored skill ranks last even
	// though it was the most expensive and most used.
	require.Equal(t, []string{"verification", "triage", "release-notes"}, insightNames(output.Skills))

	verification := output.Skills[0]
	require.EqualValues(t, 10, verification.Metrics.Activations)
	require.EqualValues(t, 6, verification.Metrics.ActivatedSessions)
	require.InDelta(t, 3.0, verification.Metrics.FullSessionCostUSD, 0)
	require.InDelta(t, 0.5, *verification.Metrics.AverageFullSessionCostUSD, 0)
	require.NotNil(t, verification.Metrics.Efficacy)
	require.EqualValues(t, 3, verification.Metrics.Efficacy.ScoredSessions)
	require.InDelta(t, 0.7, verification.Metrics.Efficacy.AverageScore, 1e-9)
	require.InDelta(t, 40, verification.Metrics.Efficacy.EstimatedMinutesSavedTotal, 0)
	require.InDelta(t, 40.0/3, *verification.Metrics.Efficacy.EstimatedMinutesSavedAverage, 1e-9)
	// The averages divide by the sessions that carried an estimate, and that
	// count travels with them so a caller can tell a three-sample average from
	// a thirty-sample one.
	require.EqualValues(t, 3, verification.Metrics.Efficacy.EstimatedMinutesSavedSamples)
	require.EqualValues(t, 3, verification.Metrics.Efficacy.EstimatedTurnsSavedSamples)
	require.InDelta(t, 8.0/3, *verification.Metrics.Efficacy.EstimatedTurnsSavedAverage, 1e-9)
	require.Equal(t, map[string]uint64{"low": 1, "med": 1, "high": 1}, verification.Metrics.Efficacy.ROIConfidenceCounts)
	require.Equal(t, map[string]uint64{"ignored": 0, "misapplied": 0, "partially_followed": 1, "harmful": 0}, verification.Metrics.Efficacy.FlagCounts)

	// Versions are newest first and carry the registry's creation time.
	require.Len(t, verification.Versions, 2)
	require.Equal(t, testInsightVersionANew, verification.Versions[0].ID)
	require.Equal(t, testInsightVersionNewAt, verification.Versions[0].CreatedAt)
	require.EqualValues(t, 6, verification.Versions[0].Metrics.Activations)
	require.Equal(t, testInsightVersionAOld, verification.Versions[1].ID)

	releaseNotes := output.Skills[2]
	require.Nil(t, releaseNotes.Metrics.Efficacy, "an unscored skill has no efficacy block, not a zero one")
	require.InDelta(t, 12.0, releaseNotes.Metrics.FullSessionCostUSD, 0)

	// The ClickHouse read is scoped to the resolved project and the registry's
	// skill IDs over the default month, as one bucket.
	require.NotNil(t, reader.params)
	require.Equal(t, testPrincipal().OrganizationID, reader.params.OrganizationID)
	require.Equal(t, testInsightProjectID, reader.params.ProjectID)
	require.ElementsMatch(t, []string{testInsightSkillA, testInsightSkillB, testInsightSkillC}, reader.params.SkillIDs)
	require.Nil(t, reader.params.SkillVersionIDs)
	require.True(t, reader.params.IncludeSessionUsage)
	require.Equal(t, testInsightNow.Add(-30*24*time.Hour), reader.params.From)
	require.Equal(t, testInsightNow, reader.params.To)
	require.Equal(t, int64((30*24*time.Hour)/time.Second), reader.params.IntervalSeconds)

	require.Equal(t, DiagnosticWindowLastMonth, output.Envelope.ResolvedWindow.Window)
	require.Equal(t, FreshnessCurrent, output.Envelope.Freshness)
	require.False(t, output.Envelope.NoObservations)

	// Versions were paged once per ranked skill, never for the whole registry.
	require.Equal(t, 1, fixture.registry.listCalls)
	require.Empty(t, fixture.registry.getCalls)
	require.ElementsMatch(t, []string{testInsightSkillA, testInsightSkillB, testInsightSkillC}, fixture.registry.listVersionCalls)
}

func TestGetSkillInsightsHonorsEachSortKey(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		sortBy string
		order  []string
	}{
		{sortBy: SkillInsightsSortEfficacy, order: []string{"triage", "verification", "release-notes"}},
		{sortBy: SkillInsightsSortActivations, order: []string{"verification", "release-notes", "triage"}},
		{sortBy: SkillInsightsSortSessionCost, order: []string{"release-notes", "verification", "triage"}},
		{sortBy: " Estimated_Minutes_Saved ", order: []string{"verification", "triage", "release-notes"}},
	} {
		fixture := newInsightsFixture(t, testInsightRegistry(), &stubSkillInsightsReader{rows: testInsightRows()})

		output, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug, SortBy: test.sortBy})

		require.NoError(t, err, test.sortBy)
		require.Equal(t, test.order, insightNames(output.Skills), test.sortBy)
		require.Equal(t, strings.ToLower(strings.TrimSpace(test.sortBy)), output.SortBy)
	}
}

// An unscored skill must never outrank a scored one on an efficacy key, even a
// scored skill whose average score is zero: unknown is not zero.
func TestRankSkillInsightsPlacesUnscoredSkillsBelowZeroScores(t *testing.T) {
	t.Parallel()

	skills := map[string]*types.Skill{
		testInsightSkillA: insightSkill(testInsightSkillA, "alpha"),
		testInsightSkillB: insightSkill(testInsightSkillB, "beta"),
	}
	rows := []telemetryrepo.SkillInsightBucket{
		{SkillID: testInsightSkillA, SkillVersionID: testInsightVersionANew, ActivationCount: 1, ActivatedSessions: 1, ScoredSessions: 0},
		{SkillID: testInsightSkillB, SkillVersionID: testInsightVersionB, ActivationCount: 1, ActivatedSessions: 1, ScoredSessions: 1, ScoreSum: 0},
	}

	ranked, scoresAvailable := rankSkillInsights(rows, skills, nil, SkillInsightsSortEfficacy)

	require.True(t, scoresAvailable)
	require.Equal(t, []string{"beta", "alpha"}, insightNames(ranked))
}

func TestGetSkillInsightsComparesOneSkillsVersions(t *testing.T) {
	t.Parallel()

	rows := testInsightRows()[1:2] // only the newest version of skill A was active
	reader := &stubSkillInsightsReader{rows: rows, watermark: testInsightNow.UnixNano()}
	fixture := newInsightsFixture(t, testInsightRegistry(), reader)

	output, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug, SkillID: " " + testInsightSkillA + " "})
	require.NoError(t, err)

	require.Equal(t, SkillInsightsModeCompareVersions, output.Mode)
	require.Len(t, output.Skills, 1)
	require.Equal(t, testInsightSkillA, output.Skills[0].ID)
	require.Equal(t, []string{testInsightSkillA}, reader.params.SkillIDs)

	// Every registry version is present, newest first; the inactive one reads
	// as zero rather than disappearing from the comparison.
	versions := output.Skills[0].Versions
	require.Len(t, versions, 2)
	require.Equal(t, testInsightVersionANew, versions[0].ID)
	require.EqualValues(t, 6, versions[0].Metrics.Activations)
	require.NotNil(t, versions[0].Metrics.Efficacy)
	require.Equal(t, testInsightVersionAOld, versions[1].ID)
	require.Equal(t, testInsightVersionOldAt, versions[1].CreatedAt)
	require.Zero(t, versions[1].Metrics.Activations)
	require.Nil(t, versions[1].Metrics.AverageFullSessionCostUSD)
	require.Nil(t, versions[1].Metrics.Efficacy)

	// Compare mode reads one skill and its versions; it never lists the registry.
	require.Zero(t, fixture.registry.listCalls)
	require.Equal(t, []string{testInsightSkillA}, fixture.registry.getCalls)
	require.Equal(t, []string{testInsightSkillA}, fixture.registry.listVersionCalls)
}

func TestGetSkillInsightsReportsAnEmptyWindowAsNoObservations(t *testing.T) {
	t.Parallel()

	reader := &stubSkillInsightsReader{rows: nil, watermark: 0}
	fixture := newInsightsFixture(t, testInsightRegistry(), reader)

	output, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})
	require.NoError(t, err)

	require.Empty(t, output.Skills)
	require.False(t, output.ScoresAvailable)
	require.True(t, output.Envelope.NoObservations)
	require.Equal(t, FreshnessUnavailable, output.Envelope.Freshness)
	require.Empty(t, output.Envelope.DataThrough)
}

func TestGetSkillInsightsRefusesInvalidInputBeforeReadingAnything(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		input GetSkillInsightsInput
	}{
		{name: "missing project", input: GetSkillInsightsInput{ProjectSlug: " "}},
		{name: "skill id that is not a uuid", input: GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug, SkillID: "verification"}},
		{name: "unknown sort key", input: GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug, SortBy: "popularity"}},
		{name: "unknown window", input: GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug, Window: "2h"}},
		{name: "window longer than a month", input: GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug, Window: "90d"}},
	} {
		reader := &stubSkillInsightsReader{rows: testInsightRows()}
		fixture := newInsightsFixture(t, testInsightRegistry(), reader)

		_, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), test.input)

		require.ErrorIs(t, err, ErrRegistrationInvalid, test.name)
		require.Nil(t, reader.params, "%s: ClickHouse must not be read for a refused request", test.name)
		require.Zero(t, fixture.registry.listCalls, test.name)
	}
}

func TestGetSkillInsightsWindowDefaultsToAMonthAndAcceptsShorterOnes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		window   string
		resolved DiagnosticWindow
		duration time.Duration
	}{
		{window: "", resolved: DiagnosticWindowLastMonth, duration: 30 * 24 * time.Hour},
		{window: "30d", resolved: DiagnosticWindowLastMonth, duration: 30 * 24 * time.Hour},
		{window: " 7D ", resolved: DiagnosticWindowLastWeek, duration: 7 * 24 * time.Hour},
		{window: "24h", resolved: DiagnosticWindowLastDay, duration: 24 * time.Hour},
		{window: "1h", resolved: DiagnosticWindowLastHour, duration: time.Hour},
	} {
		reader := &stubSkillInsightsReader{rows: testInsightRows()}
		fixture := newInsightsFixture(t, testInsightRegistry(), reader)

		output, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug, Window: test.window})

		require.NoError(t, err, test.window)
		require.Equal(t, test.resolved, output.Envelope.ResolvedWindow.Window, test.window)
		require.Equal(t, testInsightNow.Add(-test.duration), reader.params.From, test.window)
		require.Equal(t, testInsightNow, reader.params.To, test.window)
		require.Equal(t, int64(test.duration/time.Second), reader.params.IntervalSeconds, test.window)
		require.Equal(t, testInsightNow.Add(-test.duration).Format(time.RFC3339), output.Envelope.ResolvedWindow.From, test.window)
	}
}

func TestGetSkillInsightsClampsTheLimitAndReportsTruncation(t *testing.T) {
	t.Parallel()

	registry := &registrySkillsManagement{skills: nil, versionsBySkill: map[string]*genskills.ListSkillVersionsResult{}}
	var rows []telemetryrepo.SkillInsightBucket
	for i := range 25 {
		id := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
		registry.skills = append(registry.skills, insightSkill(id, fmt.Sprintf("skill-%02d", i)))
		rows = append(rows, telemetryrepo.SkillInsightBucket{SkillID: id, SkillVersionID: id, ActivationCount: uint64(i + 1), ActivatedSessions: 1})
	}

	for _, test := range []struct {
		limit int
		want  int
	}{
		{limit: 0, want: skillInsightsDefaultLimit},
		{limit: -3, want: skillInsightsDefaultLimit},
		{limit: 5, want: 5},
		{limit: 99, want: skillInsightsMaxLimit},
	} {
		fixture := newInsightsFixture(t, registry, &stubSkillInsightsReader{rows: rows})

		output, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug, Limit: test.limit, SortBy: SkillInsightsSortActivations})

		require.NoError(t, err)
		require.Len(t, output.Skills, test.want, "limit %d", test.limit)
		require.True(t, output.Truncated, "limit %d cut the ranking", test.limit)
		require.Equal(t, "skill-24", output.Skills[0].Name, "ranking runs before the cut")
	}

	// A registry page with more skills behind it is truncation too, even when
	// the ranking itself fits.
	registry.skills = registry.skills[:3]
	registry.moreSkills = true
	fixture := newInsightsFixture(t, registry, &stubSkillInsightsReader{rows: rows[:3]})
	output, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})
	require.NoError(t, err)
	require.Len(t, output.Skills, 3)
	require.True(t, output.Truncated)
}

// Insights are a ClickHouse aggregate metered on the diagnostics lane. Neither
// the skills authoring allowance nor the project resolver is touched when that
// lane refuses, and a permitted read leaves the authoring allowance untouched.
func TestGetSkillInsightsIsMeteredOnTheDiagnosticsLaneNotTheSkillsAllowance(t *testing.T) {
	t.Parallel()

	reader := &stubSkillInsightsReader{rows: testInsightRows()}
	fixture := newInsightsFixture(t, testInsightRegistry(), reader)
	fixture.insightLane.result = ratelimit.Result{Allowed: false}
	projects := &countingSkillProjects{}
	fixture.service.projects = projects

	_, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})

	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Nil(t, reader.params)
	require.Zero(t, projects.calls)
	require.Empty(t, fixture.skillsLane.keys)
	require.NotEmpty(t, fixture.insightLane.keys)

	fixture.insightLane.result = ratelimit.Result{Allowed: true}
	_, err = fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})
	require.NoError(t, err)
	require.Empty(t, fixture.skillsLane.keys, "a permitted insights read spends nothing from the authoring allowance")
}

func TestGetSkillInsightsRefusesWhenSkillsAreOffOrTheReaderIsAbsent(t *testing.T) {
	t.Parallel()

	off := newInsightsFixture(t, testInsightRegistry(), &stubSkillInsightsReader{})
	off.service.gate = stubSkillsGate{enabled: false}
	_, err := off.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})
	require.ErrorIs(t, err, ErrSkillsUnavailable)

	withoutReader := testSkillsService(t, &recordingSkillsManagement{skill: testSkill()})
	_, err = withoutReader.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})
	require.ErrorIs(t, err, ErrSkillsUnavailable)

	var absent *SkillsService
	_, err = absent.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})
	require.ErrorIs(t, err, ErrSkillsUnavailable)
	require.Nil(t, absent.WithInsights(&stubSkillInsightsReader{}, OperationBudget{Connection: nil, Organization: nil}))
}

func TestGetSkillInsightsWrapsReaderFailuresAsErrorsNotRefusals(t *testing.T) {
	t.Parallel()

	fixture := newInsightsFixture(t, testInsightRegistry(), &stubSkillInsightsReader{err: fmt.Errorf("clickhouse unreachable")})

	_, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})

	require.Error(t, err)
	require.Contains(t, err.Error(), "read skill insights")
	_, isRefusal := skillsToolResult(err)
	require.False(t, isRefusal, "an internal failure is a transport error, not a refusal the model should act on")
}

func TestGetSkillInsightsOutputProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	fixture := newInsightsFixture(t, testInsightRegistry(), &stubSkillInsightsReader{rows: testInsightRows(), watermark: testInsightNow.UnixNano()})
	output, err := fixture.service.GetSkillInsights(t.Context(), testPrincipal(), GetSkillInsightsInput{ProjectSlug: testSkillProjectSlug})
	require.NoError(t, err)

	// Metrics appear once per skill and once per version, so the key walk
	// repeats them; the allowlist is about which keys exist, not how often.
	keys := decodeKeys(t, output)
	slices.Sort(keys)
	require.ElementsMatch(t, []string{
		"project_slug",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"mode", "sort_by", "scores_available", "cost_attribution", "score_coverage", "truncated",
		"skills", "id", "name", "display_name", "metrics", "versions", "created_at",
		"activations", "activated_sessions", "full_session_cost_usd", "average_full_session_cost_usd",
		"efficacy", "scored_sessions", "average_score",
		"estimated_turns_saved_total", "estimated_turns_saved_average", "estimated_turns_saved_samples",
		"estimated_minutes_saved_total", "estimated_minutes_saved_average", "estimated_minutes_saved_samples",
		"roi_confidence_counts", "low", "med", "high",
		"flag_counts", "ignored", "misapplied", "partially_followed", "harmful",
	}, slices.Compact(keys))
}

// The reader flips what get_skill_insights answers, never whether it exists or
// who may call it, so a client sees one stable contract across the rollout.
func TestSkillInsightsToolIsDeclaredWithAndWithoutItsReader(t *testing.T) {
	t.Parallel()

	live := newInsightsFixture(t, testInsightRegistry(), &stubSkillInsightsReader{rows: testInsightRows()}).service
	for _, test := range []struct {
		name    string
		service *SkillsService
		served  bool
	}{
		{name: "reader attached", service: live, served: true},
		{name: "no reader", service: testSkillsService(t, &recordingSkillsManagement{skill: testSkill()}), served: false},
		{name: "skills absent", service: nil, served: false},
	} {
		_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, test.service, nil, nil, nil, nil, CatalogDescriptor{})
		descriptor := descriptorByName(t, registrar, "get_skill_insights")

		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization, test.name)
		require.Equal(t, bothAudiences, descriptor.Meta.Audiences, test.name)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope, test.name)
		require.NotNil(t, descriptor.Annotations, test.name)
		require.True(t, descriptor.Annotations.ReadOnlyHint, test.name)

		result, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(`{"project_slug":"`+testSkillProjectSlug+`"}`))
		if test.served {
			require.Contains(t, string(descriptor.InputSchema), `"project_slug"`, test.name)
			require.NoError(t, err, test.name)
			output, ok := result.(GetSkillInsightsOutput)
			require.True(t, ok, test.name)
			require.Equal(t, SkillInsightsModeRankSkills, output.Mode, test.name)
			continue
		}
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal, test.name)
		var body featureUnavailableResult
		require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &body), test.name)
		require.Equal(t, unavailableCode, body.Code, test.name)
		require.Equal(t, "skill_insights", body.Feature, test.name)
	}
}

// Input the service refuses reaches the model as a structured refusal with a
// code, not as a transport error it cannot act on.
func TestSkillInsightsToolReturnsStructuredRefusals(t *testing.T) {
	t.Parallel()

	live := newInsightsFixture(t, testInsightRegistry(), &stubSkillInsightsReader{rows: testInsightRows()}).service
	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, live, nil, nil, nil, nil, CatalogDescriptor{})
	descriptor := descriptorByName(t, registrar, "get_skill_insights")

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(`{"project_slug":"`+testSkillProjectSlug+`","window":"90d"}`))

	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	var body operationBudgetResult
	require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &body))
	require.Equal(t, "invalid_request", body.Code)
}

func insightNames(skills []SkillInsight) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	return names
}
