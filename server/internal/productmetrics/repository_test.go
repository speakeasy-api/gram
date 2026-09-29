package productmetrics

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestRollupRepository(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	now := time.Now().UTC()
	r := NewRepository(conn, func() time.Time { return now })
	c := synthetic(Counter)
	c.EventTime = now.Truncate(time.Minute).Add(-2 * time.Minute)
	c.ObservedAt = now
	c.Value = Integer(math.MaxInt64)
	c.PointAttributes = []attribute.KeyValue{attribute.Int64("large", math.MaxInt64), attribute.String("quote'", "value'")}
	require.NoError(t, r.Insert(t.Context(), []Contribution{c}))
	require.NoError(t, r.Insert(t.Context(), []Contribution{c}))
	q := Query{Tenant: c.Tenant, Definition: c.Definition, Start: c.EventTime, End: c.EventTime.Add(time.Minute), Interval: time.Minute}
	result, err := r.Query(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "18446744073709551614", result[0].IntegerSum.String())
	require.Equal(t, uint64(2), result[0].Count)
	require.False(t, result[0].Partial)

	q.Filters = []Filter{{Dimension: Dimension{Namespace: Point, Key: "large"}, Value: attribute.Int64Value(math.MaxInt64)}}
	q.GroupBy = []Dimension{{Namespace: Point, Key: "quote'"}, {Namespace: Point, Key: "absent"}}
	result, err = r.Query(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.True(t, result[0].Groups[0].Present)
	require.Equal(t, `"value'"`, string(result[0].Groups[0].Value))
	require.False(t, result[0].Groups[1].Present)
	q.Filters[0].Value = attribute.StringValue("9223372036854775807")
	result, err = r.Query(t.Context(), q)
	require.NoError(t, err)
	require.Empty(t, result)
	q.Filters = nil
	q.GroupBy = nil
	q.Tenant.ProjectID = uuid.New()
	result, err = r.Query(t.Context(), q)
	require.NoError(t, err)
	require.Empty(t, result)
	q.Tenant = c.Tenant
	q.Tenant.OrganizationID = "other-synthetic-org"
	result, err = r.Query(t.Context(), q)
	require.NoError(t, err)
	require.Empty(t, result)

	h := synthetic(Histogram)
	h.Definition.Name = "gram.synthetic.histogram"
	h.EventTime = c.EventTime
	h.ObservedAt = now
	h.Value = Float(10)
	require.NoError(t, r.Insert(t.Context(), []Contribution{h}))
	h.EventTime = h.EventTime.Add(time.Minute)
	h.Value = Float(1)
	for range 9 {
		require.NoError(t, r.Insert(t.Context(), []Contribution{h}))
	}
	q = Query{Tenant: h.Tenant, Definition: h.Definition, Start: c.EventTime, End: now.Truncate(time.Minute), Interval: 24 * time.Hour}
	result, err = r.Query(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, uint64(10), result[0].Count)
	require.Equal(t, "19/10", result[0].Mean().RatString())
	require.Equal(t, Float(1), result[0].Min)
	require.Equal(t, Float(10), result[0].Max)
	require.True(t, result[0].Partial)
	diagnostics, err := r.Diagnostics(t.Context(), q, h.EventTime)
	require.NoError(t, err)
	require.Equal(t, uint64(1), diagnostics.ActiveSeries)
	require.Zero(t, diagnostics.NewSeries)
	require.Equal(t, uint64(10), diagnostics.Contributions)
	require.Len(t, diagnostics.AttributeDiversity, 2)

	h.EventTime = now.Truncate(time.Minute)
	h.Value = Integer(-2)
	require.NoError(t, r.Insert(t.Context(), []Contribution{h}))
	q.Start = h.EventTime
	q.End = h.EventTime.Add(time.Minute)
	q.Interval = time.Minute
	result, err = r.Query(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.True(t, result[0].Partial)
	require.Equal(t, "-2", result[0].IntegerSum.String())

	h.EventTime = EarliestBucket(now)
	require.NoError(t, r.Insert(t.Context(), []Contribution{h}))
	q.Start = h.EventTime
	q.End = h.EventTime.Add(time.Minute)
	result, err = r.Query(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, result, 1)
	h.EventTime = h.EventTime.Add(-time.Nanosecond)
	require.Error(t, r.Insert(t.Context(), []Contribution{h}))

	// MV failures must surface even if the caller enabled ignore-errors. A
	// retry after restoring the target is duplicate-inclusive, not atomic rollback.
	require.NoError(t, conn.Exec(t.Context(), "ALTER TABLE product_metric_sums_1m ADD CONSTRAINT reject_insert CHECK contributions = 0"))
	unsafe := clickhouse.Context(t.Context(), clickhouse.WithAsync(false), clickhouse.WithSettings(clickhouse.Settings{"materialized_views_ignore_errors": 1, "async_insert": 1, "wait_for_async_insert": 0})) //nolint:forbidigo // Regression fixture proves inherited driver-level fire-and-forget cannot bypass durable insertion.
	require.Error(t, r.Insert(unsafe, []Contribution{c}))
	require.NoError(t, conn.Exec(t.Context(), "ALTER TABLE product_metric_sums_1m DROP CONSTRAINT reject_insert"))
	require.NoError(t, r.Insert(t.Context(), []Contribution{c}))

	c.Definition.Name = "gram.synthetic.bounded"
	c.Value = Integer(1)
	for offset := 0; offset <= QueryMaxRows; offset += BatchMaxMessages {
		batch := make([]Contribution, 0, BatchMaxMessages)
		for i := offset; i < offset+BatchMaxMessages && i <= QueryMaxRows; i++ {
			row := c
			row.ID = strconv.Itoa(i)
			row.PointAttributes = []attribute.KeyValue{attribute.String("group", row.ID)}
			batch = append(batch, row)
		}
		require.NoError(t, r.Insert(t.Context(), batch))
	}
	q = Query{Tenant: c.Tenant, Definition: c.Definition, Start: c.EventTime, End: c.EventTime.Add(time.Minute), Interval: time.Minute, GroupBy: []Dimension{{Namespace: Point, Key: "group"}}}
	result, err = r.Query(t.Context(), q)
	require.Error(t, err, "result bounds must fail, not return a truncated aggregate")
	require.Nil(t, result)
}

func TestQueryBounds(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 30, 0, time.UTC)
	c := synthetic(Counter)
	base := Query{Tenant: c.Tenant, Definition: c.Definition, Start: now.Truncate(time.Minute).Add(-time.Hour), End: now.Truncate(time.Minute), Interval: time.Minute}
	cases := []struct {
		name   string
		change func(*Query)
	}{
		{"missing tenant", func(q *Query) { q.Tenant.OrganizationID = "" }},
		{"missing project", func(q *Query) { q.Tenant.ProjectID = uuid.Nil }},
		{"subminute start", func(q *Query) { q.Start = q.Start.Add(time.Second) }},
		{"subminute end", func(q *Query) { q.End = q.End.Add(time.Second) }},
		{"subminute interval", func(q *Query) { q.Interval = time.Second }},
		{"expired", func(q *Query) { q.Start = EarliestBucket(now).Add(-time.Minute) }},
		{"future", func(q *Query) { q.End = now.Truncate(time.Minute).Add(2 * time.Minute) }},
		{"empty", func(q *Query) { q.Start = q.End }},
		{"namespace injection", func(q *Query) { q.GroupBy = []Dimension{{Namespace: "point_attributes) --", Key: "x"}} }},
		{"too many groups", func(q *Query) { q.GroupBy = make([]Dimension, QueryMaxDimensions+1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := base
			tc.change(&q)
			_, _, err := buildQuery(q, now)
			require.Error(t, err)
		})
	}
	sql, args, err := buildQuery(base, now)
	require.NoError(t, err)
	require.NotContains(t, sql, "FINAL")
	require.NotContains(t, sql, "LIMIT")
	require.NotEmpty(t, args)
}
