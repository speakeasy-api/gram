package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// SpendParams defines one organization-owned bounded spend-summary read.
type SpendParams struct {
	// OrganizationID identifies the owning organization.
	OrganizationID string

	// From is the inclusive UTC-day boundary.
	From time.Time

	// To is the exclusive UTC-day boundary.
	To time.Time
}

// SpendRow is one exact daily product quantity from the ordinary summaries.
type SpendRow struct {
	// Day is the UTC calendar day containing this aggregate.
	Day time.Time

	// ProductID is agent_session_storage, risk_content_scans, or mcp_egress.
	ProductID string

	// Quantity is an exact integer ordinary usage quantity encoded in base ten.
	Quantity string
}

const spendQuery = `
WITH daily AS (
	SELECT
		day,
		if(family = 'mcp_bandwidth', 'mcp_egress', family) AS product_id,
		sum(quantity) AS d_quantity,
		sum(reading_count) AS d_reading_count,
		sumIf(
			reading_count,
			(family IN ('agent_session_storage', 'risk_content_scans') AND (unit != 'stokens' OR measurement_method != 'tiktoken_o200k_base'))
			OR (family = 'mcp_bandwidth' AND (unit != 'bytes' OR measurement_method != 'http_body_bytes'))
		) AS d_incompatible_count
	FROM billing_meter_daily_summaries
	PREWHERE organization_id = ?
		AND reading_kind = 'usage'
		AND family IN ('agent_session_storage', 'risk_content_scans', 'mcp_bandwidth')
	WHERE day >= toDate(?) AND day < toDate(?)
		AND (
			(family IN ('agent_session_storage', 'risk_content_scans') AND facet = 'total' AND series_kind = 'value' AND series_key = 'total')
			OR (family = 'mcp_bandwidth' AND facet = 'direction' AND series_kind = 'value' AND series_key = 'egress')
		)
	GROUP BY day, product_id
	HAVING d_reading_count != 0
)
SELECT
	day,
	product_id,
	toString(d_quantity) AS quantity,
	sum(d_incompatible_count) OVER () AS incompatible_count
FROM daily
ORDER BY day ASC, product_id ASC
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

// GetSpend returns daily ordinary quantities for the three priced products from
// the incremental summary. Physical summary rows are summed because background
// SummingMergeTree merges need not have completed before the read.
func (q *Queries) GetSpend(ctx context.Context, params SpendParams) ([]SpendRow, error) {
	if params.OrganizationID == "" {
		return nil, ErrInvalidUsageSelection
	}
	if !isUTCUsageDay(params.From) || !isUTCUsageDay(params.To) || !params.From.Before(params.To) {
		return nil, ErrInvalidUsageRange
	}

	rows, err := q.conn.Query(ctx, spendQuery, params.OrganizationID, params.From, params.To)
	if err != nil {
		return nil, fmt.Errorf("query daily meter spend quantities: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	result := make([]SpendRow, 0)
	var incompatibleCount int64
	for rows.Next() {
		var row SpendRow
		if err := rows.Scan(&row.Day, &row.ProductID, &row.Quantity, &incompatibleCount); err != nil {
			return nil, fmt.Errorf("scan daily meter spend quantity: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read daily meter spend quantities: %w", err)
	}
	if incompatibleCount != 0 {
		return nil, ErrMixedMeasurement
	}
	return result, nil
}
