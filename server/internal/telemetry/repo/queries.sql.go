package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const listHTTPRequests = "select * from http_requests_raw where project_id = $%d and ts >= $%d and ts <= $%d"

// List retrieves tool logs based on the provided options.
func (q *Queries) ListHTTPRequests(ctx context.Context, opts ListToolLogsOptions) (res *ListResult, err error) {
	projectID := opts.ProjectID
	tsStart := opts.TsStart
	tsEnd := opts.TsEnd
	cursor := opts.Cursor
	pagination := opts.Pagination

	ctx, span := q.tracer.Start(ctx, "clickhouse.list_logs",
		trace.WithAttributes(
			attr.ProjectID(projectID),
			attr.PaginationTsStart(tsStart),
			attr.PaginationTsEnd(tsEnd),
			attr.PaginationCursor(cursor),
			attr.PaginationLimit(pagination.Limit()),
			attr.PaginationSortOrder(pagination.SortOrder()),
		),
	)
	defer func() {
		if err == nil {
			span.SetStatus(codes.Ok, "")
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	startTime := time.Now()

	// Build query with filters
	query, args := buildListHTTPRequestsQuery(opts)

	perPage := pagination.Limit() - 1 // Remove the +1 for actual page size
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}

	defer o11y.LogDefer(ctx, q.logger, func() error { return rows.Close() })

	var results []ToolHTTPRequest
	for rows.Next() {
		var log ToolHTTPRequest
		if err = rows.ScanStruct(&log); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		results = append(results, log)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	// Calculate pagination metadata
	hasNextPage := len(results) > perPage

	// the next cursor is the id of the last record
	var nextPageCursor *string
	if len(results) > 0 && hasNextPage {
		nextPageCursor = conv.Ptr(results[len(results)-1].ID)
	}

	// Trim to actual page size if we fetched extra for detection
	if hasNextPage {
		results = results[:perPage]
	}

	queryDuration := time.Since(startTime)
	span.SetAttributes(
		attr.ValueInt(len(results)),
		attr.ProjectID(projectID),
		attr.PaginationHasNextPage(hasNextPage),
		attr.ClickhouseQueryDurationMs(float64(queryDuration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "")

	return &ListResult{
		Logs: results,
		Pagination: PaginationMetadata{
			PerPage:        perPage,
			HasNextPage:    hasNextPage,
			NextPageCursor: nextPageCursor,
		},
	}, nil
}

// ListToolLogsOptions contains options for listing tool logs.
type ListToolLogsOptions struct {
	ProjectID  string
	TsStart    time.Time
	TsEnd      time.Time
	Cursor     string
	Status     string
	ServerName string
	ToolName   string
	ToolType   string
	ToolURNs   []string
	*Pagination
}

func buildListHTTPRequestsQuery(opts ListToolLogsOptions) (string, []any) {
	var args []any
	paramIndex := 4 // Start after project_id, ts_start, ts_end

	baseQuery := listHTTPRequests
	args = append(args, opts.ProjectID, opts.TsStart, opts.TsEnd)

	// Add cursor condition based on sort order
	if opts.SortOrder() == "asc" {
		baseQuery += fmt.Sprintf(" and ts > UUIDv7ToDateTime(toUUID($%d))", paramIndex)
	} else {
		baseQuery += fmt.Sprintf(" and ts < UUIDv7ToDateTime(toUUID($%d))", paramIndex)
	}
	args = append(args, opts.Cursor)
	paramIndex++

	// Add optional filters
	switch opts.Status {
	case "success":
		baseQuery += " and status_code <= 399"
	case "failure":
		baseQuery += " and status_code >= 400"
	}

	if opts.ServerName != "" {
		baseQuery += fmt.Sprintf(" and tool_urn LIKE $%d", paramIndex)
		args = append(args, "%"+opts.ServerName+"%")
		paramIndex++
	}

	if opts.ToolName != "" {
		baseQuery += fmt.Sprintf(" and tool_urn LIKE $%d", paramIndex)
		args = append(args, "%"+opts.ToolName+"%")
		paramIndex++
	}

	if opts.ToolType != "" {
		baseQuery += fmt.Sprintf(" and tool_type = $%d", paramIndex)
		args = append(args, opts.ToolType)
		paramIndex++
	}

	if len(opts.ToolURNs) > 0 {
		// Limit to 1000 items to prevent query string from growing too large
		toolURNs := opts.ToolURNs
		if len(toolURNs) > 1000 {
			toolURNs = toolURNs[:1000]
		}

		placeholders := ""
		for i := range toolURNs {
			if i > 0 {
				placeholders += ", "
			}
			placeholders += fmt.Sprintf("$%d", paramIndex)
			args = append(args, toolURNs[i])
			paramIndex++
		}
		baseQuery += fmt.Sprintf(" and tool_urn IN (%s)", placeholders)
	}

	// Add ordering and limit
	if opts.SortOrder() == "asc" {
		baseQuery += " order by ts"
	} else {
		baseQuery += " order by ts desc"
	}

	baseQuery += fmt.Sprintf(" limit $%d", paramIndex)
	args = append(args, opts.Limit())

	// Format the query with parameter indices
	query := fmt.Sprintf(baseQuery, 1, 2, 3)

	return query, args
}

const insertHttpRaw = `insert into http_requests_raw
	(id, ts, organization_id, project_id, deployment_id, tool_id, tool_urn, tool_type, trace_id, span_id, http_method,
	 http_server_url, http_route, status_code, duration_ms, user_agent, request_headers, request_body_bytes, response_headers, response_body_bytes)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)`

// Log inserts a tool HTTP request log entry.
func (q *Queries) LogHTTPRequest(ctx context.Context, log ToolHTTPRequest) (err error) {
	ctx, span := q.tracer.Start(ctx, "clickhouse.log_http_request",
		trace.WithAttributes(
			attr.ToolID(log.ToolID),
			attr.ToolURN(log.ToolURN),
			attr.ProjectID(log.ProjectID),
			attr.OrganizationID(log.OrganizationID),
			attr.HTTPRequestMethod(log.HTTPMethod),
			attr.HTTPRoute(log.HTTPRoute),
			attr.HTTPResponseStatusCode(int(log.StatusCode)),
			attr.HTTPClientRequestDuration(log.DurationMs/1000),
		),
	)
	defer func() {
		if err == nil {
			span.SetStatus(codes.Ok, "")
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	startTime := time.Now()

	// order matters here
	args := []any{
		log.ID,
		log.Ts,
		log.OrganizationID,
		log.ProjectID,
		log.DeploymentID,
		log.ToolID,
		log.ToolURN,
		log.ToolType,
		log.TraceID,
		log.SpanID,
		log.HTTPMethod,
		log.HTTPServerURL,
		log.HTTPRoute,
		log.StatusCode,
		log.DurationMs,
		log.UserAgent,
		log.RequestHeaders,
		log.RequestBodyBytes,
		log.ResponseHeaders,
		log.ResponseBodyBytes,
	}

	err = q.conn.Exec(ctx, insertHttpRaw, args...)
	if err != nil {
		return fmt.Errorf("insert http raw: %w", err)
	}

	insertDuration := time.Since(startTime)
	span.SetAttributes(
		attr.ClickhouseQueryDurationMs(float64(insertDuration.Milliseconds())),
		attr.HTTPRequestBodyBytes(int(log.RequestBodyBytes)),
		attr.HTTPResponseBodyBytes(int(log.ResponseBodyBytes)),
	)

	return nil
}

const listToolLogs = `-- name: ListToolLogs :many
select
	id,
	timestamp,
	instance,
	level,
	source,
	raw_log,
	message,
	toString(attributes) as attributes,
	project_id,
	deployment_id,
	function_id
from tool_logs
where project_id = ?
	and timestamp >= ?
	and timestamp <= ?
	and (? = '' or deployment_id = ?)
	and (? = '' or function_id = ?)
	and (? = '' or instance = ?)
	and (? = '' or level = ?)
	and (? = '' or source = ?)
	-- Cursor pagination: nil UUID = first page, otherwise compare based on sort direction
	-- Use IF to avoid subquery execution on first page
	and if(
		? = '00000000-0000-0000-0000-000000000000',
		true,
		if(
			? = 'asc',
			(timestamp, toUUID(id)) > (select timestamp, toUUID(id) from tool_logs where id = ? limit 1),
			(timestamp, toUUID(id)) < (select timestamp, toUUID(id) from tool_logs where id = ? limit 1)
		)
	)
order by
	if(? = 'asc', timestamp, toDateTime('1970-01-01')) asc,
	if(? = 'asc', toUUID(id), toUUID('00000000-0000-0000-0000-000000000000')) asc,
	if(? = 'desc', timestamp, toDateTime('1970-01-01')) desc,
	if(? = 'desc', toUUID(id), toUUID('00000000-0000-0000-0000-000000000000')) desc
limit ?
`

type ListToolLogsParams struct {
	ProjectID    string
	TsStart      time.Time
	TsEnd        time.Time
	DeploymentID string
	FunctionID   string
	Instance     string
	Level        string
	Source       string
	SortOrder    string
	Cursor       string
	Limit        int
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListToolLogs(ctx context.Context, arg ListToolLogsParams) (*ToolLogsListResult, error) {
	perPage := arg.Limit - 1

	rows, err := q.conn.Query(ctx, listToolLogs,
		arg.ProjectID,                      // 1: project_id
		arg.TsStart,                        // 2: timestamp >=
		arg.TsEnd,                          // 3: timestamp <=
		arg.DeploymentID, arg.DeploymentID, // 4,5: deployment_id filter
		arg.FunctionID, arg.FunctionID, // 6,7: function_id filter
		arg.Instance, arg.Instance, // 8,9: instance filter
		arg.Level, arg.Level, // 10,11: level filter
		arg.Source, arg.Source, // 12,13: source filter
		arg.Cursor,    // 14: cursor nil check
		arg.SortOrder, // 15: ASC or DESC for comparison
		arg.Cursor,    // 16: ASC cursor subquery
		arg.Cursor,    // 17: DESC cursor subquery
		arg.SortOrder, // 18: ORDER BY timestamp ASC
		arg.SortOrder, // 19: ORDER BY id ASC
		arg.SortOrder, // 20: ORDER BY timestamp DESC
		arg.SortOrder, // 21: ORDER BY id DESC
		arg.Limit,     // 22: LIMIT
	)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var items []ToolLog
	for rows.Next() {
		var log ToolLog
		if err = rows.ScanStruct(&log); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		items = append(items, log)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Calculate pagination metadata
	hasNextPage := len(items) > perPage

	// Trim to actual page size if we fetched extra for detection
	if hasNextPage {
		items = items[:perPage]
	}

	// Set cursor to last item in the trimmed page
	var nextPageCursor *string
	if len(items) > 0 && hasNextPage {
		nextPageCursor = conv.Ptr(items[len(items)-1].ID)
	}

	return &ToolLogsListResult{
		Logs: items,
		Pagination: PaginationMetadata{
			PerPage:        perPage,
			HasNextPage:    hasNextPage,
			NextPageCursor: nextPageCursor,
		},
	}, nil
}

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

const listLogsForTrace = `-- name: ListLogsForTrace :many
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
    AND trace_id = ?
ORDER BY time_unix_nano ASC, id ASC
LIMIT ?
`

type ListLogsForTraceParams struct {
	GramProjectID string
	TraceID       string
	Limit         int
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListLogsForTrace(ctx context.Context, arg ListLogsForTraceParams) ([]TelemetryLog, error) {
	rows, err := q.conn.Query(ctx, listLogsForTrace,
		arg.GramProjectID, // 1: gram_project_id
		arg.TraceID,       // 2: trace_id
		arg.Limit,         // 3: LIMIT
	)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var logs []TelemetryLog
	for rows.Next() {
		var log TelemetryLog
		if err = rows.ScanStruct(&log); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		logs = append(logs, log)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return logs, nil
}
