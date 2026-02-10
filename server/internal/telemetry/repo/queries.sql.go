package repo

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
)

// sq is the squirrel statement builder pre-configured for ClickHouse (uses ? placeholders).
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// InsertTelemetryLogParams contains the parameters for inserting a telemetry log.
type InsertTelemetryLogParams struct {
	ID                   string
	TimeUnixNano         int64
	ObservedTimeUnixNano int64
	SeverityText         *string
	Body                 string
	TraceID              *string
	SpanID               *string
	Attributes           string
	ResourceAttributes   string
	GramProjectID        string
	GramDeploymentID     *string
	GramFunctionID       *string
	GramURN              string
	ServiceName          string
	ServiceVersion       *string
	GramChatID           *string
}

// InsertTelemetryLog inserts a telemetry log record into ClickHouse.
//
// Original SQL reference:
// INSERT INTO telemetry_logs (id, time_unix_nano, ...) VALUES (?, ?, ...)
//
//nolint:wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) InsertTelemetryLog(ctx context.Context, arg InsertTelemetryLogParams) error {
	ctx = clickhouse.Context(ctx, clickhouse.WithAsync(false))

	query, args, err := sq.Insert("telemetry_logs").
		Columns(
			"id",
			"time_unix_nano",
			"observed_time_unix_nano",
			"severity_text",
			"body",
			"trace_id",
			"span_id",
			"attributes",
			"resource_attributes",
			"gram_project_id",
			"gram_deployment_id",
			"gram_function_id",
			"gram_urn",
			"service_name",
			"service_version",
			"gram_chat_id",
		).
		Values(
			arg.ID,
			arg.TimeUnixNano,
			arg.ObservedTimeUnixNano,
			arg.SeverityText,
			arg.Body,
			arg.TraceID,
			arg.SpanID,
			arg.Attributes,
			arg.ResourceAttributes,
			arg.GramProjectID,
			arg.GramDeploymentID,
			arg.GramFunctionID,
			arg.GramURN,
			arg.ServiceName,
			arg.ServiceVersion,
			arg.GramChatID,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("building insert query: %w", err)
	}

	return q.conn.Exec(ctx, query, args...)
}

// ListTelemetryLogsParams contains the parameters for listing telemetry logs.
type ListTelemetryLogsParams struct {
	GramProjectID          string
	TimeStart              int64
	TimeEnd                int64
	GramURNs               []string // Supports multiple URNs
	TraceID                string
	GramDeploymentID       string
	GramFunctionID         string
	SeverityText           string
	HTTPResponseStatusCode int32
	HTTPRoute              string
	HTTPRequestMethod      string
	ServiceName            string
	GramChatID             string
	UserID                 string
	ExternalUserID         string
	SortOrder              string
	Cursor                 string
	Limit                  int
}

