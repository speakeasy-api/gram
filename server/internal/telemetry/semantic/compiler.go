package semantic

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Masterminds/squirrel"
)

// sq is the ClickHouse-flavored squirrel builder (?-placeholders), matching
// the telemetry repo package.
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// maxDimensionValues caps the per-dimension distinct-value lists collected for
// each grouped table row, mirroring the legacy repo cap.
const maxDimensionValues = 1000

// measureAliasPrefix prefixes measure SELECT aliases. Without it an alias
// could collide with an underlying state column of the same name and
// ClickHouse would resolve ORDER BY to the non-comparable state column.
const measureAliasPrefix = "m_"

// Compile turns a plan into ClickHouse SQL. GranularitySeconds selects the
// shape: 0 compiles the whole-range table query, >0 the bucketed timeseries.
// Both reproduce the legacy attribute-metrics SQL structure exactly, with
// catalog names in the measure aliases and dimension_values keys.
func Compile(plan *QueryPlan) (string, []any, error) {
	groupExpr, grouped, err := groupValueExpr(plan)
	if err != nil {
		return "", nil, err
	}

	measureSelects := make([]string, 0, len(plan.Measures))
	for _, ms := range plan.Measures {
		measureSelects = append(measureSelects, plan.Binding.Measures[ms.Name].SQL+" AS "+measureAliasPrefix+ms.Name)
	}

	var sb squirrel.SelectBuilder
	if plan.Query.GranularitySeconds > 0 {
		bucketExpr, err := bucketTimeExpr(plan.Binding.Time)
		if err != nil {
			return "", nil, err
		}
		sb = sq.Select().
			Column(squirrel.Expr(bucketExpr, plan.Query.GranularitySeconds)).
			Column(groupExpr + " AS group_value").
			Columns(measureSelects...)
	} else {
		sb = sq.Select(groupExpr + " AS group_value").
			Columns(measureSelects...)
		if len(plan.DimensionValuesDims) > 0 {
			expr, err := dimensionValuesExpr(plan)
			if err != nil {
				return "", nil, err
			}
			sb = sb.Column(squirrel.Expr(expr))
		}
	}

	sb = sb.From(plan.Binding.Source)
	if plan.Binding.RowFilter != "" {
		sb = sb.Where(plan.Binding.RowFilter)
	}
	sb = sb.Where(squirrel.Eq{plan.Binding.Dimensions["project"].SQL: plan.Query.Scope.ProjectIDs})

	timeCol := plan.Binding.Time.Column
	switch plan.Binding.Time.Kind {
	case TimeKindHourBucket:
		sb = sb.Where(timeCol+" >= toStartOfHour(fromUnixTimestamp64Nano(?))", plan.Query.TimeStart).
			Where(timeCol+" <= toStartOfHour(fromUnixTimestamp64Nano(?))", plan.Query.TimeEnd)
	case TimeKindUnixNano:
		sb = sb.Where(timeCol+" >= ?", plan.Query.TimeStart).
			Where(timeCol+" <= ?", plan.Query.TimeEnd)
	default:
		return "", nil, fmt.Errorf("unhandled binding time kind %q", plan.Binding.Time.Kind)
	}

	sb, err = applyFilters(sb, plan)
	if err != nil {
		return "", nil, err
	}

	if plan.Query.GranularitySeconds > 0 {
		groupCols := []string{"bucket_time_unix_nano"}
		if grouped {
			groupCols = append(groupCols, "group_value")
		}
		sb = sb.GroupBy(groupCols...)
	} else {
		if grouped {
			sb = sb.GroupBy("group_value")
		}
		if plan.SortMeasure != "" {
			direction := " ASC"
			if plan.Query.Sort != nil && plan.Query.Sort.Desc {
				direction = " DESC"
			}
			// Order by the prefixed alias (a comparable scalar), never a state
			// column of the same base name.
			sb = sb.OrderBy(measureAliasPrefix + plan.SortMeasure + direction)
		}
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building semantic query: %w", err)
	}
	return query, args, nil
}

// groupValueExpr returns the SELECT/GROUP BY expression for the query's
// GroupBy dimension and whether a GROUP BY is needed.
func groupValueExpr(plan *QueryPlan) (expr string, grouped bool, err error) {
	if plan.Query.GroupBy == "" {
		return "''", false, nil
	}
	dim, ok := plan.Definition.Dimension(plan.Query.GroupBy)
	if !ok {
		return "", false, fmt.Errorf("unknown group_by dimension %q", plan.Query.GroupBy)
	}
	sql := plan.Binding.Dimensions[dim.Name].SQL
	switch dim.Type {
	case DimTypeStringArray:
		// arrayJoin attributes spend to each element. Map the empty array to a
		// single empty-string element so rows with no values are not silently
		// dropped — they surface under the '' group, the same way a missing
		// scalar attribute does.
		return "arrayJoin(if(empty(" + sql + "), [''], " + sql + "))", true, nil
	case DimTypeID:
		return "toString(" + sql + ")", true, nil
	case DimTypeString:
		return sql, true, nil
	default:
		return "", false, fmt.Errorf("unhandled dimension type for %q", dim.Name)
	}
}

