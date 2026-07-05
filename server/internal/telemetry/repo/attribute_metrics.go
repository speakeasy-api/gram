package repo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Masterminds/squirrel"
)

// maxDimensionValues caps the per-dimension distinct-value lists collected for
// each grouped table row, bounding the response payload when a group contains a
// large number of distinct emails, models, etc.
const maxDimensionValues = 1000

// measureAliasPrefix prefixes the merged-measure SELECT aliases. Without it the
// alias (e.g. "total_cost") collides with the underlying AggregateFunction
// state column of the same name, and ClickHouse resolves ORDER BY/expressions
// to that non-comparable state column. The prefix keeps the merged value (a
// plain Float64/Int64/UInt64) distinct and orderable.
const measureAliasPrefix = "m_"

// attributeMeasureSelects reads the aggregate states back out of the
// AggregatingMergeTree. The state functions are the *If variants (see
// attribute_metrics_summaries_mv), so reads must use the matching *IfMerge
// combinators. Aliases are prefixed (see measureAliasPrefix) and match the
// scan struct ch tags below.
var attributeMeasureSelects = []string{
	"sumIfMerge(total_cost) AS m_total_cost",
	"sumIfMerge(total_input_tokens) AS m_total_input_tokens",
	"sumIfMerge(total_output_tokens) AS m_total_output_tokens",
	"sumIfMerge(total_tokens) AS m_total_tokens",
	"sumIfMerge(cache_read_input_tokens) AS m_cache_read_input_tokens",
	"sumIfMerge(cache_creation_input_tokens) AS m_cache_creation_input_tokens",
	"countIfMerge(total_tool_calls) AS m_total_tool_calls",
	"uniqExactIfMerge(total_chats) AS m_total_chats",
}

// attributeMeasureSet is the allowlist of measures available for ranking
// (sort_by). Every measure is always returned regardless of this choice.
var attributeMeasureSet = map[string]bool{
	"total_cost":                  true,
	"total_input_tokens":          true,
	"total_output_tokens":         true,
	"total_tokens":                true,
	"cache_read_input_tokens":     true,
	"cache_creation_input_tokens": true,
	"total_tool_calls":            true,
	"total_chats":                 true,
}

// AttributeMetricsMeasures holds the aggregated measure values for a group or a
// single bucket. It is the accumulation type used by the service layer for
// "Other" rollup and zero-fill; the scan structs below carry the same fields
// flat (clickhouse-go ScanStruct does not recurse into embedded structs).
type AttributeMetricsMeasures struct {
	TotalCost                float64
	TotalInputTokens         int64
	TotalOutputTokens        int64
	TotalTokens              int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	TotalToolCalls           uint64
	TotalChats               uint64
}

// Add accumulates another set of measures into the receiver (for "Other"
// rollup and bucket aggregation).
func (m *AttributeMetricsMeasures) Add(o AttributeMetricsMeasures) {
	m.TotalCost += o.TotalCost
	m.TotalInputTokens += o.TotalInputTokens
	m.TotalOutputTokens += o.TotalOutputTokens
	m.TotalTokens += o.TotalTokens
	m.CacheReadInputTokens += o.CacheReadInputTokens
	m.CacheCreationInputTokens += o.CacheCreationInputTokens
	m.TotalToolCalls += o.TotalToolCalls
	m.TotalChats += o.TotalChats
}

// AttributeMetricsRow is one grouped table row: measures aggregated over the
// whole time range for a single group value.
type AttributeMetricsRow struct {
	GroupValue               string  `ch:"group_value"`
	TotalCost                float64 `ch:"m_total_cost"`
	TotalInputTokens         int64   `ch:"m_total_input_tokens"`
	TotalOutputTokens        int64   `ch:"m_total_output_tokens"`
	TotalTokens              int64   `ch:"m_total_tokens"`
	CacheReadInputTokens     int64   `ch:"m_cache_read_input_tokens"`
	CacheCreationInputTokens int64   `ch:"m_cache_creation_input_tokens"`
	TotalToolCalls           uint64  `ch:"m_total_tool_calls"`
	TotalChats               uint64  `ch:"m_total_chats"`

	// DimensionValues holds, for every allowlisted dimension other than the
	// grouped one, the distinct values observed within this group, keyed by the
	// public dimension identifier (e.g. "email", "job_title", "role").
	DimensionValues map[string][]string `ch:"dimension_values"`
}