// ListTelemetryLogs retrieves telemetry logs with optional filters and cursor pagination.
//
// Original SQL reference:
// SELECT id, time_unix_nano, ... FROM telemetry_logs
// WHERE gram_project_id = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
// [+ optional filters] ORDER BY time_unix_nano LIMIT ?
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListTelemetryLogs(ctx context.Context, arg ListTelemetryLogsParams) ([]TelemetryLog, error) {
	sb := sq.Select(
		"id",
		"time_unix_nano",
		"observed_time_unix_nano",
		"severity_text",
		"body",
		"trace_id",
		"span_id",
		"toString(attributes) as attributes",
		"toString(resource_attributes) as resource_attributes",
		"gram_project_id",
		"gram_deployment_id",
		"gram_function_id",
		"gram_urn",
		"service_name",
		"service_version",
		"gram_chat_id",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	// Optional filters
	if len(arg.GramURNs) > 0 {
		sb = sb.Where("has(?, gram_urn)", arg.GramURNs)
	}
	if arg.TraceID != "" {
		sb = sb.Where(squirrel.Eq{"trace_id": arg.TraceID})
	}
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}
	if arg.GramFunctionID != "" {
		sb = sb.Where("gram_function_id = toUUIDOrNull(?)", arg.GramFunctionID)
	}
	if arg.SeverityText != "" {
		sb = sb.Where(squirrel.Eq{"severity_text": arg.SeverityText})
	}
	if arg.HTTPResponseStatusCode != 0 {
		sb = sb.Where("toInt32OrZero(toString(attributes.http.response.status_code)) = ?", arg.HTTPResponseStatusCode)
	}
	if arg.HTTPRoute != "" {
		sb = sb.Where("toString(attributes.http.route) = ?", arg.HTTPRoute)
	}
	if arg.HTTPRequestMethod != "" {
		sb = sb.Where("toString(attributes.http.request.method) = ?", arg.HTTPRequestMethod)
	}
	if arg.ServiceName != "" {
		sb = sb.Where(squirrel.Eq{"service_name": arg.ServiceName})
	}
	if arg.GramChatID != "" {
		sb = sb.Where(squirrel.Eq{"gram_chat_id": arg.GramChatID})
	}
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{"user_id": arg.UserID})
	}
	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}

	sb = withPagination(sb, arg.Cursor, arg.SortOrder)

	sb = withOrdering(sb, arg.SortOrder, "time_unix_nano", "toUUID(id)")

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list logs query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TelemetryLog
	for rows.Next() {
		var log TelemetryLog
		if err = rows.ScanStruct(&log); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		items = append(items, log)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// ListTracesParams contains the parameters for listing traces.
type ListTracesParams struct {
	GramProjectID    string
	TimeStart        int64
	TimeEnd          int64
	GramDeploymentID string
	GramFunctionID   string
	GramURN          string // Single URN filter (supports substring matching)
	SortOrder        string
	Cursor           string // trace_id to paginate from
	Limit            int
}

// ListTraces retrieves aggregated trace summaries grouped by trace_id.
//
// Original SQL reference:
// SELECT trace_id, min(time_unix_nano), count(*), ... FROM telemetry_logs
// WHERE gram_project_id = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
// [+ optional filters] GROUP BY trace_id ORDER BY start_time_unix_nano LIMIT ?
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListTraces(ctx context.Context, arg ListTracesParams) ([]TraceSummary, error) {
	sb := sq.Select(
		"trace_id",
		"min(start_time_unix_nano) as start_time_unix_nano",
		"sum(log_count) as log_count",
		"anyIfMerge(http_status_code) as http_status_code",
		"any(gram_urn) as gram_urn",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("trace_summaries.start_time_unix_nano >= ?", arg.TimeStart).
		Where("trace_summaries.start_time_unix_nano <= ?", arg.TimeEnd).
		Having("start_time_unix_nano >= ?", arg.TimeStart).
		Having("start_time_unix_nano <= ?", arg.TimeEnd)

	// Optional filters
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}
	if arg.GramFunctionID != "" {
		sb = sb.Where("gram_function_id = toUUIDOrNull(?)", arg.GramFunctionID)
	}

	// URN filter must use HAVING because gram_urn in SELECT is aliased to any(gram_urn),
	// and ClickHouse resolves aliases in WHERE, which would create an invalid aggregate-in-WHERE.
	if arg.GramURN != "" {
		sb = sb.Having("position(gram_urn, ?) > 0", arg.GramURN)
	}

	sb = sb.GroupBy("trace_id")

	sb = withHavingPagination(
		sb,
		arg.Cursor,
		arg.SortOrder,
		arg.GramProjectID,
		"trace_id",
		"start_time_unix_nano",
		"min(start_time_unix_nano)",
		TableNameTraceSummaries,
	)

	sb = withOrdering(sb, arg.SortOrder, "start_time_unix_nano", "")

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list traces query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var traces []TraceSummary
	for rows.Next() {
		var trace TraceSummary
		if err = rows.ScanStruct(&trace); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		traces = append(traces, trace)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return traces, nil
}

// GetMetricsSummaryParams contains the parameters for getting metrics summary.
type GetMetricsSummaryParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
}

// GetMetricsSummary retrieves aggregate metrics for a project within a time range.
//
// Original SQL reference:
// SELECT [aggregation functions] FROM telemetry_logs
// WHERE gram_project_id = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetMetricsSummary(ctx context.Context, arg GetMetricsSummaryParams) (*MetricsSummaryRow, error) {
	sb := sq.Select(
		// Activity timestamps
		"min(time_unix_nano) AS first_seen_unix_nano",
		"max(time_unix_nano) AS last_seen_unix_nano",

		// Cardinality (exclude empty strings)
		"uniqExactIf(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats",
		"uniqExactIf(toString(attributes.gen_ai.response.model), toString(attributes.gen_ai.response.model) != '') AS distinct_models",
		"uniqExactIf(toString(attributes.gen_ai.provider.name), toString(attributes.gen_ai.provider.name) != '') AS distinct_providers",

		// Token metrics (from chat completion events)
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_tokens",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS avg_tokens_per_request",

		// Chat request metrics
		"countIf(toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_chat_requests",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS avg_chat_duration_ms",

		// Resolution status
		"countIf(position(toString(attributes.gen_ai.response.finish_reasons), 'stop') > 0) AS finish_reason_stop",
		"countIf(position(toString(attributes.gen_ai.response.finish_reasons), 'tool_calls') > 0) AS finish_reason_tool_calls",

		// Tool call metrics
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_call_success",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_call_failure",
		"avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS avg_tool_duration_ms",

		// Chat resolution metrics (from AI evaluation of chat outcomes)
		"countIf(evaluation_score_label = 'success') AS chat_resolution_success",
		"countIf(evaluation_score_label = 'failure') AS chat_resolution_failure",
		"countIf(evaluation_score_label = 'partial') AS chat_resolution_partial",
		"countIf(evaluation_score_label = 'abandoned') AS chat_resolution_abandoned",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.evaluation.score.value)), evaluation_score_label != '') AS avg_chat_resolution_score",

		// Model breakdown (map of model name -> count)
		"sumMapIf(map(toString(attributes.gen_ai.response.model), toUInt64(1)), toString(attributes.gram.resource.urn) = 'agents:chat:completion' AND toString(attributes.gen_ai.response.model) != '') AS models",

		// Tool breakdowns (maps of tool URN -> count)
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:')) AS tool_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_success_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_failure_counts",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building metrics summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		// Return empty metrics if no rows
		return &MetricsSummaryRow{
			FirstSeenUnixNano:       0,
			LastSeenUnixNano:        0,
			TotalChats:              0,
			DistinctModels:          0,
			DistinctProviders:       0,
			TotalInputTokens:        0,
			TotalOutputTokens:       0,
			TotalTokens:             0,
			AvgTokensPerReq:         0,
			TotalChatRequests:       0,
			AvgChatDurationMs:       0,
			FinishReasonStop:        0,
			FinishReasonToolCalls:   0,
			TotalToolCalls:          0,
			ToolCallSuccess:         0,
			ToolCallFailure:         0,
			AvgToolDurationMs:       0,
			ChatResolutionSuccess:   0,
			ChatResolutionFailure:   0,
			ChatResolutionPartial:   0,
			ChatResolutionAbandoned: 0,
			AvgChatResolutionScore:  0,
			Models:                  make(map[string]uint64),
			ToolCounts:              make(map[string]uint64),
			ToolSuccessCounts:       make(map[string]uint64),
			ToolFailureCounts:       make(map[string]uint64),
		}, nil
	}

	var metrics MetricsSummaryRow
	if err = rows.ScanStruct(&metrics); err != nil {
		return nil, fmt.Errorf("error scanning row: %w", err)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return &metrics, nil
}

