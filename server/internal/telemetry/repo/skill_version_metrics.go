package repo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Masterminds/squirrel"
)

const skillVersionDimension = "skill_version"

const (
	skillVersionAssistantUsagePredicate = "(gram_urn = 'assistants:chat:completion')"
	skillVersionUsageMeasureFilter      = "(" + sessionUsageMeasureFilter + " OR " + skillVersionAssistantUsagePredicate + ")"
	skillVersionSourceRowPredicate      = "(" + sessionSourceRowPredicate + " OR " + skillVersionAssistantUsagePredicate + ")"
	skillVersionInputTokensExpr         = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.input_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))), " + skillVersionUsageMeasureFilter + ")"
	skillVersionOutputTokensExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.output_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))), " + skillVersionUsageMeasureFilter + ")"
	skillVersionTotalTokensExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.input_tokens)) + toInt64OrZero(toString(attributes.output_tokens)) + " +
		"toInt64OrZero(toString(attributes.cache_creation_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)) + toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)) + " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), " + skillVersionUsageMeasureFilter + ")"
	skillVersionCacheReadTokensExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.cache_read_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens))), " + skillVersionUsageMeasureFilter + ")"
	skillVersionCacheCreationTokensExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.cache_creation_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), " + skillVersionUsageMeasureFilter + ")"
	skillVersionCostExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), " +
		"toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0), " +
		"toFloat64OrZero(toString(attributes.gen_ai.usage.cost))), " + skillVersionUsageMeasureFilter + ")"
)

var skillVersionSessionMeasureSelects = []string{
	skillVersionCostExpr + " AS s_total_cost",
	skillVersionInputTokensExpr + " AS s_total_input_tokens",
	skillVersionOutputTokensExpr + " AS s_total_output_tokens",
	skillVersionTotalTokensExpr + " AS s_total_tokens",
	skillVersionCacheReadTokensExpr + " AS s_cache_read_input_tokens",
	skillVersionCacheCreationTokensExpr + " AS s_cache_creation_input_tokens",
	"uniqExactIf(" + sessionToolCallDedupIDExpr + ", " + sessionCountedToolCallPredicate + ") AS s_total_tool_calls",
}

var skillVersionMeasureSelects = []string{
	"sum(s_total_cost) AS m_total_cost",
	"sum(s_total_input_tokens) AS m_total_input_tokens",
	"sum(s_total_output_tokens) AS m_total_output_tokens",
	"sum(s_total_tokens) AS m_total_tokens",
	"sum(s_cache_read_input_tokens) AS m_cache_read_input_tokens",
	"sum(s_cache_creation_input_tokens) AS m_cache_creation_input_tokens",
	"sum(s_total_tool_calls) AS m_total_tool_calls",
	"countIf(s_has_usage) AS m_total_chats",
}

// skillVersionSortMeasures is the set of measures the skill-version
// raw-telemetry query actually computes, derived from
// skillVersionMeasureSelects so it can never drift from the SQL.
// attributeMeasureSet entries outside it (the work-units scoring measures,
// which live in the summary path only) are rejected up front: sorting by them
// would reference a nonexistent m_ alias at execution time.
var skillVersionSortMeasures = func() map[string]bool {
	measures := make(map[string]bool, len(skillVersionMeasureSelects))
	for _, sel := range skillVersionMeasureSelects {
		if _, alias, ok := strings.Cut(sel, " AS "+measureAliasPrefix); ok {
			measures[alias] = true
		}
	}
	return measures
}()

func skillVersionDimensionKeys() []string {
	keys := make([]string, 0, len(sessionDimensionRegistry)+1)
	for key := range sessionDimensionRegistry {
		keys = append(keys, key)
	}
	keys = append(keys, skillVersionDimension)
	sort.Strings(keys)
	return keys
}

func sessionDimensionAlias(key string) string {
	return "d_" + key
}

