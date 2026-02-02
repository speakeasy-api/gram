package repo

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
)

const insertTelemetryLog = `-- name: InsertTelemetryLog :exec
INSERT INTO telemetry_logs (
    id,
    time_unix_nano,
    observed_time_unix_nano,
    severity_text,
    body,
    trace_id,
    span_id,
    attributes,
    resource_attributes,
    gram_project_id,
    gram_deployment_id,
    gram_function_id,
    gram_urn,
    service_name,
    service_version,
    gram_chat_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

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
	GramURN        string
	ServiceName    string
	ServiceVersion *string
	GramChatID     *string
}

//nolint:wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) InsertTelemetryLog(ctx context.Context, arg InsertTelemetryLogParams) error {
	ctx = clickhouse.Context(ctx, clickhouse.WithAsync(false))

	// Async insert is configured at the connection level in deps.go
	return q.conn.Exec(ctx, insertTelemetryLog,
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
	)
}

const listTelemetryLogs = `-- name: ListTelemetryLogs :many
SELECT
    id,
    time_unix_nano,
    observed_time_unix_nano,
    severity_text,
    body,
    trace_id,
    span_id,
    toString(attributes) as attributes,
    toString(resource_attributes) as resource_attributes,
    gram_project_id,
    gram_deployment_id,
    gram_function_id,
    gram_urn,
    service_name,
    service_version,
    gram_chat_id
FROM telemetry_logs
WHERE gram_project_id = ?
    AND time_unix_nano >= ?
    AND time_unix_nano <= ?
    AND (length(?) = 0 OR has(?, gram_urn))
    AND (? = '' OR trace_id = ?)
    AND (? = '' OR gram_deployment_id = toUUIDOrNull(?))
    AND (? = '' OR gram_function_id = toUUIDOrNull(?))
    AND (? = '' OR severity_text = ?)
    AND (? = 0 OR toInt32OrZero(toString(attributes.` + "`http.response.status_code`" + `)) = ?)
    AND (? = '' OR toString(attributes.` + "`http.route`" + `) = ?)
    AND (? = '' OR toString(attributes.` + "`http.request.method`" + `) = ?)
    AND (? = '' OR service_name = ?)
    AND (? = '' OR gram_chat_id = ?)
    -- Cursor pagination: empty string = first page, otherwise compare based on sort direction
    AND if(
        ? = '',
        true,
        if(
            ? = 'asc',
            (time_unix_nano, toUUID(id)) > (SELECT time_unix_nano, toUUID(id) FROM telemetry_logs WHERE id = toUUID(?) LIMIT 1),
            (time_unix_nano, toUUID(id)) < (SELECT time_unix_nano, toUUID(id) FROM telemetry_logs WHERE id = toUUID(?) LIMIT 1)
        )
    )
ORDER BY
    IF(? = 'asc', time_unix_nano, 0) ASC,
    IF(? = 'asc', toUUID(id), toUUID('00000000-0000-0000-0000-000000000000')) ASC,
    IF(? = 'desc', time_unix_nano, 0) DESC,
    IF(? = 'desc', toUUID(id), toUUID('00000000-0000-0000-0000-000000000000')) DESC
