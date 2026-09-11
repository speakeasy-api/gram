package chrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/ext"
	"github.com/Masterminds/squirrel"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// ReadingKindUsage selects ordinary positive usage facts.
	ReadingKindUsage = "usage"

	// ReadingKindAdjustment selects separate signed correction facts.
	ReadingKindAdjustment = "adjustment"
)

var (
	// ErrInvalidUsageSelection reports an incomplete or unsupported internal selection.
	ErrInvalidUsageSelection = errors.New("invalid meter usage selection")

	// ErrInvalidReadingKind reports a reading kind outside usage and adjustment.
	ErrInvalidReadingKind = errors.New("invalid meter reading kind")

	// ErrMixedMeasurement reports rows that violate their family's fixed measurement contract.
	ErrMixedMeasurement = errors.New("meter usage contains incompatible units or measurement methods")

	// ErrUsageSummaryUnavailable reports absent, stale, or changing published coverage.
	ErrUsageSummaryUnavailable = errors.New("meter usage daily summary is unavailable")
)

// UsageFacetKind identifies one closed SQL grouping shape.
type UsageFacetKind string

const (
	// UsageFacetTotal groups the full family into one series.
	UsageFacetTotal UsageFacetKind = "total"

	// UsageFacetProject groups by the retained project UUID.
	UsageFacetProject UsageFacetKind = "project"

	// UsageFacetAttribute groups by one scalar map attribute.
	UsageFacetAttribute UsageFacetKind = "attribute"

	// UsageFacetSortedSet groups by one normalized JSON string-set attribute.
	UsageFacetSortedSet UsageFacetKind = "sorted_set"

	// UsageFacetMeter maps exact meter IDs to series identities.
	UsageFacetMeter UsageFacetKind = "meter"

	// UsageFacetMCPServer groups by server type plus server ID with a slug label.
	UsageFacetMCPServer UsageFacetKind = "mcp_server"
)

// UsageFacetValue maps one exact meter identity to a stable key and label.
type UsageFacetValue struct {
	// Source is the exact meter identifier.
	Source string

	// Key is the stable series identity.
	Key string

	// Label is display text for the series.
	Label string
}

// UsageFacet is a semantic facet selection. Attribute names must belong to the
// repository's promoted-column allowlist and are never accepted from API callers.
type UsageFacet struct {
	// Kind chooses one of the repository's closed grouping shapes.
	Kind UsageFacetKind

	// Attribute selects the primary promoted reporting attribute.
	Attribute string

	// SecondaryAttribute completes compound identities such as MCP server type plus ID.
	SecondaryAttribute string

	// LabelAttribute selects the promoted display-label attribute for identities.
	LabelAttribute string

	// Values maps exact meter IDs for meter-backed dimensions.
	Values []UsageFacetValue
}

// UsageSelection is a family-compatible meter and measurement selection.
type UsageSelection struct {
	// Family is the canonical stored summary family.
	Family string

	// Breakdown is the canonical stored summary facet.
	Breakdown string

	// MeterIDs are the exact registered meter identifiers included in the family.
	MeterIDs []string

	// Unit is the sole unit allowed in selected readings.
	Unit string

	// MeasurementMethod is the sole measurement method allowed in selected readings.
	MeasurementMethod string

	// Facet is the validated grouping shape for the requested breakdown.
	Facet UsageFacet
}

// UsageParams defines one organization-owned bounded meter report.
type UsageParams struct {
	// OrganizationID identifies the owning organization.
	OrganizationID string

	// Selection fixes the exact meters, measurement contract, and grouping shape.
	Selection UsageSelection

	// From is the inclusive occurrence-time boundary.
	From time.Time

	// To is the exclusive occurrence-time boundary.
	To time.Time

	// ReadingKind selects ordinary usage or separate adjustments.
	ReadingKind string
}

// UsageRow is one bounded daily/facet aggregate returned by GetUsage.
type UsageRow struct {
	// Day is the UTC calendar day containing this aggregate.
	Day time.Time

	// Unit is the fixed family unit.
	Unit string

	// MeasurementMethod is the fixed family measurement method.
	MeasurementMethod string

	// Kind is value, unset, or remainder.
	Kind string

	// Key is the canonical identity for value rows and empty for markers.
	Key string

	// Label is display text and never chart identity.
	Label string

	// Total is an exact signed integer encoded in base ten.
	Total string
}

