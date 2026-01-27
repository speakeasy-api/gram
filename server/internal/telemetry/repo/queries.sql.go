package repo

import (
	"context"
	"fmt"
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
    http_request_method,
    http_response_status_code,
    http_route,
    http_server_url
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

type InsertTelemetryLogParams struct {
	ID                     string
	TimeUnixNano           int64
	ObservedTimeUnixNano   int64
	SeverityText           *string
	Body                   string
	TraceID                *string
	SpanID                 *string
	Attributes             string
	ResourceAttributes     string
	GramProjectID          string
	GramDeploymentID       *string
	GramFunctionID         *string
	GramURN                string
	ServiceName            string
	ServiceVersion         *string
	HTTPRequestMethod      *string
	HTTPResponseStatusCode *int32
	HTTPRoute              *string
	HTTPServerURL          *string
}

//nolint:wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) InsertTelemetryLog(ctx context.Context, arg InsertTelemetryLogParams) error {
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
		arg.HTTPRequestMethod,
		arg.HTTPResponseStatusCode,
		arg.HTTPRoute,
		arg.HTTPServerURL,
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
    http_request_method,
    http_response_status_code,
    http_route,
    http_server_url
FROM telemetry_logs
WHERE gram_project_id = ?
    AND time_unix_nano >= ?
    AND time_unix_nano <= ?
    AND (length(?) = 0 OR has(?, gram_urn))
    AND (? = '' OR trace_id = ?)
    AND (? = '' OR gram_deployment_id = toUUIDOrNull(?))
    AND (? = '' OR gram_function_id = toUUIDOrNull(?))
    AND (? = '' OR severity_text = ?)
    AND (? = 0 OR http_response_status_code = ?)
    AND (? = '' OR http_route = ?)
    AND (? = '' OR http_request_method = ?)
    AND (? = '' OR service_name = ?)
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
		arg.Cursor,    // 22: cursor empty string check
		arg.SortOrder, // 23: ASC or DESC for comparison
		arg.Cursor,    // 24: ASC cursor subquery
		arg.Cursor,    // 25: DESC cursor subquery
		arg.SortOrder, // 26: ORDER BY time_unix_nano ASC
		arg.SortOrder, // 27: ORDER BY id ASC
		arg.SortOrder, // 28: ORDER BY time_unix_nano DESC
		arg.SortOrder, // 29: ORDER BY id DESC
		arg.Limit,     // 30: LIMIT
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
    any(http_response_status_code) as http_status_code,
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
            min(time_unix_nano) > (SELECT min(time_unix_nano) FROM telemetry_logs WHERE trace_id = ? GROUP BY trace_id LIMIT 1),
            min(time_unix_nano) < (SELECT min(time_unix_nano) FROM telemetry_logs WHERE trace_id = ? GROUP BY trace_id LIMIT 1)
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
		arg.Cursor,    // 10: cursor empty string check
		arg.SortOrder, // 11: ASC or DESC for comparison
		arg.Cursor,    // 12: ASC cursor subquery
		arg.Cursor,    // 13: DESC cursor subquery
		arg.SortOrder, // 14: ORDER BY start_time ASC
		arg.SortOrder, // 15: ORDER BY start_time DESC
		arg.Limit,     // 16: LIMIT
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
    -- Cardinality (only meaningful for project scope, exclude empty strings)
    uniqExactIf(toString(attributes.` + "`gen_ai.conversation.id`" + `), ? = 'project' AND toString(attributes.` + "`gen_ai.conversation.id`" + `) != '') AS total_chats,
    uniqExactIf(toString(attributes.` + "`gen_ai.response.model`" + `), ? = 'project' AND toString(attributes.` + "`gen_ai.response.model`" + `) != '') AS distinct_models,
    uniqExactIf(toString(attributes.` + "`gen_ai.provider.name`" + `), ? = 'project' AND toString(attributes.` + "`gen_ai.provider.name`" + `) != '') AS distinct_providers,

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
            AND http_response_status_code >= 200 AND http_response_status_code < 300) AS tool_call_success,
    countIf(startsWith(toString(attributes.` + "`gram.tool.urn`" + `), 'tools:')
            AND http_response_status_code >= 400) AS tool_call_failure,
    avgIf(toFloat64OrZero(toString(attributes.` + "`http.server.request.duration`" + `)) * 1000,
          startsWith(toString(attributes.` + "`gram.tool.urn`" + `), 'tools:')) AS avg_tool_duration_ms

FROM telemetry_logs
WHERE gram_project_id = ?
    AND time_unix_nano >= ?
    AND time_unix_nano <= ?
    -- Chat filter (only applied when scope=chat)
    AND (? = 'project' OR toString(attributes.` + "`gen_ai.conversation.id`" + `) = ?)
`

type GetMetricsSummaryParams struct {
	Scope         string // "project" or "chat"
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
	ChatID        string // Required when scope=chat
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetMetricsSummary(ctx context.Context, arg GetMetricsSummaryParams) (*MetricsSummaryRow, error) {
	rows, err := q.conn.Query(ctx, getMetricsSummary,
		arg.Scope,             // 1: cardinality check (total_chats)
		arg.Scope,             // 2: cardinality check (distinct_models)
		arg.Scope,             // 3: cardinality check (distinct_providers)
		arg.GramProjectID,     // 4: gram_project_id
		arg.TimeStart,         // 5: time_unix_nano >=
		arg.TimeEnd,           // 6: time_unix_nano <=
		arg.Scope, arg.ChatID, // 7,8: chat filter
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