// bucketTimeExpr returns the bucket-start expression (unix nanoseconds) for
// the binding's time addressing; the single ? is the interval seconds.
func bucketTimeExpr(t BindingTime) (string, error) {
	switch t.Kind {
	case TimeKindHourBucket:
		return "toInt64(toStartOfInterval(" + t.Column + ", toIntervalSecond(?))) * 1000000000 AS bucket_time_unix_nano", nil
	case TimeKindUnixNano:
		return "toInt64(toStartOfInterval(fromUnixTimestamp64Nano(" + t.Column + "), toIntervalSecond(?))) * 1000000000 AS bucket_time_unix_nano", nil
	default:
		return "", fmt.Errorf("unhandled binding time kind %q", t.Kind)
	}
}

// dimensionValuesExpr builds the map() expression collecting, per group, the
// distinct values of every plan dimension-values dimension. Keys are catalog
// dimension names, pre-sorted by the planner. Dimension names come from the
// definition (never client input) so inlining the string literals is safe.
func dimensionValuesExpr(plan *QueryPlan) (string, error) {
	capStr := strconv.Itoa(maxDimensionValues)
	parts := make([]string, 0, len(plan.DimensionValuesDims))
	for _, name := range plan.DimensionValuesDims {
		dim, ok := plan.Definition.Dimension(name)
		if !ok {
			return "", fmt.Errorf("unknown dimension_values dimension %q", name)
		}
		sql := plan.Binding.Dimensions[name].SQL
		var collected string
		switch dim.Type {
		case DimTypeStringArray:
			// Flatten the per-row arrays and dedup across the group. An empty
			// array contributes no elements, so the "(unset)" bucket the
			// group-by path renders for it (empty → ['']) is re-added
			// explicitly when any row in the group carries one.
			collected = "arrayDistinct(arrayConcat(groupUniqArrayArray(" + capStr + ")(" + sql + "), if(countIf(empty(" + sql + ")) > 0, [''], [])))"
		case DimTypeID:
			collected = "groupUniqArray(" + capStr + ")(toString(" + sql + "))"
		case DimTypeString:
			collected = "groupUniqArray(" + capStr + ")(" + sql + ")"
		default:
			return "", fmt.Errorf("unhandled dimension type for %q", name)
		}
		// '' is kept by default: for most dimensions an empty value is the
		// real, drillable "(unset)" bucket a breakdown would render.
		// Dimensions declaring empty_means: not_applicable are the exception:
		// there '' just marks rows the attribute doesn't apply to, so it is
		// dropped rather than surfacing as a blank value.
		valExpr := collected
		if dim.EmptyMeans == "not_applicable" {
			valExpr = "arrayFilter(x -> x != '', " + collected + ")"
		}
		parts = append(parts, "'"+name+"', "+valExpr)
	}
	return "map(" + strings.Join(parts, ", ") + ") AS dimension_values", nil
}

// applyFilters adds the WHERE predicates for the query's filters, in query
// order.
func applyFilters(sb squirrel.SelectBuilder, plan *QueryPlan) (squirrel.SelectBuilder, error) {
	for _, f := range plan.Query.Filters {
		if len(f.Values) == 0 {
			continue
		}
		dim, ok := plan.Definition.Dimension(f.Dimension)
		if !ok {
			return sb, fmt.Errorf("unknown filter dimension %q", f.Dimension)
		}
		sql := plan.Binding.Dimensions[dim.Name].SQL
		switch dim.Type {
		case DimTypeStringArray:
			sb = sb.Where(arrayDimFilter(sql, f.Values))
		case DimTypeString, DimTypeID:
			sb = sb.Where(squirrel.Eq{sql: f.Values})
		default:
			return sb, fmt.Errorf("unhandled dimension type for filter %q", f.Dimension)
		}
	}
	return sb, nil
}

// arrayDimFilter builds the WHERE predicate for an Array(String) dimension.
// An empty array is grouped under the "" ("(unset)") bucket — see the
// empty→[”] mapping in groupValueExpr — so a requested "" value must match
// array emptiness, not a literal "" element: hasAny never matches an empty
// array. Non-empty requested values keep using hasAny; when both are present
// they combine with OR so the "(unset)" row stays drillable for arrays.
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