// GetTimeSeriesMetricsParams contains the parameters for getting time series metrics.
type GetTimeSeriesMetricsParams struct {
	GramProjectID   string
	TimeStart       int64
	TimeEnd         int64
	IntervalSeconds int64  // Bucket interval in seconds
	ExternalUserID  string // Optional filter
	APIKeyID        string // Optional filter
}

// GetTimeSeriesMetrics retrieves time-bucketed metrics for the observability overview charts.
// Returns buckets for the entire requested time range, with zeros for periods without data.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetTimeSeriesMetrics(ctx context.Context, arg GetTimeSeriesMetricsParams) ([]TimeSeriesBucket, error) {
	// Calculate the number of buckets needed
	intervalNanos := arg.IntervalSeconds * 1_000_000_000
	// Align start time to interval boundary
	alignedStart := (arg.TimeStart / intervalNanos) * intervalNanos

	// Build the query with a generated time series that covers the full range
	// This ensures we get buckets even for periods with no data
	query := fmt.Sprintf(`
		WITH
			-- Generate all bucket timestamps for the requested range
			buckets AS (
				SELECT toInt64(%d + (number * %d)) AS bucket_time_unix_nano
				FROM numbers(toUInt64(ceil((%d - %d) / %d)) + 1)
				WHERE %d + (number * %d) <= %d
			),
			-- Aggregate actual data by bucket
			data AS (
				SELECT
					toInt64(toStartOfInterval(fromUnixTimestamp64Nano(time_unix_nano), INTERVAL %d SECOND)) * 1000000000 as bucket_time_unix_nano,
					uniqExactIf(gram_chat_id, gram_chat_id != '' AND evaluation_score_label != '') as total_chats,
					uniqExactIf(gram_chat_id, gram_chat_id != '' AND evaluation_score_label = 'success') as resolved_chats,
					uniqExactIf(gram_chat_id, gram_chat_id != '' AND evaluation_score_label = 'failure') as failed_chats,
					uniqExactIf(gram_chat_id, gram_chat_id != '' AND evaluation_score_label = 'partial') as partial_chats,
					uniqExactIf(gram_chat_id, gram_chat_id != '' AND evaluation_score_label = 'abandoned') as abandoned_chats,
					countIf(startsWith(gram_urn, 'tools:')) as total_tool_calls,
					countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) as failed_tool_calls,
					avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:')) as avg_tool_latency_ms,
					avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gram.resource.urn) = 'agents:chat:completion') as avg_session_duration_ms
				FROM telemetry_logs
				WHERE gram_project_id = ?
					AND time_unix_nano >= ?
					AND time_unix_nano <= ?
					%s
				GROUP BY bucket_time_unix_nano
			)
		SELECT
			b.bucket_time_unix_nano,
			coalesce(d.total_chats, 0) as total_chats,
			coalesce(d.resolved_chats, 0) as resolved_chats,
			coalesce(d.failed_chats, 0) as failed_chats,
			coalesce(d.partial_chats, 0) as partial_chats,
			coalesce(d.abandoned_chats, 0) as abandoned_chats,
			coalesce(d.total_tool_calls, 0) as total_tool_calls,
			coalesce(d.failed_tool_calls, 0) as failed_tool_calls,
			coalesce(d.avg_tool_latency_ms, 0) as avg_tool_latency_ms,
			coalesce(d.avg_session_duration_ms, 0) as avg_session_duration_ms
		FROM buckets b
		LEFT JOIN data d ON b.bucket_time_unix_nano = d.bucket_time_unix_nano
		ORDER BY b.bucket_time_unix_nano ASC
	`,
		alignedStart, intervalNanos, // First bucket and interval for generation
		arg.TimeEnd, alignedStart, intervalNanos, // For calculating number of buckets
		alignedStart, intervalNanos, arg.TimeEnd, // WHERE clause for bucket generation
		arg.IntervalSeconds, // INTERVAL for data aggregation
		buildOptionalFiltersSQL(arg.ExternalUserID, arg.APIKeyID), // Optional filters - parameterized
	)

	queryArgs := []any{arg.GramProjectID, arg.TimeStart, arg.TimeEnd}
	// Append optional filter values as parameterized args (in order)
	if arg.ExternalUserID != "" {
		queryArgs = append(queryArgs, arg.ExternalUserID)
	}
	if arg.APIKeyID != "" {
		queryArgs = append(queryArgs, arg.APIKeyID)
	}

	rows, err := q.conn.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []TimeSeriesBucket
	for rows.Next() {
		var bucket TimeSeriesBucket
		if err = rows.ScanStruct(&bucket); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		buckets = append(buckets, bucket)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return buckets, nil
}

