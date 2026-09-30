package productmetrics

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestSeriesPointBudget(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	now := time.Now().UTC()
	r := NewRepository(conn, func() time.Time { return now })
	c := synthetic(Counter)
	c.EventTime = now.Truncate(time.Minute).Add(-time.Minute)
	rows := make([]Contribution, 0, 100)
	for i := range 100 {
		row := c
		row.ID = fmt.Sprint(i)
		row.PointAttributes = []attribute.KeyValue{attribute.Int64("series", int64(i))}
		rows = append(rows, row)
	}
	require.NoError(t, r.Insert(t.Context(), rows))
	q := Query{Tenant: c.Tenant, Definition: c.Definition, Start: EarliestBucket(now), End: now.Truncate(time.Minute), Interval: time.Minute}
	_, err := r.Query(t.Context(), q)
	require.ErrorContains(t, err, "series-point budget exceeded")
	q.Interval = 24 * time.Hour
	points, err := r.Query(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, points, 1)
	require.Equal(t, uint64(100), points[0].Count)
}

func TestTierPlan(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 1, 0, 17, 0, 0, time.UTC)
	q := Query{Definition: Definition{Instrument: Counter}, Start: start, End: start.Add(89*24*time.Hour + 13*time.Hour + 13*time.Minute), Interval: 24 * time.Hour}
	segments := planSegments(q)
	require.Len(t, segments, 5)
	require.Equal(t, "product_metric_sums_1m", segments[0].table)
	require.Equal(t, "product_metric_sums_1h", segments[1].table)
	require.Equal(t, "product_metric_sums_1d", segments[2].table)
	require.Equal(t, "product_metric_sums_1h", segments[3].table)
	require.Equal(t, "product_metric_sums_1m", segments[4].table)
	position := q.Start
	for _, s := range segments {
		require.Equal(t, position, s.start)
		require.True(t, s.end.After(s.start))
		position = s.end
	}
	require.Equal(t, q.End, position)
	q.Interval = 7 * time.Minute
	require.Len(t, planSegments(q), 1)
	require.Equal(t, "product_metric_sums_1m", planSegments(q)[0].table)
}

func TestTierQueriesPreservePartialEdgesAndLateArrivals(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	now := time.Now().UTC()
	r := NewRepository(conn, func() time.Time { return now })
	start := now.Truncate(24 * time.Hour).Add(-88*24*time.Hour + 17*time.Minute)
	end := now.Truncate(24 * time.Hour).Add(-24*time.Hour + 43*time.Minute)
	c := synthetic(Histogram)
	c.PointAttributes = []attribute.KeyValue{attribute.String("region", "east")}
	times := []time.Time{start.Add(-time.Minute), start, start.Add(time.Hour), start.Add(24 * time.Hour), end.Add(-time.Minute), end}
	for i, ts := range times {
		row := c
		row.ID = ts.String()
		row.EventTime = ts
		row.Value = Integer(int64(i + 1))
		require.NoError(t, r.Insert(t.Context(), []Contribution{row}))
	}
	q := Query{Tenant: c.Tenant, Definition: c.Definition, Start: start, End: end, Interval: 24 * time.Hour, Filters: []Filter{{Dimension: Dimension{Namespace: Point, Key: "region"}, Value: attribute.StringValue("east")}}, GroupBy: []Dimension{{Namespace: Point, Key: "region"}}}
	check := func(wantCount uint64, wantSum int64) {
		points, err := r.Query(t.Context(), q)
		require.NoError(t, err)
		var count uint64
		sum := new(big.Int)
		for _, p := range points {
			count += p.Count
			sum.Add(sum, p.IntegerSum)
			require.Equal(t, `"east"`, string(p.Groups[0].Value))
		}
		require.Equal(t, wantCount, count)
		require.Equal(t, big.NewInt(wantSum), sum)
		require.True(t, points[0].Partial)
		require.True(t, points[len(points)-1].Partial)
	}
	check(4, 14)
	c.EventTime = start.Add(40 * 24 * time.Hour)
	c.ID = "late"
	c.Value = Integer(10)
	require.NoError(t, r.Insert(t.Context(), []Contribution{c, c}))
	check(6, 34)
	for _, suffix := range []string{"1m", "1h", "1d"} {
		require.NoError(t, conn.Exec(t.Context(), "SYSTEM START MERGES product_metric_histograms_"+suffix))
		require.NoError(t, conn.Exec(t.Context(), "OPTIMIZE TABLE product_metric_histograms_"+suffix+" FINAL"))
	}
	check(6, 34)
}

func TestQueryRejectsMissingCatalogue(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	now := time.Now().UTC()
	r := NewRepository(conn, func() time.Time { return now })
	c := synthetic(Counter)
	c.EventTime = now.Truncate(time.Minute).Add(-time.Minute)
	require.NoError(t, r.Insert(t.Context(), []Contribution{c}))
	require.NoError(t, conn.Exec(t.Context(), "TRUNCATE TABLE product_metric_series"))
	_, err := r.Query(t.Context(), Query{Tenant: c.Tenant, Definition: c.Definition, Start: c.EventTime, End: c.EventTime.Add(time.Minute), Interval: time.Minute, Filters: []Filter{{Dimension: Dimension{Namespace: Point, Key: "absent"}, Value: attribute.StringValue("x")}}})
	require.ErrorContains(t, err, "catalogue incomplete or conflicting")
}

func TestQueryRejectsConflictingCatalogue(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	now := time.Now().UTC()
	r := NewRepository(conn, func() time.Time { return now })
	c := synthetic(Counter)
	c.EventTime = now.Truncate(time.Minute).Add(-time.Minute)
	require.NoError(t, r.Insert(t.Context(), []Contribution{c}))
	// Deliberately corrupt metadata under the same digest. Both identities survive
	// catalogue merges, so readers reject rather than choosing an arbitrary winner.
	require.NoError(t, conn.Exec(t.Context(), `INSERT INTO product_metric_series SELECT * REPLACE ([('collision','STRING','"different"')] AS point_attributes) FROM product_metric_series SETTINGS async_insert=0`))
	_, err := r.Query(t.Context(), Query{Tenant: c.Tenant, Definition: c.Definition, Start: c.EventTime, End: c.EventTime.Add(time.Minute), Interval: time.Minute})
	require.ErrorContains(t, err, "catalogue incomplete or conflicting")
}