// Measures returns the row's measure values as an accumulation struct.
func (r AttributeMetricsRow) Measures() AttributeMetricsMeasures {
	return AttributeMetricsMeasures{
		TotalCost:                r.TotalCost,
		TotalInputTokens:         r.TotalInputTokens,
		TotalOutputTokens:        r.TotalOutputTokens,
		TotalTokens:              r.TotalTokens,
		CacheReadInputTokens:     r.CacheReadInputTokens,
		CacheCreationInputTokens: r.CacheCreationInputTokens,
		TotalToolCalls:           r.TotalToolCalls,
		TotalChats:               r.TotalChats,
	}
}

// AttributeMetricsTimePoint is one (group, bucket) cell of the timeseries. The
// repo returns raw, un-gap-filled cells; the service layer folds groups beyond
// top_n into "Other" and zero-fills missing buckets so the fill logic stays
// consistent with the table's group selection.
type AttributeMetricsTimePoint struct {
	GroupValue               string  `ch:"group_value"`
	BucketTimeUnixNano       int64   `ch:"bucket_time_unix_nano"`
	TotalCost                float64 `ch:"m_total_cost"`
	TotalInputTokens         int64   `ch:"m_total_input_tokens"`
	TotalOutputTokens        int64   `ch:"m_total_output_tokens"`
	TotalTokens              int64   `ch:"m_total_tokens"`
	CacheReadInputTokens     int64   `ch:"m_cache_read_input_tokens"`
	CacheCreationInputTokens int64   `ch:"m_cache_creation_input_tokens"`
	TotalToolCalls           uint64  `ch:"m_total_tool_calls"`
	TotalChats               uint64  `ch:"m_total_chats"`
}

// Measures returns the point's measure values as an accumulation struct.
func (p AttributeMetricsTimePoint) Measures() AttributeMetricsMeasures {
	return AttributeMetricsMeasures{
		TotalCost:                p.TotalCost,
		TotalInputTokens:         p.TotalInputTokens,
		TotalOutputTokens:        p.TotalOutputTokens,
		TotalTokens:              p.TotalTokens,
		CacheReadInputTokens:     p.CacheReadInputTokens,
		CacheCreationInputTokens: p.CacheCreationInputTokens,
		TotalToolCalls:           p.TotalToolCalls,
		TotalChats:               p.TotalChats,
	}
}

// AttributeMetricsFilter is an AND-ed predicate on an allowlisted dimension.
// A row matches if the dimension equals any of Values (for array dimensions,
// if any element is present).
type AttributeMetricsFilter struct {
	Dimension string
	Values    []string
}

// AttributeMetricsQueryParams are shared by the table and timeseries queries.
type AttributeMetricsQueryParams struct {
	ProjectIDs []string // org's projects; query is scoped to these
	TimeStart  int64    // unix nanoseconds, inclusive
	TimeEnd    int64    // unix nanoseconds, inclusive
	GroupBy    string   // dimension key, or "" for a single aggregate group
	SortBy     string   // measure key used for ORDER BY (table only)
	Filters    []AttributeMetricsFilter

	// IntervalSeconds is the timeseries bucket width. The source is bucketed
	// hourly so this is expected to be a multiple of 3600.
	IntervalSeconds int64
}