LIMIT ?
`

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

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListTelemetryLogs(ctx context.Context, arg ListTelemetryLogsParams) ([]TelemetryLog, error) {
	rows, err := q.conn.Query(ctx, listTelemetryLogs,
		arg.GramProjectID,          // 1: gram_project_id
		arg.TimeStart,              // 2: time_unix_nano >=
		arg.TimeEnd,                // 3: time_unix_nano <=
		arg.GramURNs, arg.GramURNs, // 4,5: gram_urns filter (array)
		arg.TraceID, arg.TraceID, // 6,7: trace_id filter
		arg.GramDeploymentID, arg.GramDeploymentID, // 8,9: gram_deployment_id filter
		arg.GramFunctionID, arg.GramFunctionID, // 10,11: gram_function_id filter
		arg.SeverityText, arg.SeverityText, // 12,13: severity_text filter
		arg.HTTPResponseStatusCode, arg.HTTPResponseStatusCode, // 14,15: http_response_status_code filter
		arg.HTTPRoute, arg.HTTPRoute, // 16,17: http_route filter
		arg.HTTPRequestMethod, arg.HTTPRequestMethod, // 18,19: http_request_method filter
		arg.ServiceName, arg.ServiceName, // 20,21: service_name filter
		arg.GramChatID, arg.GramChatID, // 22,23: gram_chat_id filter
		arg.Cursor,    // 24: cursor empty string check
		arg.SortOrder, // 25: ASC or DESC for comparison
		arg.Cursor,    // 26: ASC cursor subquery
		arg.Cursor,    // 27: DESC cursor subquery
		arg.SortOrder, // 28: ORDER BY time_unix_nano ASC
		arg.SortOrder, // 29: ORDER BY id ASC
		arg.SortOrder, // 30: ORDER BY time_unix_nano DESC
		arg.SortOrder, // 31: ORDER BY id DESC
		arg.Limit,     // 32: LIMIT
	)
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

const listTraces = `-- name: ListTraces :many
SELECT
    trace_id,
    min(time_unix_nano) as start_time_unix_nano,
    count(*) as log_count,
    anyIf(toInt32OrNull(toString(attributes.` + "`http.response.status_code`" + `)), toString(attributes.` + "`http.response.status_code`" + `) != '') as http_status_code,
	any(gram_urn) as gram_urn
FROM telemetry_logs
WHERE gram_project_id = ?
    AND time_unix_nano >= ?
    AND time_unix_nano <= ?
    AND trace_id IS NOT NULL
    AND trace_id != ''
    AND (? = '' OR gram_deployment_id = toUUIDOrNull(?))
    AND (? = '' OR gram_function_id = toUUIDOrNull(?))
    AND if(? = '', true, position(telemetry_logs.gram_urn, ?) > 0)
GROUP BY trace_id
HAVING if(
        ? = '',
        true,
        if(
            ? = 'asc',
            min(time_unix_nano) > (SELECT min(time_unix_nano) FROM telemetry_logs WHERE gram_project_id = ? AND trace_id = ? GROUP BY trace_id LIMIT 1),
            min(time_unix_nano) < (SELECT min(time_unix_nano) FROM telemetry_logs WHERE gram_project_id = ? AND trace_id = ? GROUP BY trace_id LIMIT 1)
        )
    )
ORDER BY
    IF(? = 'asc', start_time_unix_nano, 0) ASC,
    IF(? = 'desc', start_time_unix_nano, 0) DESC