func skillVersionSessionSelects() []string {
	keys := skillVersionDimensionKeys()
	selects := make([]string, 0, len(keys)-1)
	for _, key := range keys {
		if key == skillVersionDimension {
			continue
		}
		dim := sessionDimensionRegistry[key]
		alias := sessionDimensionAlias(key)
		switch dim.kind {
		case attributeDimArray:
			selects = append(selects, "arrayDistinct(arrayFlatten(groupArray("+dim.column+"))) AS "+alias)
		case attributeDimProject:
			selects = append(selects, "toString(gram_project_id) AS "+alias)
		case attributeDimScalar:
			selects = append(selects, "argMaxIf("+dim.column+", time_unix_nano, "+dim.column+" != '') AS "+alias)
		}
	}
	return selects
}

func skillVersionGroupExpr(groupBy string) (string, bool, error) {
	if groupBy == "" {
		return "''", false, nil
	}
	if groupBy == skillVersionDimension {
		return "arrayJoin(skill_versions)", true, nil
	}
	dim, ok := sessionDimensionRegistry[groupBy]
	if !ok {
		return "", false, fmt.Errorf("unknown group_by dimension %q", groupBy)
	}
	alias := sessionDimensionAlias(groupBy)
	if dim.kind == attributeDimArray {
		return "arrayJoin(if(empty(" + alias + "), [''], " + alias + "))", true, nil
	}
	return alias, true, nil
}

func skillVersionDimensionValuesExpr(groupBy string) string {
	parts := make([]string, 0, len(sessionDimensionRegistry)*2)
	capStr := strconv.Itoa(maxDimensionValues)
	for _, key := range skillVersionDimensionKeys() {
		if key == groupBy {
			continue
		}
		var collected string
		if key == skillVersionDimension {
			collected = "arrayDistinct(arrayFlatten(groupArray(skill_versions)))"
		} else {
			dim := sessionDimensionRegistry[key]
			alias := sessionDimensionAlias(key)
			switch dim.kind {
			case attributeDimArray:
				collected = "arrayDistinct(arrayConcat(arrayFlatten(groupArray(" + alias + ")), if(countIf(empty(" + alias + ")) > 0, [''], [])))"
			case attributeDimProject, attributeDimScalar:
				collected = "groupUniqArray(" + capStr + ")(" + alias + ")"
			}
			if dim.emptyIsNotApplicable {
				collected = "arrayFilter(x -> x != '', " + collected + ")"
			}
		}
		parts = append(parts, "'"+key+"', "+collected)
	}
	return "map(" + strings.Join(parts, ", ") + ") AS dimension_values"
}

func partitionSkillVersionFilters(filters []AttributeMetricsFilter) ([][]string, []AttributeMetricsFilter) {
	versionFilters := make([][]string, 0, len(filters))
	sessionFilters := make([]AttributeMetricsFilter, 0, len(filters))
	for _, filter := range filters {
		if filter.Dimension != skillVersionDimension {
			sessionFilters = append(sessionFilters, filter)
			continue
		}
		if len(filter.Values) > 0 {
			versionFilters = append(versionFilters, filter.Values)
		}
	}
	return versionFilters, sessionFilters
}

func skillVersionMappingRows(projectIDs []string, versionFilters [][]string) squirrel.SelectBuilder {
	rows := sq.Select().
		From("skill_session_versions").
		Where(squirrel.Eq{"project_id": projectIDs})
	if len(versionFilters) == 0 {
		return rows
	}

	relevantValues := make(map[string]struct{})
	for _, values := range versionFilters {
		for _, value := range values {
			relevantValues[value] = struct{}{}
		}
	}
	values := make([]string, 0, len(relevantValues))
	for value := range relevantValues {
		values = append(values, value)
	}
	sort.Strings(values)
	return rows.Where(squirrel.Eq{"toString(skill_version_id)": values})
}

func requireSkillVersionFilters(builder squirrel.SelectBuilder, versionFilters [][]string) squirrel.SelectBuilder {
	for _, values := range versionFilters {
		builder = builder.Having("hasAny(groupUniqArray(toString(skill_version_id)), ?)", values)
	}
	return builder
}