// buildOptionalFiltersSQL creates the WHERE clause additions for optional filters using parameterized placeholders.
// The caller must append the corresponding values to queryArgs in the same order.
func buildOptionalFiltersSQL(externalUserID, apiKeyID string) string {
	var filters string
	if externalUserID != "" {
		filters += " AND external_user_id = ?"
	}
	if apiKeyID != "" {
		filters += " AND api_key_id = ?"
	}
	return filters
}

// GetToolMetricsBreakdownParams contains the parameters for getting tool metrics breakdown.
type GetToolMetricsBreakdownParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	ExternalUserID string // Optional filter
	APIKeyID       string // Optional filter
	Limit          int
	SortBy         string // "count" or "failure_rate"
}

// GetToolMetricsBreakdown retrieves per-tool aggregated metrics for top tools tables.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetToolMetricsBreakdown(ctx context.Context, arg GetToolMetricsBreakdownParams) ([]ToolMetric, error) {
	sb := sq.Select(
		"gram_urn",
		"count(*) as call_count",
		"countIf(toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) as success_count",
		"countIf(toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) as failure_count",
		"avg(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000) as avg_latency_ms",
		"countIf(toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) / greatest(toFloat64(count(*)), 1) as failure_rate",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("startsWith(gram_urn, 'tools:')")

	// Optional filters
	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}

	sb = sb.GroupBy("gram_urn")

	// Sort by count or failure rate
	if arg.SortBy == "failure_rate" {
		sb = sb.OrderBy("failure_rate DESC", "call_count DESC")
	} else {
		sb = sb.OrderBy("call_count DESC")
	}

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool metrics query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tools []ToolMetric
	for rows.Next() {
		var tool ToolMetric
		if err = rows.ScanStruct(&tool); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		tools = append(tools, tool)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return tools, nil
}

