package toolmetrics

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

const insertHttpRaw = `insert into gram.http_requests_raw
    (id, ts, organization_id, project_id, deployment_id, tool_id, tool_urn, tool_type, trace_id, span_id, http_method,
     http_route, status_code, duration_ms, user_agent, request_headers, request_body_bytes, response_headers, response_body_bytes)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`

const listLogsQueryDesc = `
select * from gram.http_requests_raw
where project_id = $1
and ts >= $2
and ts <= $3
and ts < UUIDv7ToDateTime(toUUID($4))
order by ts desc
limit $5
`

const listLogsQueryAsc = `
select * from gram.http_requests_raw
where project_id = $1
and ts >= $2
and ts <= $3
and ts > UUIDv7ToDateTime(toUUID($4))
order by ts
limit $5
`

func (q *Queries) ShouldLog(ctx context.Context, orgId string) (bool, error) {
	return q.ShouldFlag(ctx, orgId)
}

// List retrieves tool logs based on the provided options.
func (q *Queries) List(ctx context.Context, opts ListToolLogsOptions) (res *ListResult, err error) {
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

	query := listLogsQueryDesc
	if pagination.SortOrder() == "ASC" {
		query = listLogsQueryAsc
	}

	perPage := pagination.Limit() - 1 // Remove the +1 for actual page size
	rows, err := q.conn.Query(ctx, query,
		projectID,
		tsStart,
		tsEnd,
		cursor,
		pagination.Limit())
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

// Log inserts a tool HTTP request log entry.
func (q *Queries) Log(ctx context.Context, log ToolHTTPRequest) (err error) {
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
