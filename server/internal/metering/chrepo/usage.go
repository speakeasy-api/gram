package chrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

	// UsageFacetIdentityLabel groups by one identity attribute and displays another.
	UsageFacetIdentityLabel UsageFacetKind = "identity_label"

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
			"billing_user_account_email", "billing_user_cost_center_name", "billing_user_department_name",
			"billing_user_directory_groups", "billing_user_division_name", "billing_user_employee_type",
			"billing_user_id", "billing_user_job_title", "billing_user_rbac_roles",
			"custom_domain",
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
	case UsageFacetIdentityLabel:
		if facet.Attribute == "" || facet.SecondaryAttribute != "" || facet.LabelAttribute == "" || len(facet.Values) != 0 {
			return usageFacetSQL{}, ErrInvalidUsageSelection
		}
		return identityLabelFacet(facet.Attribute, facet.LabelAttribute), nil
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

func identityLabelFacet(identityAttribute, labelAttribute string) usageFacetSQL {
	identity := promotedAttribute(identityAttribute)
	label := promotedAttribute(labelAttribute)
	return usageFacetSQL{
		kind:  fmt.Sprintf("if(%s = '', 'unset', 'value')", identity),
		key:   identity,
		label: fmt.Sprintf("if(%s = '', '(unset)', if(%s = '', %s, %s))", identity, label, identity, label),
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

// GetUsage returns a fixed-period top-six facet aggregation. The query reads
// only the selected organization, exact meters, and [from,to) occurrence range;
// FINAL removes physical redeliveries before any sums are computed.
// Promoted attributes avoid reading the wide map. Bound parallel FINAL readers
// and block sizes to limit per-request buffers across concurrent reports.
func (q *Queries) GetUsage(ctx context.Context, params UsageParams) (UsageResult, error) {
	selection := params.Selection
	if len(selection.MeterIDs) == 0 || selection.Unit == "" || selection.MeasurementMethod == "" {
		return UsageResult{}, ErrInvalidUsageSelection
	}
	facet, err := buildUsageFacetSQL(selection.Facet)
	if err != nil {
		return UsageResult{}, err
	}
	if params.ReadingKind != ReadingKindUsage && params.ReadingKind != ReadingKindAdjustment {
		return UsageResult{}, ErrInvalidReadingKind
	}

	daily := sq.Select(
		"toStartOfDay(occurred_at, 'UTC') AS day",
		facet.kind+" AS facet_kind",
		facet.key+" AS facet_key",
		"max("+facet.label+") AS facet_label",
		"sum(toInt128(value)) AS quantity",
		fmt.Sprintf("countIf(toString(unit) != %s OR toString(measurement_method) != %s) AS incompatible_count", sqlLiteral(selection.Unit), sqlLiteral(selection.MeasurementMethod)),
	).
		From("billing_meter_readings_by_time FINAL").
		Where(squirrel.Eq{"organization_id": params.OrganizationID}).
		Where(squirrel.Eq{"meter_id": selection.MeterIDs}).
		Where("occurred_at >= ?", params.From.UTC()).
		Where("occurred_at < ?", params.To.UTC()).
		Where(squirrel.Eq{"reading_kind": params.ReadingKind}).
		GroupBy("day", "facet_kind", "facet_key")

	periodized := sq.Select(
		"day", "facet_kind", "facet_key", "facet_label", "quantity", "incompatible_count",
		"sum(quantity) OVER (PARTITION BY facet_kind, facet_key) AS series_total",
	).FromSelect(daily, "daily_usage")

	rankQuantity := "series_total"
	if params.ReadingKind == ReadingKindAdjustment {
		rankQuantity = "abs(series_total)"
	}
	ranked := sq.Select(
		"day", "facet_kind", "facet_key", "facet_label", "quantity", "incompatible_count",
		"dense_rank() OVER (ORDER BY "+rankQuantity+" DESC, facet_kind ASC, facet_key ASC) AS facet_rank",
	).FromSelect(periodized, "periodized_usage")

	builder := sq.Select(
		"day",
		sqlLiteral(selection.Unit)+" AS unit",
		sqlLiteral(selection.MeasurementMethod)+" AS measurement_method",
		"if(facet_rank <= 6, facet_kind, 'remainder') AS result_kind",
		"if(facet_rank <= 6, facet_key, '') AS result_key",
		"if(facet_rank <= 6, facet_label, 'Other') AS result_label",
		"toString(sum(quantity)) AS total",
		"sum(incompatible_count) AS result_incompatible_count",
	).
		FromSelect(ranked, "ranked_usage").
		GroupBy("day", "result_kind", "result_key", "result_label").
		OrderBy("day ASC", "result_kind ASC", "result_key ASC").
		Suffix("SETTINGS do_not_merge_across_partitions_select_final = 1, max_threads = 2, max_final_threads = 2, max_block_size = 1024")

	query, args, err := builder.ToSql()
	if err != nil {
		return UsageResult{}, fmt.Errorf("build meter usage query: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return UsageResult{}, fmt.Errorf("query meter usage: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	result := UsageResult{Unit: selection.Unit, MeasurementMethod: selection.MeasurementMethod, Rows: make([]UsageRow, 0)}
	for rows.Next() {
		var row UsageRow
		var incompatibleCount uint64
		if err := rows.Scan(&row.Day, &row.Unit, &row.MeasurementMethod, &row.Kind, &row.Key, &row.Label, &row.Total, &incompatibleCount); err != nil {
			return UsageResult{}, fmt.Errorf("scan meter usage row: %w", err)
		}
		if incompatibleCount > 0 {
			return UsageResult{}, ErrMixedMeasurement
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return UsageResult{}, fmt.Errorf("read meter usage rows: %w", err)
	}
	return result, nil
}