func buildSkillVersionMetricsQuery(arg AttributeMetricsQueryParams, timeseries bool) (string, []any, error) {
	if !attributeMeasureSet[arg.SortBy] {
		return "", nil, fmt.Errorf("unknown sort_by measure %q", arg.SortBy)
	}
	if !skillVersionSortMeasures[arg.SortBy] {
		return "", nil, fmt.Errorf("sort_by measure %q is not supported for skill version grouping", arg.SortBy)
	}

	versionFilters, sessionFilters := partitionSkillVersionFilters(arg.Filters)
	mappingRows := skillVersionMappingRows(arg.ProjectIDs, versionFilters)
	mappings := mappingRows.Columns(
		"project_id",
		"session_id",
		"surface",
		"groupUniqArray(toString(skill_version_id)) AS skill_versions",
	).
		GroupBy("project_id", "session_id", "surface")
	mappings = requireSkillVersionFilters(mappings, versionFilters)

	mappedSessions := mappingRows.
		Column("session_id").
		GroupBy("project_id", "session_id", "surface")
	mappedSessions = requireSkillVersionFilters(mappedSessions, versionFilters)

	sessions := mappedSkillTelemetryQuery(arg.ProjectIDs, arg.TimeStart, arg.TimeEnd, mappedSessions).
		Columns(
			"gram_project_id AS project_id",
			"chat_id AS session_id",
			// Surface stays in the join key: assistant completion URNs map to
			// assistant, while every admitted dev-agent telemetry row maps to dev.
			"if("+skillVersionAssistantUsagePredicate+", 'assistant', 'dev') AS surface",
			"min(time_unix_nano) AS session_time_unix_nano",
			"countIf("+skillVersionUsageMeasureFilter+") > 0 AS s_has_usage",
		).
		Columns(skillVersionSessionMeasureSelects...).
		Columns(skillVersionSessionSelects()...).
		GroupBy("gram_project_id", "chat_id", "surface")
	var err error
	sessions, err = applySessionFilters(sessions, sessionFilters, canonicalIdentityOrgLiteral(arg.CanonicalIdentityOrg))
	if err != nil {
		return "", nil, err
	}

	groupExpr, grouped, err := skillVersionGroupExpr(arg.GroupBy)
	if err != nil {
		return "", nil, err
	}
	outer := sq.Select(groupExpr + " AS group_value").
		Columns(skillVersionMeasureSelects...).
		From("sessions").
		Join("mappings USING (project_id, session_id, surface)").
		PrefixExpr(squirrel.ConcatExpr(
			"WITH mappings AS (", mappings, "), sessions AS (", sessions, ")",
		))

	groupColumns := make([]string, 0, 2)
	if timeseries {
		outer = outer.Column(squirrel.Expr(
			"toInt64(toStartOfInterval(fromUnixTimestamp64Nano(session_time_unix_nano), toIntervalSecond(?))) * 1000000000 AS bucket_time_unix_nano",
			arg.IntervalSeconds,
		))
		groupColumns = append(groupColumns, "bucket_time_unix_nano")
	} else {
		outer = outer.Column(squirrel.Expr(skillVersionDimensionValuesExpr(arg.GroupBy)))
	}
	if grouped {
		groupColumns = append(groupColumns, "group_value")
	}
	if len(groupColumns) > 0 {
		outer = outer.GroupBy(groupColumns...)
	}
	if !timeseries {
		outer = outer.OrderBy(measureAliasPrefix + arg.SortBy + " DESC")
	}

	outer = withCanonicalFoldSettings(outer, canonicalIdentityOrgLiteral(arg.CanonicalIdentityOrg))
	query, args, err := outer.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building skill version metrics query: %w", err)
	}
	return query, args, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) QuerySkillVersionMetricsTable(ctx context.Context, arg AttributeMetricsQueryParams) ([]AttributeMetricsRow, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}
	query, args, err := buildSkillVersionMetricsQuery(arg, false)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("scanning skill version metrics row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) QuerySkillVersionMetricsTimeseries(ctx context.Context, arg AttributeMetricsQueryParams) ([]AttributeMetricsTimePoint, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}
	query, args, err := buildSkillVersionMetricsQuery(arg, true)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("scanning skill version metrics point: %w", err)
		}
		out = append(out, point)
	}
	return out, rows.Err()
}