// GetOverviewSummaryParams contains the parameters for getting overview summary metrics.
type GetOverviewSummaryParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	ExternalUserID string // Optional filter
	APIKeyID       string // Optional filter
}

// GetOverviewSummary retrieves aggregated summary metrics for the observability overview.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetOverviewSummary(ctx context.Context, arg GetOverviewSummaryParams) (*OverviewSummary, error) {
	sb := sq.Select(
		// Chat metrics - count only chats with resolution analysis for accurate resolution rate
		// total_chats = chats with any resolution event (success, failure, partial, abandoned)
		"uniqExactIf(gram_chat_id, gram_chat_id != '' AND evaluation_score_label != '') as total_chats",
		// Resolved: chats with evaluation_score_label = 'success' (from resolution analysis)
		"uniqExactIf(gram_chat_id, gram_chat_id != '' AND evaluation_score_label = 'success') as resolved_chats",
		// Failed: chats with evaluation_score_label = 'failure' (from resolution analysis)
		"uniqExactIf(gram_chat_id, gram_chat_id != '' AND evaluation_score_label = 'failure') as failed_chats",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gram.resource.urn) = 'agents:chat:completion') as avg_session_duration_ms",
		// Resolution time: average duration from resolution analysis events (gen_ai.conversation.duration is set in resolution events)
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, evaluation_score_label = 'success') as avg_resolution_time_ms",

		// Tool metrics
		"countIf(startsWith(gram_urn, 'tools:')) as total_tool_calls",
		"countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) as failed_tool_calls",
		"avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:')) as avg_latency_ms",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	// Optional filters
	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building overview summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return &OverviewSummary{
			TotalChats:           0,
			ResolvedChats:        0,
			FailedChats:          0,
			AvgSessionDurationMs: 0,
			AvgResolutionTimeMs:  0,
			TotalToolCalls:       0,
			FailedToolCalls:      0,
			AvgLatencyMs:         0,
		}, nil
	}

	var summary OverviewSummary
	if err = rows.ScanStruct(&summary); err != nil {
		return nil, fmt.Errorf("error scanning row: %w", err)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return &summary, nil
}

