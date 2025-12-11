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
	if opts.SortOrder() == "ASC" {
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
	if opts.SortOrder() == "ASC" {
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

func (q *Queries) ShouldLog(ctx context.Context, orgId string) (bool, error) {
	return q.ShouldFlag(ctx, orgId)
}

const insertHttpRaw = `insert into http_requests_raw
	(id, ts, organization_id, project_id, deployment_id, tool_id, tool_urn, tool_type, trace_id, span_id, http_method,
	 http_server_url, http_route, status_code, duration_ms, user_agent, request_headers, request_body_bytes, response_headers, response_body_bytes)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)`

// Log inserts a tool HTTP request log entry.
func (q *Queries) LogHTTPRequest(ctx context.Context, log ToolHTTPRequest) (err error) {
	allow, err := q.ShouldFlag(ctx, log.OrganizationID)
	if err != nil {
		q.logger.ErrorContext(ctx, "failed to fetch feature flag", attr.SlogError(err))
		return nil
	}

	if !allow {
		return nil
	}

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
	-- Cursor-based pagination using the nil UUID (00000000-0000-0000-0000-000000000000) as a sentinel
	-- value to indicate "no cursor" (first page). This is necessary because ClickHouse doesn't support
	-- short-circuit evaluation in OR expressions - it would try to parse an empty string as a UUID.
	-- The IF function provides conditional evaluation: skip cursor filtering on first page, otherwise
	-- filter by timestamp extracted from the UUIDv7 cursor (which embeds creation time).
	and if(
		toUUID(?) = toUUID('00000000-0000-0000-0000-000000000000'),
		true,
		if(? = 'ASC', timestamp > UUIDv7ToDateTime(toUUID(?)), timestamp < UUIDv7ToDateTime(toUUID(?)))
	)
order by
	case when ? = 'ASC' then timestamp end asc,
	case when ? = 'DESC' then timestamp end desc
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
		arg.SortOrder, // 15: sort order check in IF
		arg.Cursor,    // 16: cursor for ASC case
		arg.Cursor,    // 17: cursor for DESC case
		arg.SortOrder, // 18: ORDER BY ASC
		arg.SortOrder, // 19: ORDER BY DESC
		arg.Limit,     // 20: LIMIT
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

	var nextPageCursor *string
	if len(items) > 0 && hasNextPage {
		nextPageCursor = conv.Ptr(items[len(items)-1].ID)
	}

	if hasNextPage {
		items = items[:perPage]
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