// UsageResult contains compatible measurement metadata and bounded aggregates.
type UsageResult struct {
	// Unit is the fixed family unit, including for empty results.
	Unit string

	// MeasurementMethod is the fixed family method, including for empty results.
	MeasurementMethod string

	// Rows contains at most seven aggregates per intersected UTC day.
	Rows []UsageRow
}

type usageFacetSQL struct {
	kind  string
	key   string
	label string
}

func buildUsageFacetSQL(facet UsageFacet) (usageFacetSQL, error) {
	for _, attribute := range [3]string{facet.Attribute, facet.SecondaryAttribute, facet.LabelAttribute} {
		switch attribute {
		case "",
			"assistant_id", "billing_mode",
			"billing_user_cost_center_name", "billing_user_department_name",
			"billing_user_directory_groups", "billing_user_division_name", "billing_user_employee_type",
			"billing_user_id", "billing_user_job_title",
			"mcp_server_id", "mcp_server_slug", "mcp_server_type",
			"model", "provider", "risk_policy_id", "tool_name":
		default:
			return usageFacetSQL{}, ErrInvalidUsageSelection
		}
	}

	switch facet.Kind {
	case UsageFacetTotal:
		return valueFacet("'total'", "'Total'"), nil
	case UsageFacetProject:
		return valueFacet("toString(project_id)", "toString(project_id)"), nil
	case UsageFacetAttribute:
		if facet.Attribute == "" || facet.SecondaryAttribute != "" || facet.LabelAttribute != "" || len(facet.Values) != 0 {
			return usageFacetSQL{}, ErrInvalidUsageSelection
		}
		return attributeFacet(facet.Attribute), nil
	case UsageFacetSortedSet:
		if facet.Attribute == "" || facet.SecondaryAttribute != "" || facet.LabelAttribute != "" || len(facet.Values) != 0 {
			return usageFacetSQL{}, ErrInvalidUsageSelection
		}
		return sortedSetFacet(facet.Attribute), nil
	case UsageFacetMeter:
		if facet.Attribute != "" || facet.SecondaryAttribute != "" || facet.LabelAttribute != "" || len(facet.Values) == 0 {
			return usageFacetSQL{}, ErrInvalidUsageSelection
		}
		return meterFacet(facet.Values), nil
	case UsageFacetMCPServer:
		if facet.Attribute == "" || facet.SecondaryAttribute == "" || facet.LabelAttribute == "" || len(facet.Values) != 0 {
			return usageFacetSQL{}, ErrInvalidUsageSelection
		}
		return mcpServerFacet(facet.Attribute, facet.SecondaryAttribute, facet.LabelAttribute), nil
	default:
		return usageFacetSQL{}, ErrInvalidUsageSelection
	}
}

func valueFacet(key, label string) usageFacetSQL {
	return usageFacetSQL{kind: "'value'", key: key, label: label}
}

func attributeFacet(attribute string) usageFacetSQL {
	value := promotedAttribute(attribute)
	return usageFacetSQL{
		kind:  fmt.Sprintf("if(%s = '', 'unset', 'value')", value),
		key:   value,
		label: fmt.Sprintf("if(%s = '', '(unset)', %s)", value, value),
	}
}

func sortedSetFacet(attribute string) usageFacetSQL {
	attributeValue := promotedAttribute(attribute)
	value := fmt.Sprintf("arraySort(arrayDistinct(JSONExtract(if(%s = '', '[]', %s), 'Array(String)')))", attributeValue, attributeValue)
	return usageFacetSQL{
		kind:  fmt.Sprintf("if(empty(%s), 'unset', 'value')", value),
		key:   fmt.Sprintf("if(empty(%s), '', toJSONString(%s))", value, value),
		label: fmt.Sprintf("if(empty(%s), '(unset)', arrayStringConcat(%s, ', '))", value, value),
	}
}