// ListChatsParams contains the parameters for listing chats.
type ListChatsParams struct {
	GramProjectID    string
	TimeStart        int64
	TimeEnd          int64
	GramDeploymentID string
	GramURN          string
	UserID           string
	ExternalUserID   string
	SortOrder        string
	Cursor           string // gram_chat_id to paginate from
	Limit            int
}

// ListChats retrieves aggregated chat summaries grouped by gram_chat_id.
//
// Original SQL reference:
// SELECT gram_chat_id, min(time_unix_nano), max(time_unix_nano), ... FROM telemetry_logs
// WHERE gram_project_id = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
// [+ optional filters] GROUP BY gram_chat_id ORDER BY start_time_unix_nano LIMIT ?
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListChats(ctx context.Context, arg ListChatsParams) ([]ChatSummary, error) {
	sb := sq.Select(
		"gram_chat_id",
		"min(time_unix_nano) as start_time_unix_nano",
		"max(time_unix_nano) as end_time_unix_nano",
		"count(*) as log_count",
		"countIf(startsWith(gram_urn, 'tools:')) as tool_call_count",
		// Message count: number of LLM completion events in this chat
		"countIf(toString(attributes.gram.resource.urn) = 'agents:chat:completion') as message_count",
		// Duration in seconds (max event time - min event time)
		"toFloat64(max(time_unix_nano) - min(time_unix_nano)) / 1000000000.0 as duration_seconds",
		// Status: failed if any tool call returned 4xx/5xx, otherwise success
		"if(countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) > 0, 'error', 'success') as status",
		"anyIf(toString(attributes.user.id), toString(attributes.user.id) != '') as user_id",
		// Model used (pick any non-empty response model from completion events)
		"anyIf(toString(attributes.gen_ai.response.model), toString(attributes.gram.resource.urn) = 'agents:chat:completion' AND toString(attributes.gen_ai.response.model) != '') as model",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') as total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') as total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') as total_tokens",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("gram_chat_id IS NOT NULL").
		Where("gram_chat_id != ''")

	// Optional filters
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}
	if arg.GramURN != "" {
		sb = sb.Where("position(telemetry_logs.gram_urn, ?) > 0", arg.GramURN)
	}
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{"user_id": arg.UserID})
	}
	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}

	sb = sb.GroupBy("gram_chat_id")

	// HAVING clause for cursor pagination with tuple comparison for tie-breaking
	sb = withHavingTuplePagination(sb, arg.Cursor, arg.SortOrder, arg.GramProjectID, "gram_chat_id", "min(time_unix_nano)")

	// Ordering - include gram_chat_id as secondary for stable ordering
	sb = withOrdering(sb, arg.SortOrder, "start_time_unix_nano", "gram_chat_id")

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list chats query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []ChatSummary
	for rows.Next() {
		var chat ChatSummary
		if err = rows.ScanStruct(&chat); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		chats = append(chats, chat)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return chats, nil
}

// SearchUsersParams contains the parameters for searching users with aggregated metrics.
type SearchUsersParams struct {
	GramProjectID    string
	TimeStart        int64
	TimeEnd          int64
	GramDeploymentID string // optional
	GroupBy          string // "user_id" or "external_user_id"
	SortOrder        string // "asc" or "desc"
	Cursor           string // user identifier to paginate from
	Limit            int
}

