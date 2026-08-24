package analytics

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var (
	testOrganizationID = uuid.MustParse("10000000-0000-0000-0000-000000000001")
	testProjectIDOne   = uuid.MustParse("20000000-0000-0000-0000-000000000001")
	testProjectIDTwo   = uuid.MustParse("20000000-0000-0000-0000-000000000002")
)

func TestNewScopeRequiresOrganization(t *testing.T) {
	t.Parallel()

	_, err := NewScope(uuid.Nil, testProjectIDOne)
	require.ErrorIs(t, err, ErrInvalidScope)
}

func TestNewScopeRequiresProjects(t *testing.T) {
	t.Parallel()

	_, err := NewScope(testOrganizationID)
	require.ErrorIs(t, err, ErrInvalidScope)
}

func TestNewScopeDeduplicatesProjects(t *testing.T) {
	t.Parallel()

	scope, err := NewScope(testOrganizationID, testProjectIDOne, testProjectIDOne, testProjectIDTwo)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{testProjectIDOne, testProjectIDTwo}, scope.projectIDs)
}

func TestNewTimeRangeRequiresIncreasingBounds(t *testing.T) {
	t.Parallel()

	bound := time.Date(2026, time.August, 24, 10, 0, 0, 0, time.UTC)
	_, err := NewTimeRange(bound, bound)
	require.ErrorIs(t, err, ErrInvalidTimeRange)
}

func TestUsageCatalogMetadataIsStable(t *testing.T) {
	t.Parallel()

	dimensions := UsageDimensions()
	require.Equal(t, []DimensionID{DimensionModel, DimensionProvider, DimensionSource}, []DimensionID{
		dimensions[0].ID,
		dimensions[1].ID,
		dimensions[2].ID,
	})
	require.Equal(t, []Operator{OperatorEquals, OperatorIn}, dimensions[0].Operators)

	measures := UsageMeasures()
	require.Len(t, measures, 7)
	require.Equal(t, MeasureCacheCreationInputTokens, measures[0].ID)
	require.True(t, measures[0].Exact)
	require.True(t, measures[0].Sortable)
}

func TestCatalogIsInternallyConsistent(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateCatalog(usageDataset))
}

func TestPlannerSelectsFreshDailyRollup(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	availability, err := NewRollupAvailability(
		time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	scope, timeRange := testScopeAndRange(t,
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC),
	)

	query := NewUsageQuery(scope, timeRange).
		AtGrain(GrainDay).
		GroupBy(DimensionProvider, DimensionModel).
		Select(MeasureRequests, MeasureTotalTokens, MeasureTotalCost).
		Where(OneOf(DimensionSource, "provider_otel", "provider_api")).
		OrderBy(Descending(MeasureTotalCost)).
		WithLimit(10)

	compiled, err := NewPlanner(now, availability).Compile(query)
	require.NoError(t, err)
	require.Equal(t, PlanUsageDaily, compiled.PlanID)
	require.Contains(t, compiled.SQL, "FROM telemetry_usage_daily")
	require.Contains(t, compiled.SQL, "organization_id = ?")
	require.Contains(t, compiled.SQL, "project_id IN (?,?)")
	require.Contains(t, compiled.SQL, "day >= ? AND day < ?")
	require.Contains(t, compiled.SQL, "GROUP BY time_bucket, provider, model")
	require.Contains(t, compiled.SQL, "ORDER BY total_cost DESC")
	require.Contains(t, compiled.SQL, "LIMIT 10")
	require.Contains(t, compiled.Args, "provider_otel")
	require.Contains(t, compiled.Args, "provider_api")
}

func TestPlannerFallsBackToFactsForUnalignedRange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	availability, err := NewRollupAvailability(
		time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	scope, timeRange := testScopeAndRange(t,
		time.Date(2026, time.August, 23, 10, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC),
	)

	query := NewUsageQuery(scope, timeRange).
		AtGrain(GrainDay).
		GroupBy(DimensionProvider).
		Select(MeasureRequests)

	compiled, err := NewPlanner(now, availability).Compile(query)
	require.NoError(t, err)
	require.Equal(t, PlanUsageFacts, compiled.PlanID)
	require.Contains(t, compiled.SQL, "FROM telemetry_usage_facts FINAL")
	require.Contains(t, compiled.SQL, "event_time >= ? AND event_time < ?")
}

func TestPlannerRejectsRangeOutsideAvailableSources(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	scope, timeRange := testScopeAndRange(t,
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
	)
	query := NewUsageQuery(scope, timeRange).
		AtGrain(GrainDay).
		Select(MeasureRequests)

	_, err := NewPlanner(now, RollupAvailability{}).Compile(query)
	require.ErrorIs(t, err, ErrNoCompatiblePlan)
}

func TestCompilerParameterizesFilterValues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	scope, timeRange := testScopeAndRange(t,
		time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
	)
	dangerousValue := "model') OR 1 = 1 --"
	query := NewUsageQuery(scope, timeRange).
		Select(MeasureRequests).
		Where(Equals(DimensionModel, dangerousValue))

	compiled, err := NewPlanner(now, RollupAvailability{}).Compile(query)
	require.NoError(t, err)
	require.NotContains(t, compiled.SQL, dangerousValue)
	require.Contains(t, compiled.Args, dangerousValue)
}

func TestCompilerRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	scope, timeRange := testScopeAndRange(t,
		time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
	)
	query := NewUsageQuery(scope, timeRange).
		GroupBy(DimensionID("arbitrary_sql")).
		Select(MeasureRequests)

	_, err := NewPlanner(now, RollupAvailability{}).Compile(query)
	require.ErrorIs(t, err, ErrInvalidQuery)
}

func TestCompilerRequiresOrderedMeasureToBeSelected(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	scope, timeRange := testScopeAndRange(t,
		time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
	)
	query := NewUsageQuery(scope, timeRange).
		Select(MeasureRequests).
		OrderBy(Descending(MeasureTotalCost))

	_, err := NewPlanner(now, RollupAvailability{}).Compile(query)
	require.ErrorIs(t, err, ErrInvalidQuery)
}

func TestCompilerRejectsExcessiveLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	scope, timeRange := testScopeAndRange(t,
		time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
	)
	query := NewUsageQuery(scope, timeRange).
		Select(MeasureRequests).
		WithLimit(1001)

	_, err := NewPlanner(now, RollupAvailability{}).Compile(query)
	require.ErrorIs(t, err, ErrInvalidQuery)
}

func testScopeAndRange(t *testing.T, from, to time.Time) (Scope, TimeRange) {
	t.Helper()

	scope, err := NewScope(testOrganizationID, testProjectIDOne, testProjectIDTwo)
	require.NoError(t, err)
	timeRange, err := NewTimeRange(from, to)
	require.NoError(t, err)
	return scope, timeRange
}