func meterFacet(values []UsageFacetValue) usageFacetSQL {
	keyArgs := make([]string, 0, len(values)*2+1)
	labelArgs := make([]string, 0, len(values)*2+1)
	for _, value := range values {
		keyArgs = append(keyArgs, "meter_id = "+sqlLiteral(value.Source), sqlLiteral(value.Key))
		labelArgs = append(labelArgs, "meter_id = "+sqlLiteral(value.Source), sqlLiteral(value.Label))
	}
	keyArgs = append(keyArgs, "toString(meter_id)")
	labelArgs = append(labelArgs, "toString(meter_id)")
	return usageFacetSQL{
		kind:  "'value'",
		key:   "multiIf(" + strings.Join(keyArgs, ", ") + ")",
		label: "multiIf(" + strings.Join(labelArgs, ", ") + ")",
	}
}

func mcpServerFacet(typeAttribute, idAttribute, slugAttribute string) usageFacetSQL {
	serverType := promotedAttribute(typeAttribute)
	serverID := promotedAttribute(idAttribute)
	slug := promotedAttribute(slugAttribute)
	missing := fmt.Sprintf("%s = '' OR %s = ''", serverType, serverID)
	key := fmt.Sprintf("concat(%s, ':', %s)", serverType, serverID)
	return usageFacetSQL{
		kind:  fmt.Sprintf("if(%s, 'unset', 'value')", missing),
		key:   fmt.Sprintf("if(%s, '', %s)", missing, key),
		label: fmt.Sprintf("if(%s, '(unset)', if(%s = '', %s, %s))", missing, slug, key, slug),
	}
}

func promotedAttribute(attribute string) string {
	return "`" + attribute + "`"
}

func sqlLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

type usagePublication struct {
	publishedBefore time.Time
	snapshotAt      time.Time
}

type usageRawFragment struct {
	day               time.Time
	seriesKind        string
	seriesKey         string
	label             string
	unit              string
	measurementMethod string
	quantity          string
	readingCount      uint64
}

type usageTimeRange struct {
	from time.Time
	to   time.Time
}

const usageQuerySettings = " SETTINGS do_not_merge_across_partitions_select_final = 1, max_threads = 2, max_final_threads = 2, max_block_size = 1024, max_bytes_before_external_group_by = 67108864, max_bytes_before_external_sort = 67108864"