// attributeGroupValueExpr returns the SQL expression to select/group by for the
// requested dimension, and whether a GROUP BY on it is needed.
func attributeGroupValueExpr(groupBy string) (expr string, grouped bool, err error) {
	if groupBy == "" {
		return "''", false, nil
	}
	dim, ok := attributeDimensionRegistry[groupBy]
	if !ok {
		return "", false, fmt.Errorf("unknown group_by dimension %q", groupBy)
	}
	switch dim.kind {
	case attributeDimArray:
		// arrayJoin attributes spend to each element. Map the empty array to a
		// single empty-string element so rows with no roles/groups are not
		// silently dropped — role-less spend surfaces under the '' group, the
		// same way a missing scalar attribute does.
		return "arrayJoin(if(empty(" + dim.column + "), [''], " + dim.column + "))", true, nil
	case attributeDimProject:
		return "toString(" + dim.column + ")", true, nil
	case attributeDimScalar:
		return dim.column, true, nil
	default:
		return "", false, fmt.Errorf("unhandled dimension kind for %q", groupBy)
	}
}

// attributeDimensionValuesExpr builds a ClickHouse map() expression that
// collects, per group, the distinct values of every allowlisted dimension
// except the one being grouped on. The result is a Map(String, Array(String))
// keyed by the public dimension identifier, scanned into
// AttributeMetricsRow.DimensionValues. Keys are sorted for deterministic SQL.
// Dimension keys come from the allowlist (never client input) so inlining the
// string literals is safe.
func attributeDimensionValuesExpr(groupBy string) string {
	keys := make([]string, 0, len(attributeDimensionRegistry))
	for k := range attributeDimensionRegistry {
		if k == groupBy {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	capStr := strconv.Itoa(maxDimensionValues)
	parts := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		dim := attributeDimensionRegistry[k]
		var collected string
		switch dim.kind {
		case attributeDimArray:
			// Flatten the per-row arrays and dedup across the group.
			collected = "groupUniqArrayArray(" + capStr + ")(" + dim.column + ")"
		case attributeDimProject:
			collected = "groupUniqArray(" + capStr + ")(toString(" + dim.column + "))"
		case attributeDimScalar:
			collected = "groupUniqArray(" + capStr + ")(" + dim.column + ")"
		}
		// Drop empty strings so absent attributes don't surface as a blank value.
		// billing_mode is the exception: '' marks an unclassified contributor, and
		// the cost view must see it so a scope mixing metered and unclassified
		// spend is never presented as confidently metered (DNO-384).
		valExpr := "arrayFilter(x -> x != '', " + collected + ")"
		if k == "billing_mode" {
			valExpr = collected
		}
		parts = append(parts, "'"+k+"', "+valExpr)
	}
	return "map(" + strings.Join(parts, ", ") + ") AS dimension_values"
}

// arrayDimFilter builds the WHERE predicate for an Array(String) dimension.
// An empty array is grouped under the "" ("(unset)") bucket — see the
// empty→[”] mapping in attributeGroupValueExpr — so a requested "" value must
// match array emptiness, not a literal "" element: hasAny never matches an
// empty array. Non-empty requested values keep using hasAny; when both are
// present they combine with OR so the "(unset)" row stays drillable for arrays.
func arrayDimFilter(column string, values []string) squirrel.Sqlizer {
	hasEmpty := false
	nonEmpty := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			hasEmpty = true
			continue
		}
		nonEmpty = append(nonEmpty, v)
	}
	emptyPred := squirrel.Expr("empty(" + column + ")")
	if len(nonEmpty) == 0 {
		// Only "(unset)" requested → match rows whose array is empty.
		return emptyPred
	}
	// hasAny(col, [v1, v2, ...]); clickhouse-go binds the slice as an array arg.
	hasAnyPred := squirrel.Expr("hasAny("+column+", ?)", nonEmpty)
	if !hasEmpty {
		return hasAnyPred
	}
	return squirrel.Or{hasAnyPred, emptyPred}
}

