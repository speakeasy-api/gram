package chrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

var (
	// ErrInvalidUsageSelection reports an incomplete internal selection.
	ErrInvalidUsageSelection = errors.New("invalid meter usage selection")

	// ErrInvalidUsageRange reports a range whose boundaries are not distinct UTC days.
	ErrInvalidUsageRange = errors.New("invalid meter usage range")

	// ErrMixedMeasurement reports rows that violate their family's fixed measurement contract.
	ErrMixedMeasurement = errors.New("meter usage contains incompatible units or measurement methods")
)

// UsageSelection is a family-compatible summary and measurement selection.
type UsageSelection struct {
	// Family is the canonical stored summary family.
	Family string

	// Breakdown is the canonical stored summary facet.
	Breakdown string

	// Unit is the sole unit allowed in selected readings.
	Unit string

	// MeasurementMethod is the sole measurement method allowed in selected readings.
	MeasurementMethod string
}

// UsageParams defines one organization-owned bounded ordinary usage report.
type UsageParams struct {
	// OrganizationID identifies the owning organization.
	OrganizationID string

	// Selection fixes the summary family, facet, and measurement contract.
	Selection UsageSelection

	// From is the inclusive UTC-day boundary.
	From time.Time

	// To is the exclusive UTC-day boundary.
	To time.Time
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

	// Total is an exact integer ordinary usage quantity encoded in base ten.
	Total string
}

// UsageResult contains compatible measurement metadata and bounded ordinary usage aggregates.
type UsageResult struct {
	// Unit is the fixed family unit, including for empty results.
	Unit string

	// MeasurementMethod is the fixed family method, including for empty results.
	MeasurementMethod string

	// Rows contains at most seven aggregates per intersected UTC day.
	Rows []UsageRow
}

const usageQuery = `
WITH top_series AS (
	SELECT series_kind, series_key
	FROM billing_meter_daily_summaries
	PREWHERE organization_id = ?
		AND family = ?
		AND reading_kind = 'usage'
		AND facet = ?
	WHERE day >= toDate(?) AND day < toDate(?)
	GROUP BY series_kind, series_key
	HAVING sum(reading_count) != 0
	ORDER BY
		sum(quantity) DESC,
		series_kind ASC,
		series_key ASC
	LIMIT 6
), classified AS (
	SELECT
		day,
		multiIf(
			? = 'total', tuple('value', 'total', 'Total'),
			(series_kind, series_key) IN (SELECT series_kind, series_key FROM top_series),
				tuple(toString(series_kind), series_key, label),
			tuple('remainder', '', 'Other')
		) AS bucket,
		unit,
		measurement_method,
		quantity,
		reading_count
	FROM billing_meter_daily_summaries
	PREWHERE organization_id = ?
		AND family = ?
		AND reading_kind = 'usage'
		AND facet = ?
	WHERE day >= toDate(?) AND day < toDate(?)
), daily AS (
	SELECT
		day,
		bucket.1 AS d_kind,
		bucket.2 AS d_key,
		if(d_kind = 'remainder', 'Other', max(bucket.3)) AS d_label,
		sum(quantity) AS d_quantity,
		sum(reading_count) AS d_reading_count,
		sumIf(reading_count, unit != ? OR measurement_method != ?) AS d_incompatible_count
	FROM classified
	GROUP BY day, d_kind, d_key
	HAVING d_reading_count != 0
)
SELECT
	day,
	? AS unit,
	? AS measurement_method,
	d_kind AS kind,
	d_key AS key,
	d_label AS label,
	toString(d_quantity) AS total,
	sum(d_incompatible_count) OVER () AS incompatible_count
FROM daily
ORDER BY day ASC, kind ASC, key ASC
SETTINGS
	optimize_aggregation_in_order = 1,
	aggregation_in_order_max_block_bytes = 1048576,
	max_bytes_before_remerge_sort = 1048576,
	remerge_sort_lowered_memory_bytes_ratio = 0,
	read_in_order_two_level_merge_threshold = 2,
	max_streams_for_merge_tree_reading = 2,
	max_threads = 2,
	max_block_size = 1024,
	max_memory_usage = 536870912,
	max_rows_to_read = 50000000,
	max_bytes_to_read = 8589934592,
	max_execution_time = 30,
	timeout_before_checking_execution_speed = 0,
	max_bytes_before_external_group_by = 67108864,
	max_bytes_before_external_sort = 67108864`

// GetUsage returns a period-ranked top-six daily ordinary usage aggregation
// from the incremental UTC-day summary. Physical summary rows are always summed
// because background SummingMergeTree merges need not have completed before the read.
func (q *Queries) GetUsage(ctx context.Context, params UsageParams) (UsageResult, error) {
	selection := params.Selection
	if params.OrganizationID == "" || selection.Family == "" || selection.Breakdown == "" || selection.Unit == "" || selection.MeasurementMethod == "" {
		return UsageResult{}, ErrInvalidUsageSelection
	}

	result := UsageResult{
		Unit:              selection.Unit,
		MeasurementMethod: selection.MeasurementMethod,
		Rows:              make([]UsageRow, 0),
	}
	if !isUTCUsageDay(params.From) || !isUTCUsageDay(params.To) || !params.From.Before(params.To) {
		return UsageResult{}, ErrInvalidUsageRange
	}

	rows, err := q.conn.Query(ctx, usageQuery,
		params.OrganizationID,
		selection.Family,
		selection.Breakdown,
		params.From,
		params.To,
		selection.Breakdown,
		params.OrganizationID,
		selection.Family,
		selection.Breakdown,
		params.From,
		params.To,
		selection.Unit,
		selection.MeasurementMethod,
		selection.Unit,
		selection.MeasurementMethod,
	)
	if err != nil {
		return UsageResult{}, fmt.Errorf("query daily meter usage: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	var incompatibleCount int64
	for rows.Next() {
		var row UsageRow
		if err := rows.Scan(
			&row.Day,
			&row.Unit,
			&row.MeasurementMethod,
			&row.Kind,
			&row.Key,
			&row.Label,
			&row.Total,
			&incompatibleCount,
		); err != nil {
			return UsageResult{}, fmt.Errorf("scan daily meter usage row: %w", err)
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return UsageResult{}, fmt.Errorf("read daily meter usage rows: %w", err)
	}
	if incompatibleCount != 0 {
		return UsageResult{}, ErrMixedMeasurement
	}
	return result, nil
}

func isUTCUsageDay(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	utc := value.UTC()
	return value.Equal(time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC))
}
