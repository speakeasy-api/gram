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
		sb = sb.Where("toInt32OrZero(toString(attributes.`http.response.status_code`)) = ?", arg.HTTPResponseStatusCode)
	}
	if arg.HTTPRoute != "" {
		sb = sb.Where("toString(attributes.`http.route`) = ?", arg.HTTPRoute)
	}
	if arg.HTTPRequestMethod != "" {
		sb = sb.Where("toString(attributes.`http.request.method`) = ?", arg.HTTPRequestMethod)
	}
	if arg.ServiceName != "" {
		sb = sb.Where(squirrel.Eq{"service_name": arg.ServiceName})
	}
	if arg.GramChatID != "" {
		sb = sb.Where(squirrel.Eq{"gram_chat_id": arg.GramChatID})
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
		"min(time_unix_nano) as start_time_unix_nano",
		"count(*) as log_count",
		"anyIf(toInt32OrNull(toString(attributes.`http.response.status_code`)), toString(attributes.`http.response.status_code`) != '') as http_status_code",
		"any(gram_urn) as gram_urn",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("trace_id IS NOT NULL").
		Where("trace_id != ''")

	// Optional filters
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}
	if arg.GramFunctionID != "" {
		sb = sb.Where("gram_function_id = toUUIDOrNull(?)", arg.GramFunctionID)
	}
	if arg.GramURN != "" {
		sb = sb.Where("position(telemetry_logs.gram_urn, ?) > 0", arg.GramURN)
	}

	sb = sb.GroupBy("trace_id")

	sb = withHavingPagination(sb, arg.Cursor, arg.SortOrder, arg.GramProjectID, "trace_id", "min(time_unix_nano)")

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
		// Cardinality (exclude empty strings)
		"uniqExactIf(toString(attributes.`gen_ai.conversation.id`), toString(attributes.`gen_ai.conversation.id`) != '') AS total_chats",
		"uniqExactIf(toString(attributes.`gen_ai.response.model`), toString(attributes.`gen_ai.response.model`) != '') AS distinct_models",
		"uniqExactIf(toString(attributes.`gen_ai.provider.name`), toString(attributes.`gen_ai.provider.name`) != '') AS distinct_providers",

		// Token metrics (from chat completion events)
		"sumIf(toInt64OrZero(toString(attributes.`gen_ai.usage.input_tokens`)), toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') AS total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.`gen_ai.usage.output_tokens`)), toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') AS total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.`gen_ai.usage.total_tokens`)), toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') AS total_tokens",
		"avgIf(toFloat64OrZero(toString(attributes.`gen_ai.usage.total_tokens`)), toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') AS avg_tokens_per_request",

		// Chat request metrics
		"countIf(toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') AS total_chat_requests",
		"avgIf(toFloat64OrZero(toString(attributes.`gen_ai.conversation.duration`)) * 1000, toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') AS avg_chat_duration_ms",

		// Resolution status
		"countIf(position(toString(attributes.`gen_ai.response.finish_reasons`), 'stop') > 0) AS finish_reason_stop",
		"countIf(position(toString(attributes.`gen_ai.response.finish_reasons`), 'tool_calls') > 0) AS finish_reason_tool_calls",

		// Tool call metrics
		"countIf(startsWith(toString(attributes.`gram.tool.urn`), 'tools:')) AS total_tool_calls",
		"countIf(startsWith(toString(attributes.`gram.tool.urn`), 'tools:') AND toInt32OrZero(toString(attributes.`http.response.status_code`)) >= 200 AND toInt32OrZero(toString(attributes.`http.response.status_code`)) < 300) AS tool_call_success",
		"countIf(startsWith(toString(attributes.`gram.tool.urn`), 'tools:') AND toInt32OrZero(toString(attributes.`http.response.status_code`)) >= 400) AS tool_call_failure",
		"avgIf(toFloat64OrZero(toString(attributes.`http.server.request.duration`)) * 1000, startsWith(toString(attributes.`gram.tool.urn`), 'tools:')) AS avg_tool_duration_ms",

		// Model breakdown (map of model name -> count)
		"sumMapIf(map(toString(attributes.`gen_ai.response.model`), toUInt64(1)), toString(attributes.`gram.resource.urn`) = 'agents:chat:completion' AND toString(attributes.`gen_ai.response.model`) != '') AS models",

		// Tool breakdowns (maps of tool URN -> count)
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:')) AS tool_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.`http.response.status_code`)) >= 200 AND toInt32OrZero(toString(attributes.`http.response.status_code`)) < 300) AS tool_success_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.`http.response.status_code`)) >= 400) AS tool_failure_counts",
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
			TotalChats:            0,
			DistinctModels:        0,
			DistinctProviders:     0,
			TotalInputTokens:      0,
			TotalOutputTokens:     0,
			TotalTokens:           0,
			AvgTokensPerReq:       0,
			TotalChatRequests:     0,
			AvgChatDurationMs:     0,
			FinishReasonStop:      0,
			FinishReasonToolCalls: 0,
			TotalToolCalls:        0,
			ToolCallSuccess:       0,
			ToolCallFailure:       0,
			AvgToolDurationMs:     0,
			Models:                make(map[string]uint64),
			ToolCounts:            make(map[string]uint64),
			ToolSuccessCounts:     make(map[string]uint64),
			ToolFailureCounts:     make(map[string]uint64),
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

// ListChatsParams contains the parameters for listing chats.
type ListChatsParams struct {
	GramProjectID    string
	TimeStart        int64
	TimeEnd          int64
	GramDeploymentID string
	GramURN          string
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
		"countIf(toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') as message_count",
		// Duration in seconds (max event time - min event time)
		"toFloat64(max(time_unix_nano) - min(time_unix_nano)) / 1000000000.0 as duration_seconds",
		// Status: failed if any tool call returned 4xx/5xx, otherwise success
		"if(countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.`http.response.status_code`)) >= 400) > 0, 'error', 'success') as status",
		"anyIf(toString(attributes.`user.id`), toString(attributes.`user.id`) != '') as user_id",
		// Model used (pick any non-empty response model from completion events)
		"anyIf(toString(attributes.`gen_ai.response.model`), toString(attributes.`gram.resource.urn`) = 'agents:chat:completion' AND toString(attributes.`gen_ai.response.model`) != '') as model",
		"sumIf(toInt64OrZero(toString(attributes.`gen_ai.usage.input_tokens`)), toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') as total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.`gen_ai.usage.output_tokens`)), toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') as total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.`gen_ai.usage.total_tokens`)), toString(attributes.`gram.resource.urn`) = 'agents:chat:completion') as total_tokens",
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
