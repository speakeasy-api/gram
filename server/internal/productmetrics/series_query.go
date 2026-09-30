package productmetrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/ext"
	"github.com/Masterminds/squirrel"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// QueryMaxSeries bounds the matched-series join/set, independently of output groups.
	QueryMaxSeries = 50000
	// QueryMaxActiveSeries bounds discovery before attribute filtering.
	QueryMaxActiveSeries = 250000
	// QueryMaxSeriesPoints bounds the conservative matched-series/output-bucket product.
	QueryMaxSeriesPoints = 10000000
	// QueryMaxIdentityBytes bounds individual ID sets, joins and distinct discovery.
	QueryMaxIdentityBytes = 32 << 20 // 32 MiB
)

type querySegment struct {
	table string
	start time.Time
	end   time.Time
}

// planSegments covers the exact range with disjoint tiers. Only resolutions that
// divide the requested epoch-aligned interval can replace minute observations.
func planSegments(q Query) []querySegment {
	prefix := "product_metric_sums_"
	if q.Definition.Instrument == Histogram {
		prefix = "product_metric_histograms_"
	}
	var result []querySegment
	var visit func(time.Time, time.Time, int)
	widths := []time.Duration{24 * time.Hour, time.Hour, time.Minute}
	names := []string{"1d", "1h", "1m"}
	visit = func(start, end time.Time, tier int) {
		if !start.Before(end) {
			return
		}
		if tier == 2 {
			result = append(result, querySegment{table: prefix + names[tier], start: start, end: end})
			return
		}
		width := widths[tier]
		if q.Interval%width != 0 {
			visit(start, end, tier+1)
			return
		}
		lo, hi := start.Truncate(width), end.Truncate(width)
		if lo.Before(start) {
			lo = lo.Add(width)
		}
		if !lo.Before(hi) {
			visit(start, end, tier+1)
			return
		}
		visit(start, lo, tier+1)
		result = append(result, querySegment{table: prefix + names[tier], start: lo, end: hi})
		visit(hi, end, tier+1)
	}
	visit(q.Start.UTC(), q.End.UTC(), 0)
	return result
}

func descriptorScope(q Query) squirrel.Eq {
	return squirrel.Eq{"organization_id": q.Tenant.OrganizationID, "project_id": q.Tenant.ProjectID, "metric_name": q.Definition.Name, "scope_name": q.Definition.ScopeName, "scope_version": q.Definition.ScopeVersion, "unit": q.Definition.Unit}
}

func rollupSource(q Query, columns string, ids []string) (string, []any, error) {
	parts := make([]string, 0, 5)
	args := make([]any, 0)
	for _, segment := range planSegments(q) {
		b := squirrel.Select(columns).From(segment.table).Where(descriptorScope(q)).Where("bucket >= ? AND bucket < ?", segment.start, segment.end)
		if ids != nil {
			b = b.Where("series_id IN (SELECT series_id FROM metric_series_ids)")
		}
		sql, values, err := b.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("build metric tier source: %w", err)
		}
		parts = append(parts, sql)
		args = append(args, values...)
	}
	return strings.Join(parts, " UNION ALL "), args, nil
}

func withSeriesIDs(ctx context.Context, ids []string) (context.Context, error) {
	table, err := ext.NewTable("metric_series_ids", ext.Column("series_id", "String"))
	if err != nil {
		return nil, fmt.Errorf("create metric series set: %w", err)
	}
	for _, id := range ids {
		if err := table.Append(id); err != nil {
			return nil, fmt.Errorf("append metric series ID: %w", err)
		}
	}
	return clickhouse.Context(ctx, clickhouse.WithExternalTable(table)), nil
}

func seriesQueryContext(ctx context.Context, limit int) context.Context {
	settings := boundedQuerySettings(ctx)
	settings["max_result_rows"] = limit
	settings["max_rows_to_group_by"] = limit
	return clickhouse.Context(ctx, clickhouse.WithQueryID(""), clickhouse.WithSettings(settings))
}

func (r *Repository) resolveSeries(ctx context.Context, q Query) ([]string, error) {
	source, args, err := rollupSource(q, "series_id", nil)
	if err != nil {
		return nil, err
	}
	rows, err := r.conn.Query(seriesQueryContext(ctx, QueryMaxActiveSeries), "SELECT DISTINCT series_id FROM ("+source+")", args...)
	if err != nil {
		return nil, fmt.Errorf("discover active metric series: %w", err)
	}
	active := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			o11y.NoLogDefer(rows.Close)
			return nil, fmt.Errorf("scan active metric series: %w", err)
		}
		active = append(active, id)
		if len(active) > QueryMaxActiveSeries {
			o11y.NoLogDefer(rows.Close)
			return nil, fmt.Errorf("active metric series exceed query budget")
		}
	}
	err = rows.Err()
	o11y.NoLogDefer(rows.Close)
	if err != nil {
		return nil, fmt.Errorf("read active metric series: %w", err)
	}
	if len(active) == 0 {
		return []string{}, nil
	}
	ctx, err = withSeriesIDs(ctx, active)
	if err != nil {
		return nil, err
	}
	base := squirrel.Select().From("product_metric_series").Where(descriptorScope(q)).Where(squirrel.Eq{"instrument": string(q.Definition.Instrument)}).Where("series_id IN (SELECT series_id FROM metric_series_ids)")
	// Check every active identity before applying filters: absent/colliding metadata
	// must fail closed, not hide observations from an attribute-filtered query.
	check, values, err := base.Columns("series_id", "uniqExact(tuple(number_kind, resource_attributes, scope_attributes, point_attributes)) AS identities").GroupBy("series_id").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build catalogue integrity check: %w", err)
	}
	var count, variants uint64
	if err := r.conn.QueryRow(seriesQueryContext(ctx, QueryMaxActiveSeries), "SELECT count(), max(identities) FROM ("+check+")", values...).Scan(&count, &variants); err != nil {
		return nil, fmt.Errorf("check metric catalogue: %w", err)
	}
	if count != uint64(len(active)) || variants != 1 {
		return nil, fmt.Errorf("metric catalogue incomplete or conflicting: repair from retained raw contributions")
	}
	b, err := applyAttributeFilters(base.Columns("series_id").GroupBy("series_id"), q.Filters)
	if err != nil {
		return nil, err
	}
	sql, values, err := b.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build metric dimension filter: %w", err)
	}
	rows, err = r.conn.Query(seriesQueryContext(ctx, QueryMaxSeries), sql, values...)
	if err != nil {
		return nil, fmt.Errorf("resolve metric dimensions: %w", err)
	}
	defer o11y.NoLogDefer(rows.Close)
	matched := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan matched metric series: %w", err)
		}
		matched = append(matched, id)
		if len(matched) > QueryMaxSeries {
			return nil, fmt.Errorf("matched metric series exceed query budget")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read matched metric series: %w", err)
	}
	seconds := int64(q.Interval / time.Second)
	buckets := (q.End.Unix()-1)/seconds - q.Start.Unix()/seconds + 1
	if int64(len(matched))*buckets > QueryMaxSeriesPoints {
		return nil, fmt.Errorf("metric series-point budget exceeded: use a coarser interval or narrower filters")
	}
	return matched, nil
}