// SearchUsers retrieves aggregated usage metrics grouped by user identifier.
//
// Groups telemetry logs by user_id or external_user_id and computes per-user
// metrics including tokens, chats, and tool call breakdowns.
// Pagination uses last_seen_unix_nano + the group column for stable cursor ordering.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) SearchUsers(ctx context.Context, arg SearchUsersParams) ([]UserSummary, error) {
	groupCol := "user_id"
	if arg.GroupBy == "external_user_id" {
		groupCol = "external_user_id"
	}

	sb := sq.Select(
		groupCol+" AS user_id",

		// Activity timestamps
		"min(time_unix_nano) AS first_seen_unix_nano",
		"max(time_unix_nano) AS last_seen_unix_nano",

		// Chat metrics
		"uniqExactIf(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats",
		"countIf(toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_chat_requests",

		// Token metrics (from chat completion events)
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_tokens",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS avg_tokens_per_request",

		// Tool call metrics
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_call_success",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_call_failure",

		// Tool breakdowns (maps of tool URN -> count)
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:')) AS tool_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_success_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_failure_counts",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where(groupCol + " IS NOT NULL").
		Where(groupCol + " != ''")

	// Optional deployment filter
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}

	sb = sb.GroupBy(groupCol)

	// Cursor pagination using last_seen + group column for stable ordering
	sb = withHavingTuplePagination(sb, arg.Cursor, arg.SortOrder, arg.GramProjectID, groupCol, "max(time_unix_nano)")

	// Order by last_seen with group column as tie-breaker
	sb = withOrdering(sb, arg.SortOrder, "last_seen_unix_nano", groupCol)

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building search users query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserSummary
	for rows.Next() {
		var u UserSummary
		if err = rows.ScanStruct(&u); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		users = append(users, u)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// GetUserMetricsSummaryParams contains the parameters for getting a user's metrics summary.
type GetUserMetricsSummaryParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	UserID         string // user_id (mutually exclusive with ExternalUserID)
	ExternalUserID string // external_user_id (mutually exclusive with UserID)
}