// GetUsage returns a fixed-period top-six facet aggregation. Complete published
// UTC days come from the daily summary. Only partial boundaries and the bounded
// unsettled tail read the immutable raw ledger.
func (q *Queries) GetUsage(ctx context.Context, params UsageParams) (UsageResult, error) {
	selection := params.Selection
	if selection.Family == "" || selection.Breakdown == "" || len(selection.MeterIDs) == 0 || selection.Unit == "" || selection.MeasurementMethod == "" {
		return UsageResult{}, ErrInvalidUsageSelection
	}
	facet, err := buildUsageFacetSQL(selection.Facet)
	if err != nil {
		return UsageResult{}, err
	}
	if params.ReadingKind != ReadingKindUsage && params.ReadingKind != ReadingKindAdjustment {
		return UsageResult{}, ErrInvalidReadingKind
	}

	result := UsageResult{Unit: selection.Unit, MeasurementMethod: selection.MeasurementMethod, Rows: make([]UsageRow, 0)}
	from, to := params.From.UTC(), params.To.UTC()
	if !from.Before(to) {
		return result, nil
	}

	publication, err := q.getUsagePublication(ctx)
	if err != nil {
		return UsageResult{}, err
	}
	summaryFrom, summaryTo, rawRanges := usageCoverage(from, to, publication.publishedBefore)
	if usagePastTailDays(from, to, publication.publishedBefore, time.Now().UTC()) > 2 {
		return UsageResult{}, fmt.Errorf("%w: published coverage is too old", ErrUsageSummaryUnavailable)
	}

	rawTable, err := ext.NewTable(
		"usage_raw_fragments",
		ext.Column("day", "Date"),
		ext.Column("series_kind", "String"),
		ext.Column("series_key", "String"),
		ext.Column("label", "String"),
		ext.Column("unit", "String"),
		ext.Column("measurement_method", "String"),
		ext.Column("quantity", "String"),
		ext.Column("reading_count", "UInt64"),
	)
	if err != nil {
		return UsageResult{}, fmt.Errorf("create meter usage raw fragment table: %w", err)
	}
	if err := q.getUsageRawFragments(ctx, params, facet, rawRanges, rawTable); err != nil {
		return UsageResult{}, err
	}

	summaryScan, summaryArgs, err := usageSummaryScan(params, summaryFrom, summaryTo, selection.Breakdown)
	if err != nil {
		return UsageResult{}, err
	}
	rankQuantity := "quantity"
	if params.ReadingKind == ReadingKindAdjustment {
		rankQuantity = "abs(quantity)"
	}
	rankingQuery := fmt.Sprintf(`
WITH combined AS (
	%s
	UNION ALL
	SELECT
		toUInt8(0) AS is_publication,
		%s AS facet,
		day,
		series_kind,
		series_key,
		label,
		unit,
		measurement_method,
		toInt128(quantity) AS quantity,
		reading_count,
		toDateTime64(0, 9, 'UTC') AS published_before,
		toDateTime64(0, 9, 'UTC') AS snapshot_at
	FROM usage_raw_fragments
), period AS (
	SELECT
		is_publication,
		if(is_publication = 1, '', series_kind) AS series_kind,
		if(is_publication = 1, '', series_key) AS series_key,
		sum(quantity) AS quantity,
		sumIf(reading_count, is_publication = 0 AND (unit != %s OR measurement_method != %s)) AS row_incompatible_count,
		argMax(published_before, snapshot_at) AS selected_published_before,
		max(snapshot_at) AS selected_snapshot_at
	FROM combined
	GROUP BY is_publication, series_kind, series_key
), ranked AS (
	SELECT
		*,
		row_number() OVER (ORDER BY is_publication ASC, %s DESC, series_kind ASC, series_key ASC) AS series_rank,
		sum(row_incompatible_count) OVER () AS incompatible_count
	FROM period
)
SELECT is_publication, series_kind, series_key, incompatible_count, selected_published_before, selected_snapshot_at
FROM ranked
WHERE is_publication = 1 OR series_rank <= 6
ORDER BY is_publication ASC, series_rank ASC%s`,
		summaryScan,
		sqlLiteral(selection.Breakdown),
		sqlLiteral(selection.Unit),
		sqlLiteral(selection.MeasurementMethod),
		rankQuantity,
		usageQuerySettings,
	)
	rankCtx := clickhouse.Context(ctx, clickhouse.WithExternalTable(rawTable))
	rankRows, err := q.conn.Query(rankCtx, rankingQuery, summaryArgs...)
	if err != nil {
		return UsageResult{}, fmt.Errorf("rank meter usage series: %w", err)
	}
	topSeries := make([][2]string, 0, 6)
	var rankPublication usagePublication
	var rankIncompatible uint64
	for rankRows.Next() {
		var isPublication uint8
		var kind, key string
		var incompatible uint64
		var publishedBefore, snapshotAt time.Time
		if err := rankRows.Scan(&isPublication, &kind, &key, &incompatible, &publishedBefore, &snapshotAt); err != nil {
			o11y.NoLogDefer(func() error { return rankRows.Close() })
			return UsageResult{}, fmt.Errorf("scan ranked meter usage series: %w", err)
		}
		rankIncompatible = incompatible
		if isPublication == 1 {
			rankPublication = usagePublication{publishedBefore: publishedBefore, snapshotAt: snapshotAt}
		} else {
			topSeries = append(topSeries, [2]string{kind, key})
		}
	}
	if err := rankRows.Err(); err != nil {
		o11y.NoLogDefer(func() error { return rankRows.Close() })
		return UsageResult{}, fmt.Errorf("read ranked meter usage series: %w", err)
	}
	if err := rankRows.Close(); err != nil {
		return UsageResult{}, fmt.Errorf("close ranked meter usage series: %w", err)
	}
	if !sameUsagePublication(publication, rankPublication) {
		return UsageResult{}, fmt.Errorf("%w: publication changed while ranking", ErrUsageSummaryUnavailable)
	}
	if rankIncompatible > 0 {
		return UsageResult{}, ErrMixedMeasurement
	}

	topTable, err := ext.NewTable(
		"usage_top_series",
		ext.Column("series_kind", "String"),
		ext.Column("series_key", "String"),
	)
	if err != nil {
		return UsageResult{}, fmt.Errorf("create meter usage top series table: %w", err)
	}
	for _, identity := range topSeries {
		if err := topTable.Append(identity[0], identity[1]); err != nil {
			return UsageResult{}, fmt.Errorf("append meter usage top series: %w", err)
		}
	}

	dailySummaryScan, dailySummaryArgs, err := usageSummaryScan(params, summaryFrom, summaryTo, selection.Breakdown, "total")
	if err != nil {
		return UsageResult{}, err
	}

	dailyQuery := fmt.Sprintf(`
WITH combined AS (
	SELECT
		is_publication, facet = 'total' AS is_total, day, series_kind, series_key,
		label, unit, measurement_method, quantity, reading_count, published_before, snapshot_at
	FROM (%s)
	WHERE is_publication = 1 OR facet = 'total'
		OR (series_kind, series_key) IN (SELECT series_kind, series_key FROM usage_top_series)
	UNION ALL
	SELECT
		toUInt8(0), toUInt8(1), day, '', '', '', unit, measurement_method,
		toInt128(quantity), reading_count, toDateTime64(0, 9, 'UTC'), toDateTime64(0, 9, 'UTC')
	FROM usage_raw_fragments
	UNION ALL
	SELECT
		toUInt8(0), toUInt8(0), day, series_kind, series_key, label, unit, measurement_method,
		toInt128(quantity), reading_count, toDateTime64(0, 9, 'UTC'), toDateTime64(0, 9, 'UTC')
	FROM usage_raw_fragments
	WHERE %s != 'total'
		AND (series_kind, series_key) IN (SELECT series_kind, series_key FROM usage_top_series)
), daily AS (
	SELECT
		is_publication,
		is_total,
		if(is_publication = 1, toDate(0), day) AS result_day,
		if(is_publication = 1 OR is_total = 1, '', series_kind) AS result_kind,
		if(is_publication = 1 OR is_total = 1, '', series_key) AS result_key,
		max(label) AS result_label,
		sum(quantity) AS result_quantity,
		sum(reading_count) AS result_count,
		sumIf(reading_count, is_publication = 0 AND (unit != %s OR measurement_method != %s)) AS incompatible_count,
		argMax(published_before, snapshot_at) AS selected_published_before,
		max(snapshot_at) AS selected_snapshot_at
	FROM combined
	GROUP BY is_publication, is_total, result_day, result_kind, result_key
), checked AS (
	SELECT
		*,
		sumIf(result_quantity, is_total = 1) OVER day_window
			- sumIf(result_quantity, is_total = 0 AND is_publication = 0) OVER day_window AS remainder_quantity,
		sumIf(toInt128(result_count), is_total = 1) OVER day_window
			- sumIf(toInt128(result_count), is_total = 0 AND is_publication = 0) OVER day_window AS remainder_count,
		sum(incompatible_count) OVER () AS all_incompatible_count
	FROM daily
	WINDOW day_window AS (PARTITION BY result_day)
)
SELECT
	is_publication,
	result_day,
	%s AS unit,
	%s AS measurement_method,
	if(is_total = 1, if(%s = 'total', 'value', 'remainder'), result_kind) AS kind,
	if(is_total = 1 AND %s = 'total', 'total', result_key) AS key,
	if(is_total = 1, if(%s = 'total', 'Total', 'Other'), result_label) AS label,
	toString(if(is_total = 1 AND %s != 'total', remainder_quantity, result_quantity)) AS total,
	all_incompatible_count,
	selected_published_before,
	selected_snapshot_at
FROM checked
WHERE is_publication = 1 OR is_total = 0 OR %s = 'total' OR remainder_count > 0
ORDER BY is_publication ASC, result_day ASC, kind ASC, key ASC%s`,
		dailySummaryScan,
		sqlLiteral(selection.Breakdown),
		sqlLiteral(selection.Unit),
		sqlLiteral(selection.MeasurementMethod),
		sqlLiteral(selection.Unit),
		sqlLiteral(selection.MeasurementMethod),
		sqlLiteral(selection.Breakdown),
		sqlLiteral(selection.Breakdown),
		sqlLiteral(selection.Breakdown),
		sqlLiteral(selection.Breakdown),
		sqlLiteral(selection.Breakdown),
		usageQuerySettings,
	)
	dailyCtx := clickhouse.Context(ctx, clickhouse.WithExternalTable(rawTable, topTable))
	rows, err := q.conn.Query(dailyCtx, dailyQuery, dailySummaryArgs...)
	if err != nil {
		return UsageResult{}, fmt.Errorf("query daily meter usage: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	var dailyPublication usagePublication
	var incompatibleCount uint64
	for rows.Next() {
		var isPublication uint8
		var row UsageRow
		var publishedBefore, snapshotAt time.Time
		if err := rows.Scan(
			&isPublication,
			&row.Day,
			&row.Unit,
			&row.MeasurementMethod,
			&row.Kind,
			&row.Key,
			&row.Label,
			&row.Total,
			&incompatibleCount,
			&publishedBefore,
			&snapshotAt,
		); err != nil {
			return UsageResult{}, fmt.Errorf("scan daily meter usage row: %w", err)
		}
		if isPublication == 1 {
			dailyPublication = usagePublication{publishedBefore: publishedBefore, snapshotAt: snapshotAt}
			continue
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return UsageResult{}, fmt.Errorf("read daily meter usage rows: %w", err)
	}
	if !sameUsagePublication(publication, dailyPublication) {
		return UsageResult{}, fmt.Errorf("%w: publication changed while reading daily usage", ErrUsageSummaryUnavailable)
	}
	if incompatibleCount > 0 {
		return UsageResult{}, ErrMixedMeasurement
	}
	return result, nil
}

func (q *Queries) getUsagePublication(ctx context.Context) (usagePublication, error) {
	builder := sq.Select(
		"argMax(published_before, snapshot_at)",
		"max(snapshot_at)",
	).
		From("billing_meter_daily_summaries").
		Where(squirrel.Eq{"organization_id": "", "is_publication": 1, "family": "", "reading_kind": "", "facet": ""})
	query, args, err := builder.ToSql()
	if err != nil {
		return usagePublication{}, fmt.Errorf("build meter usage publication query: %w", err)
	}
	var publication usagePublication
	if err := q.conn.QueryRow(ctx, query, args...).Scan(&publication.publishedBefore, &publication.snapshotAt); err != nil {
		return usagePublication{}, fmt.Errorf("query meter usage publication: %w", err)
	}
	if publication.snapshotAt.UnixNano() == 0 {
		return usagePublication{}, fmt.Errorf("%w: publication is not ready", ErrUsageSummaryUnavailable)
	}
	if !publication.publishedBefore.Equal(utcUsageDay(publication.publishedBefore)) || publication.publishedBefore.After(utcUsageDay(time.Now().UTC())) {
		return usagePublication{}, fmt.Errorf("%w: invalid publication boundary", ErrUsageSummaryUnavailable)
	}
	return publication, nil
}

func (q *Queries) getUsageRawFragments(ctx context.Context, params UsageParams, facet usageFacetSQL, ranges []usageTimeRange, table *ext.Table) error {
	if len(ranges) == 0 {
		return nil
	}
	rangePredicates := make(squirrel.Or, 0, len(ranges))
	for _, timeRange := range ranges {
		rangePredicates = append(rangePredicates, squirrel.And{
			squirrel.GtOrEq{"occurred_at": timeRange.from},
			squirrel.Lt{"occurred_at": timeRange.to},
		})
	}
	builder := sq.Select(
		"toStartOfDay(occurred_at, 'UTC') AS day",
		facet.kind+" AS series_kind",
		facet.key+" AS series_key",
		"max("+facet.label+") AS label",
		"toString(unit) AS unit",
		"toString(measurement_method) AS measurement_method",
		"toString(sum(toInt128(value))) AS quantity",
		"count() AS reading_count",
	).
		From("billing_meter_readings_by_time FINAL").
		Where(squirrel.Eq{"organization_id": params.OrganizationID}).
		Where(squirrel.Eq{"meter_id": params.Selection.MeterIDs}).
		Where(squirrel.Eq{"reading_kind": params.ReadingKind}).
		Where(rangePredicates).
		GroupBy("day", "series_kind", "series_key", "unit", "measurement_method").
		Suffix(usageQuerySettings)
	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("build raw meter usage fragment query: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query raw meter usage fragments: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })
	for rows.Next() {
		var fragment usageRawFragment
		if err := rows.Scan(
			&fragment.day,
			&fragment.seriesKind,
			&fragment.seriesKey,
			&fragment.label,
			&fragment.unit,
			&fragment.measurementMethod,
			&fragment.quantity,
			&fragment.readingCount,
		); err != nil {
			return fmt.Errorf("scan raw meter usage fragment: %w", err)
		}
		if err := table.Append(fragment.day, fragment.seriesKind, fragment.seriesKey, fragment.label, fragment.unit, fragment.measurementMethod, fragment.quantity, fragment.readingCount); err != nil {
			return fmt.Errorf("append meter usage raw fragment: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read raw meter usage fragments: %w", err)
	}
	return nil
}

func usageSummaryScan(params UsageParams, from, to time.Time, facets ...string) (string, []any, error) {
	var dataPredicate = squirrel.Expr("0")
	if from.Before(to) {
		dataPredicate = squirrel.And{
			squirrel.Eq{
				"organization_id":  params.OrganizationID,
				"family":           params.Selection.Family,
				"reading_kind":     params.ReadingKind,
				"facet":            facets,
				"s.is_publication": 0,
			},
			squirrel.GtOrEq{"day": from},
			squirrel.Lt{"day": to},
		}
	}
	builder := sq.Select(
		"toUInt8(s.is_publication != 0) AS is_publication",
		"facet",
		"day",
		"if(is_publication = 1, '', toString(series_kind)) AS series_kind",
		"if(is_publication = 1, '', series_key) AS series_key",
		"if(is_publication = 1, '', label) AS label",
		"if(is_publication = 1, '', toString(unit)) AS unit",
		"if(is_publication = 1, '', toString(measurement_method)) AS measurement_method",
		"quantity",
		"reading_count",
		"published_before",
		"snapshot_at",
	).
		From("billing_meter_daily_summaries AS s").
		Where(squirrel.Or{
			squirrel.And{
				squirrel.Eq{"organization_id": "", "s.is_publication": 1, "family": "", "reading_kind": "", "facet": ""},
			},
			dataPredicate,
		})
	query, args, err := builder.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build meter usage summary scan: %w", err)
	}
	return query, args, nil
}

func usageCoverage(from, to, publishedBefore time.Time) (time.Time, time.Time, []usageTimeRange) {
	firstFullDay := utcUsageDay(from)
	if from.After(firstFullDay) {
		firstFullDay = firstFullDay.AddDate(0, 0, 1)
	}
	lastFullDay := utcUsageDay(to)
	summaryTo := lastFullDay
	if publishedBefore.Before(summaryTo) {
		summaryTo = publishedBefore
	}
	if !firstFullDay.Before(summaryTo) {
		return firstFullDay, firstFullDay, []usageTimeRange{{from: from, to: to}}
	}
	ranges := make([]usageTimeRange, 0, 2)
	if from.Before(firstFullDay) {
		ranges = append(ranges, usageTimeRange{from: from, to: firstFullDay})
	}
	if summaryTo.Before(to) {
		ranges = append(ranges, usageTimeRange{from: summaryTo, to: to})
	}
	return firstFullDay, summaryTo, ranges
}

func usagePastTailDays(from, to, publishedBefore, now time.Time) int {
	start := from
	if publishedBefore.After(start) {
		start = publishedBefore
	}
	end := to
	today := utcUsageDay(now)
	if today.Before(end) {
		end = today
	}
	if !start.Before(end) {
		return 0
	}
	firstDay := utcUsageDay(start)
	lastDay := utcUsageDay(end.Add(-time.Nanosecond))
	return int(lastDay.Sub(firstDay)/(24*time.Hour)) + 1
}

func utcUsageDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func sameUsagePublication(left, right usagePublication) bool {
	return left.publishedBefore.Equal(right.publishedBefore) && left.snapshotAt.Equal(right.snapshotAt)
}