// applyAttributeFilters adds the WHERE predicates for the supplied filters.
func applyAttributeFilters(sb squirrel.SelectBuilder, filters []AttributeMetricsFilter) (squirrel.SelectBuilder, error) {
	for _, f := range filters {
		if len(f.Values) == 0 {
			continue
		}
		dim, ok := attributeDimensionRegistry[f.Dimension]
		if !ok {
			return sb, fmt.Errorf("unknown filter dimension %q", f.Dimension)
		}
		switch dim.kind {
		case attributeDimArray:
			sb = sb.Where(arrayDimFilter(dim.column, f.Values))
		case attributeDimScalar, attributeDimProject:
			sb = sb.Where(squirrel.Eq{dim.column: f.Values})
		default:
			return sb, fmt.Errorf("unhandled dimension kind for filter %q", f.Dimension)
		}
	}
	return sb, nil
}

// QueryAttributeMetricsTable returns one row per group value (or a single row
// when GroupBy is empty), aggregated over the whole time range and ordered by
// SortBy descending. No top_n limit is applied here — the service layer decides
// which groups to keep and rolls the rest into "Other" so the table and
// timeseries agree on group membership.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) QueryAttributeMetricsTable(ctx context.Context, arg AttributeMetricsQueryParams) ([]AttributeMetricsRow, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	groupExpr, grouped, err := attributeGroupValueExpr(arg.GroupBy)
	if err != nil {
		return nil, err
	}
	if !attributeMeasureSet[arg.SortBy] {
		return nil, fmt.Errorf("unknown sort_by measure %q", arg.SortBy)
	}

	sb := sq.Select(groupExpr+" AS group_value").
		Columns(attributeMeasureSelects...).
		Column(squirrel.Expr(attributeDimensionValuesExpr(arg.GroupBy))).
		From("attribute_metrics_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("time_bucket >= toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeStart).
		Where("time_bucket <= toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeEnd)

	sb, err = applyAttributeFilters(sb, arg.Filters)
	if err != nil {
		return nil, err
	}

	if grouped {
		sb = sb.GroupBy("group_value")
	}
	// Order by the prefixed merged alias (a comparable scalar), not the state
	// column of the same base name.
	sb = sb.OrderBy(measureAliasPrefix + arg.SortBy + " DESC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building attribute metrics table query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AttributeMetricsRow
	for rows.Next() {
		var row AttributeMetricsRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning attribute metrics row: %w", err)
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// QueryAttributeMetricsTimeseries returns raw (group, bucket) cells aggregated
// at IntervalSeconds. Buckets are not gap-filled and groups are not limited;
// the service layer handles "Other" rollup and zero-fill.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) QueryAttributeMetricsTimeseries(ctx context.Context, arg AttributeMetricsQueryParams) ([]AttributeMetricsTimePoint, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	groupExpr, grouped, err := attributeGroupValueExpr(arg.GroupBy)
	if err != nil {
		return nil, err
	}

	sb := sq.Select().
		Column(squirrel.Expr(
			"toInt64(toStartOfInterval(time_bucket, toIntervalSecond(?))) * 1000000000 AS bucket_time_unix_nano",
			arg.IntervalSeconds,
		)).
		Column(groupExpr+" AS group_value").
		Columns(attributeMeasureSelects...).
		From("attribute_metrics_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("time_bucket >= toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeStart).
		Where("time_bucket <= toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeEnd)

	sb, err = applyAttributeFilters(sb, arg.Filters)
	if err != nil {
		return nil, err
	}

	groupCols := []string{"bucket_time_unix_nano"}
	if grouped {
		groupCols = append(groupCols, "group_value")
	}
	sb = sb.GroupBy(groupCols...)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building attribute metrics timeseries query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AttributeMetricsTimePoint
	for rows.Next() {
		var point AttributeMetricsTimePoint
		if err = rows.ScanStruct(&point); err != nil {
			return nil, fmt.Errorf("scanning attribute metrics timeseries row: %w", err)
		}
		out = append(out, point)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