// GetUserMetricsSummary retrieves aggregated metrics for a specific user.
// Uses the same aggregations as GetMetricsSummary (project metrics) but filtered by user.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetUserMetricsSummary(ctx context.Context, arg GetUserMetricsSummaryParams) (*MetricsSummaryRow, error) {
	sb := sq.Select(
		// Activity timestamps
		"min(time_unix_nano) AS first_seen_unix_nano",
		"max(time_unix_nano) AS last_seen_unix_nano",

		// Cardinality (exclude empty strings)
		"uniqExactIf(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats",
		"uniqExactIf(toString(attributes.gen_ai.response.model), toString(attributes.gen_ai.response.model) != '') AS distinct_models",
		"uniqExactIf(toString(attributes.gen_ai.provider.name), toString(attributes.gen_ai.provider.name) != '') AS distinct_providers",

		// Token metrics (from chat completion events)
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_tokens",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS avg_tokens_per_request",

		// Chat request metrics
		"countIf(toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_chat_requests",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS avg_chat_duration_ms",

		// Resolution status
		"countIf(position(toString(attributes.gen_ai.response.finish_reasons), 'stop') > 0) AS finish_reason_stop",
		"countIf(position(toString(attributes.gen_ai.response.finish_reasons), 'tool_calls') > 0) AS finish_reason_tool_calls",

		// Tool call metrics
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_call_success",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_call_failure",
		"avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS avg_tool_duration_ms",

		// Chat resolution metrics (from AI evaluation of chat outcomes)
		"countIf(evaluation_score_label = 'success') AS chat_resolution_success",
		"countIf(evaluation_score_label = 'failure') AS chat_resolution_failure",
		"countIf(evaluation_score_label = 'partial') AS chat_resolution_partial",
		"countIf(evaluation_score_label = 'abandoned') AS chat_resolution_abandoned",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.evaluation.score.value)), evaluation_score_label != '') AS avg_chat_resolution_score",

		// Model breakdown (map of model name -> count)
		"sumMapIf(map(toString(attributes.gen_ai.response.model), toUInt64(1)), toString(attributes.gram.resource.urn) = 'agents:chat:completion' AND toString(attributes.gen_ai.response.model) != '') AS models",

		// Tool breakdowns (maps of tool URN -> count)
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:')) AS tool_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_success_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_failure_counts",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	// Filter by user ID (one of these must be set)
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{"user_id": arg.UserID})
	} else if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building get user metrics summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		// Return empty metrics if no rows
		return &MetricsSummaryRow{
			FirstSeenUnixNano:       0,
			LastSeenUnixNano:        0,
			TotalChats:              0,
			DistinctModels:          0,
			DistinctProviders:       0,
			TotalInputTokens:        0,
			TotalOutputTokens:       0,
			TotalTokens:             0,
			AvgTokensPerReq:         0,
			TotalChatRequests:       0,
			AvgChatDurationMs:       0,
			FinishReasonStop:        0,
			FinishReasonToolCalls:   0,
			TotalToolCalls:          0,
			ToolCallSuccess:         0,
			ToolCallFailure:         0,
			AvgToolDurationMs:       0,
			ChatResolutionSuccess:   0,
			ChatResolutionFailure:   0,
			ChatResolutionPartial:   0,
			ChatResolutionAbandoned: 0,
			AvgChatResolutionScore:  0,
			Models:                  make(map[string]uint64),
			ToolCounts:              make(map[string]uint64),
			ToolSuccessCounts:       make(map[string]uint64),
			ToolFailureCounts:       make(map[string]uint64),
		}, nil
	}

	var metrics MetricsSummaryRow
	if err = rows.Scan(
		&metrics.FirstSeenUnixNano,
		&metrics.LastSeenUnixNano,
		&metrics.TotalChats,
		&metrics.DistinctModels,
		&metrics.DistinctProviders,
		&metrics.TotalInputTokens,
		&metrics.TotalOutputTokens,
		&metrics.TotalTokens,
		&metrics.AvgTokensPerReq,
		&metrics.TotalChatRequests,
		&metrics.AvgChatDurationMs,
		&metrics.FinishReasonStop,
		&metrics.FinishReasonToolCalls,
		&metrics.TotalToolCalls,
		&metrics.ToolCallSuccess,
		&metrics.ToolCallFailure,
		&metrics.AvgToolDurationMs,
		&metrics.ChatResolutionSuccess,
		&metrics.ChatResolutionFailure,
		&metrics.ChatResolutionPartial,
		&metrics.ChatResolutionAbandoned,
		&metrics.AvgChatResolutionScore,
		&metrics.Models,
		&metrics.ToolCounts,
		&metrics.ToolSuccessCounts,
		&metrics.ToolFailureCounts,
	); err != nil {
		return nil, fmt.Errorf("error scanning row: %w", err)
	}

	return &metrics, nil
}

// ListFilterOptionsParams contains the parameters for listing filter options.
type ListFilterOptionsParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
	FilterType    string // "api_key" or "user"
	Limit         int
}

// ListFilterOptions retrieves distinct filter values (API keys or users) for a time period.
// Results are sorted by event count descending.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListFilterOptions(ctx context.Context, arg ListFilterOptionsParams) ([]FilterOption, error) {
	var groupCol string
	switch arg.FilterType {
	case "api_key":
		groupCol = "api_key_id"
	case "user":
		groupCol = "external_user_id"
	default:
		return nil, fmt.Errorf("invalid filter type: %s", arg.FilterType)
	}

	sb := sq.Select(
		groupCol+" AS id",
		groupCol+" AS label",               // For now, label is same as ID
		"uniqExact(gram_chat_id) AS count", // Count unique chat sessions, not log rows
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where(groupCol + " != ''").
		GroupBy(groupCol).
		OrderBy("count DESC").
		Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list filter options query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var options []FilterOption
	for rows.Next() {
		var opt FilterOption
		if err = rows.ScanStruct(&opt); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		options = append(options, opt)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return options, nil
}