LIMIT ?
`

type ListTracesParams struct {
	GramProjectID    string
	TimeStart        int64
	TimeEnd          int64
	GramDeploymentID string
	GramFunctionID   string
	GramURN          string // Single URN filter (supports LIKE pattern matching)
	SortOrder        string
	Cursor           string // trace_id to paginate from
	Limit            int
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListTraces(ctx context.Context, arg ListTracesParams) ([]TraceSummary, error) {
	rows, err := q.conn.Query(ctx, listTraces,
		arg.GramProjectID,                          // 1: gram_project_id
		arg.TimeStart,                              // 2: time_unix_nano >=
		arg.TimeEnd,                                // 3: time_unix_nano <=
		arg.GramDeploymentID, arg.GramDeploymentID, // 4,5: deployment_id filter
		arg.GramFunctionID, arg.GramFunctionID, // 6,7: function_id filter
		arg.GramURN, arg.GramURN, // 8,9: gram_urn filter (position-based substring search)
		arg.Cursor,                        // 10: cursor empty string check
		arg.SortOrder,                     // 11: ASC or DESC for comparison
		arg.GramProjectID, arg.Cursor,     // 12,13: ASC cursor subquery (project + trace_id)
		arg.GramProjectID, arg.Cursor,     // 14,15: DESC cursor subquery (project + trace_id)
		arg.SortOrder, // 16: ORDER BY start_time ASC
		arg.SortOrder, // 17: ORDER BY start_time DESC
		arg.Limit,     // 18: LIMIT
	)
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

const getMetricsSummary = `-- name: GetMetricsSummary :one
SELECT
    -- Cardinality (exclude empty strings)
    uniqExactIf(toString(attributes.` + "`gen_ai.conversation.id`" + `), toString(attributes.` + "`gen_ai.conversation.id`" + `) != '') AS total_chats,
    uniqExactIf(toString(attributes.` + "`gen_ai.response.model`" + `), toString(attributes.` + "`gen_ai.response.model`" + `) != '') AS distinct_models,
    uniqExactIf(toString(attributes.` + "`gen_ai.provider.name`" + `), toString(attributes.` + "`gen_ai.provider.name`" + `) != '') AS distinct_providers,

    -- Token metrics (from chat completion events)
    sumIf(toInt64OrZero(toString(attributes.` + "`gen_ai.usage.input_tokens`" + `)),
          toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') AS total_input_tokens,
    sumIf(toInt64OrZero(toString(attributes.` + "`gen_ai.usage.output_tokens`" + `)),
          toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') AS total_output_tokens,
    sumIf(toInt64OrZero(toString(attributes.` + "`gen_ai.usage.total_tokens`" + `)),
          toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') AS total_tokens,
    avgIf(toFloat64OrZero(toString(attributes.` + "`gen_ai.usage.total_tokens`" + `)),
          toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') AS avg_tokens_per_request,

    -- Chat request metrics
    countIf(toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') AS total_chat_requests,
    avgIf(toFloat64OrZero(toString(attributes.` + "`gen_ai.conversation.duration`" + `)) * 1000,
          toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') AS avg_chat_duration_ms,

    -- Resolution status
    countIf(position(toString(attributes.` + "`gen_ai.response.finish_reasons`" + `), 'stop') > 0) AS finish_reason_stop,
    countIf(position(toString(attributes.` + "`gen_ai.response.finish_reasons`" + `), 'tool_calls') > 0) AS finish_reason_tool_calls,

    -- Tool call metrics
    countIf(startsWith(toString(attributes.` + "`gram.tool.urn`" + `), 'tools:')) AS total_tool_calls,
    countIf(startsWith(toString(attributes.` + "`gram.tool.urn`" + `), 'tools:')
            AND toInt32OrZero(toString(attributes.` + "`http.response.status_code`" + `)) >= 200 AND toInt32OrZero(toString(attributes.` + "`http.response.status_code`" + `)) < 300) AS tool_call_success,
    countIf(startsWith(toString(attributes.` + "`gram.tool.urn`" + `), 'tools:')
            AND toInt32OrZero(toString(attributes.` + "`http.response.status_code`" + `)) >= 400) AS tool_call_failure,
    avgIf(toFloat64OrZero(toString(attributes.` + "`http.server.request.duration`" + `)) * 1000,
          startsWith(toString(attributes.` + "`gram.tool.urn`" + `), 'tools:')) AS avg_tool_duration_ms,

    -- Model breakdown (map of model name -> count)
    sumMapIf(
        map(toString(attributes.` + "`gen_ai.response.model`" + `), toUInt64(1)),
        toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion'
        AND toString(attributes.` + "`gen_ai.response.model`" + `) != ''
    ) AS models,

    -- Tool breakdowns (maps of tool URN -> count)
    sumMapIf(
        map(gram_urn, toUInt64(1)),
        startsWith(gram_urn, 'tools:')
    ) AS tool_counts,
    sumMapIf(
        map(gram_urn, toUInt64(1)),
        startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.` + "`http.response.status_code`" + `)) >= 200 AND toInt32OrZero(toString(attributes.` + "`http.response.status_code`" + `)) < 300
    ) AS tool_success_counts,
    sumMapIf(
        map(gram_urn, toUInt64(1)),
        startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.` + "`http.response.status_code`" + `)) >= 400
    ) AS tool_failure_counts

FROM telemetry_logs
WHERE gram_project_id = ?
    AND time_unix_nano >= ?
    AND time_unix_nano <= ?
`

type GetMetricsSummaryParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetMetricsSummary(ctx context.Context, arg GetMetricsSummaryParams) (*MetricsSummaryRow, error) {
	rows, err := q.conn.Query(ctx, getMetricsSummary,
		arg.GramProjectID, // 1: gram_project_id
		arg.TimeStart,     // 2: time_unix_nano >=
		arg.TimeEnd,       // 3: time_unix_nano <=
	)
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

const listChats = `-- name: ListChats :many
SELECT
    gram_chat_id,
    min(time_unix_nano) as start_time_unix_nano,
    max(time_unix_nano) as end_time_unix_nano,
    count(*) as log_count,
    countIf(startsWith(gram_urn, 'tools:')) as tool_call_count,
    -- Message count: number of LLM completion events in this chat
    countIf(toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') as message_count,
    -- Duration in seconds (max event time - min event time)
    toFloat64(max(time_unix_nano) - min(time_unix_nano)) / 1000000000.0 as duration_seconds,
    -- Status: failed if any tool call returned 4xx/5xx, otherwise success
    if(countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.` + "`http.response.status_code`" + `)) >= 400) > 0, 'error', 'success') as status,
    anyIf(toString(attributes.` + "`user.id`" + `), toString(attributes.` + "`user.id`" + `) != '') as user_id,
    -- Model used (pick any non-empty response model from completion events)
    anyIf(toString(attributes.` + "`gen_ai.response.model`" + `),
          toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion'
          AND toString(attributes.` + "`gen_ai.response.model`" + `) != '') as model,
    sumIf(toInt64OrZero(toString(attributes.` + "`gen_ai.usage.input_tokens`" + `)),
          toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') as total_input_tokens,
    sumIf(toInt64OrZero(toString(attributes.` + "`gen_ai.usage.output_tokens`" + `)),
          toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') as total_output_tokens,
    sumIf(toInt64OrZero(toString(attributes.` + "`gen_ai.usage.total_tokens`" + `)),
          toString(attributes.` + "`gram.resource.urn`" + `) = 'agents:chat:completion') as total_tokens
FROM telemetry_logs
WHERE gram_project_id = ?
    AND time_unix_nano >= ?
    AND time_unix_nano <= ?
    AND gram_chat_id IS NOT NULL
    AND gram_chat_id != ''
    AND (? = '' OR gram_deployment_id = toUUIDOrNull(?))
    AND if(? = '', true, position(telemetry_logs.gram_urn, ?) > 0)
GROUP BY gram_chat_id
HAVING if(
        ? = '',
        true,
        if(
            ? = 'asc',
            (min(time_unix_nano), gram_chat_id) > ((SELECT min(time_unix_nano) FROM telemetry_logs WHERE gram_project_id = ? AND gram_chat_id = ? GROUP BY gram_chat_id LIMIT 1), ?),
            (min(time_unix_nano), gram_chat_id) < ((SELECT min(time_unix_nano) FROM telemetry_logs WHERE gram_project_id = ? AND gram_chat_id = ? GROUP BY gram_chat_id LIMIT 1), ?)
        )
    )
ORDER BY
    IF(? = 'asc', start_time_unix_nano, 0) ASC,
    IF(? = 'desc', start_time_unix_nano, 0) DESC,
    gram_chat_id ASC
LIMIT ?
`

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

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListChats(ctx context.Context, arg ListChatsParams) ([]ChatSummary, error) {
	rows, err := q.conn.Query(ctx, listChats,
		arg.GramProjectID,                          // 1: gram_project_id
		arg.TimeStart,                              // 2: time_unix_nano >=
		arg.TimeEnd,                                // 3: time_unix_nano <=
		arg.GramDeploymentID, arg.GramDeploymentID, // 4,5: deployment_id filter
		arg.GramURN, arg.GramURN, // 6,7: gram_urn filter
		arg.Cursor,                        // 8: cursor empty string check
		arg.SortOrder,                     // 9: ASC or DESC for comparison
		arg.GramProjectID, arg.Cursor, arg.Cursor, // 10,11,12: ASC cursor subquery (project + chat_id) + tiebreaker
		arg.GramProjectID, arg.Cursor, arg.Cursor, // 13,14,15: DESC cursor subquery (project + chat_id) + tiebreaker
		arg.SortOrder, // 16: ORDER BY start_time ASC
		arg.SortOrder, // 17: ORDER BY start_time DESC
		arg.Limit,     // 18: LIMIT
	)
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
