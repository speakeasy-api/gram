package telemetry

import (
	"strconv"
	"testing"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

const hourNanos = int64(3600) * 1_000_000_000

func tableRow(group string, cost float64) repo.AttributeMetricsRow {
	return repo.AttributeMetricsRow{GroupValue: group, TotalCost: cost}
}

func tsPoint(group string, bucket int64, cost float64) repo.AttributeMetricsTimePoint {
	return repo.AttributeMetricsTimePoint{GroupValue: group, BucketTimeUnixNano: bucket, TotalCost: cost}
}

// seriesByGroup indexes a result's timeseries by group value for assertions.
func seriesCostByBucket(t *testing.T, series []*telem_gen.QuerySeries, group string) map[string]float64 {
	t.Helper()
	for _, s := range series {
		if s.GroupValue == group {
			out := make(map[string]float64, len(s.Points))
			for _, p := range s.Points {
				out[p.BucketTimeUnixNano] = p.Measures.TotalCost
			}
			return out
		}
	}
	t.Fatalf("series for group %q not found", group)
	return nil
}

func TestBuildQueryResult_TopNAndOtherRollup(t *testing.T) {
	t.Parallel()

	// Two buckets: [0, hourNanos].
	timeStart := int64(0)
	timeEnd := hourNanos

	// Three groups ranked A > B > C; top_n = 2 keeps A, B and folds C into Other.
	tableRows := []repo.AttributeMetricsRow{
		tableRow("A", 5),
		tableRow("B", 3),
		tableRow("C", 1),
	}
	tsRows := []repo.AttributeMetricsTimePoint{
		tsPoint("A", 0, 2),
		tsPoint("A", hourNanos, 3),
		tsPoint("C", 0, 1),
	}

	res := buildQueryResult("department_name", 3600, timeStart, timeEnd, 2, tableRows, tsRows)

	require.Equal(t, "department_name", res.GroupBy)
	require.Equal(t, int64(3600), res.IntervalSeconds)

	// Table: A, B, then Other (= C).
	require.Len(t, res.Table, 3)
	require.Equal(t, "A", res.Table[0].GroupValue)
	require.InDelta(t, 5.0, res.Table[0].Measures.TotalCost, 1e-9)
	require.Equal(t, "B", res.Table[1].GroupValue)
	require.Equal(t, otherGroupLabel, res.Table[2].GroupValue)
	require.InDelta(t, 1.0, res.Table[2].Measures.TotalCost, 1e-9)

	// Timeseries: one series per table row, each gap-filled to 2 buckets.
	require.Len(t, res.Timeseries, 3)
	for _, s := range res.Timeseries {
		require.Len(t, s.Points, 2, "series %q should be gap-filled to 2 buckets", s.GroupValue)
	}

	b0 := strconv.FormatInt(0, 10)
	b1 := strconv.FormatInt(hourNanos, 10)

	a := seriesCostByBucket(t, res.Timeseries, "A")
	require.InDelta(t, 2.0, a[b0], 1e-9)
	require.InDelta(t, 3.0, a[b1], 1e-9)

	b := seriesCostByBucket(t, res.Timeseries, "B")
	require.InDelta(t, 0.0, b[b0], 1e-9, "B has no points and should be zero-filled")
	require.InDelta(t, 0.0, b[b1], 1e-9)

	other := seriesCostByBucket(t, res.Timeseries, otherGroupLabel)
	require.InDelta(t, 1.0, other[b0], 1e-9, "C's spend should roll into Other")
	require.InDelta(t, 0.0, other[b1], 1e-9)
}

func TestBuildQueryResult_DimensionValuesPassThroughAndOtherUnion(t *testing.T) {
	t.Parallel()

	// Group by department; top_n=1 keeps Eng and folds Sales+Ops into Other.
	tableRows := []repo.AttributeMetricsRow{
		{GroupValue: "Eng", TotalCost: 5, DimensionValues: map[string][]string{
			"email":     {"a@x.com", "b@x.com"},
			"job_title": {"swe"},
		}},
		{GroupValue: "Sales", TotalCost: 3, DimensionValues: map[string][]string{
			"email":     {"c@x.com"},
			"job_title": {"ae"},
		}},
		{GroupValue: "Ops", TotalCost: 1, DimensionValues: map[string][]string{
			"email":     {"c@x.com", "d@x.com"}, // c@x.com overlaps Sales
			"job_title": {"ops"},
		}},
	}

	res := buildQueryResult("department_name", 3600, 0, hourNanos, 1, tableRows, nil)

	require.Len(t, res.Table, 2)

	// Kept row passes its dimension values through unchanged.
	require.Equal(t, "Eng", res.Table[0].GroupValue)
	require.Equal(t, []string{"a@x.com", "b@x.com"}, res.Table[0].DimensionValues["email"])
	require.Equal(t, []string{"swe"}, res.Table[0].DimensionValues["job_title"])

	// Other unions the folded groups' values (deduped + sorted).
	other := res.Table[1]
	require.Equal(t, otherGroupLabel, other.GroupValue)
	require.Equal(t, []string{"c@x.com", "d@x.com"}, other.DimensionValues["email"])
	require.Equal(t, []string{"ae", "ops"}, other.DimensionValues["job_title"])
}

func TestBuildQueryResult_DimensionValuesNeverNil(t *testing.T) {
	t.Parallel()

	// Rows without dimension values must still yield a non-nil map (required field).
	res := buildQueryResult("model", 3600, 0, hourNanos, 10, []repo.AttributeMetricsRow{tableRow("A", 5)}, nil)
	require.NotNil(t, res.Table[0].DimensionValues)
}

func TestBuildQueryResult_NoGroupBySingleSeries(t *testing.T) {
	t.Parallel()

	tableRows := []repo.AttributeMetricsRow{tableRow("", 10)}
	tsRows := []repo.AttributeMetricsTimePoint{tsPoint("", 0, 4)}

	res := buildQueryResult("", 3600, 0, hourNanos, 10, tableRows, tsRows)

	require.Empty(t, res.GroupBy)
	require.Len(t, res.Table, 1)
	require.Empty(t, res.Table[0].GroupValue)
	require.InDelta(t, 10.0, res.Table[0].Measures.TotalCost, 1e-9)

	require.Len(t, res.Timeseries, 1)
	require.Empty(t, res.Timeseries[0].GroupValue)
	require.Len(t, res.Timeseries[0].Points, 2)

	costs := seriesCostByBucket(t, res.Timeseries, "")
	require.InDelta(t, 4.0, costs[strconv.FormatInt(0, 10)], 1e-9)
	require.InDelta(t, 0.0, costs[strconv.FormatInt(hourNanos, 10)], 1e-9)
}

func TestBuildQueryResult_EmptyNoGroupStillEmitsZeroSeries(t *testing.T) {
	t.Parallel()

	res := buildQueryResult("", 3600, 0, hourNanos, 10, nil, nil)

	require.Empty(t, res.Table)
	// No group_by always yields a single zero-filled series for the chart.
	require.Len(t, res.Timeseries, 1)
	require.Empty(t, res.Timeseries[0].GroupValue)
	require.Len(t, res.Timeseries[0].Points, 2)
	require.InDelta(t, 0.0, res.Timeseries[0].Points[0].Measures.TotalCost, 1e-9)
}

func TestBuildQueryResult_NoOtherWhenWithinTopN(t *testing.T) {
	t.Parallel()

	tableRows := []repo.AttributeMetricsRow{tableRow("A", 5), tableRow("B", 3)}
	res := buildQueryResult("model", 3600, 0, hourNanos, 10, tableRows, nil)

	require.Len(t, res.Table, 2)
	for _, r := range res.Table {
		require.NotEqual(t, otherGroupLabel, r.GroupValue)
	}
	require.Len(t, res.Timeseries, 2)
}

func TestBucketStarts(t *testing.T) {
	t.Parallel()

	// 3 hourly buckets across a 2h05m span (end mid-bucket still included).
	buckets := bucketStarts(0, 2*hourNanos+5, 3600)
	require.Equal(t, []int64{0, hourNanos, 2 * hourNanos}, buckets)

	// Daily interval aligns to day boundaries.
	day := int64(86400) * 1_000_000_000
	buckets = bucketStarts(day+10, day+20, 86400)
	require.Equal(t, []int64{day}, buckets)
}
