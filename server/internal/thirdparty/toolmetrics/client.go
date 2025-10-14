package toolmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type ToolLogLevel string
type ToolType string

const (
	HTTPToolType     ToolType = "http"
	FunctionToolType ToolType = "function"
	PromptToolType   ToolType = "prompt"
)

func (t *ToolType) Scan(src interface{}) error {
	if src == nil {
		*t = ""
		return nil
	}
	switch v := src.(type) {
	case string:
		*t = ToolType(v)
		return nil
	case []byte:
		*t = ToolType(v)
		return nil
	default:
		return fmt.Errorf("cannot scan %T into ToolType", src)
	}
}

type ListToolLogsOptions struct {
	ProjectID string
	TsStart   time.Time
	TsEnd     time.Time
	Cursor    string
	*Pagination
}

type ToolMetricsProvider interface {
	// List tool call logs
	List(ctx context.Context, opts ListToolLogsOptions) (*ListResult, error)
	// Log tool call request/response
	Log(context.Context, ToolHTTPRequest) error
}

type PaginationMetadata struct {
	PerPage        int     `json:"per_page"`
	HasNextPage    bool    `json:"has_next_page"`
	NextPageCursor *string `json:"next_page_cursor,omitempty"`
}

type ListResult struct {
	Logs       []ToolHTTPRequest  `json:"logs"`
	Pagination PaginationMetadata `json:"pagination"`
}

func New(logger *slog.Logger, conn clickhouse.Conn, traceProvider trace.TracerProvider, shouldFlag func(ctx context.Context, log ToolHTTPRequest) (bool, error)) *ClickhouseClient {
	if shouldFlag == nil {
		shouldFlag = func(ctx context.Context, log ToolHTTPRequest) (bool, error) {
			return true, nil
		}
	}

	tracer := traceProvider.Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics")

	return &ClickhouseClient{
		logger:     logger,
		conn:       conn,
		tracer:     tracer,
		ShouldFlag: shouldFlag,
	}
}

type ClickhouseClient struct {
	logger     *slog.Logger
	conn       clickhouse.Conn
	tracer     trace.Tracer
	ShouldFlag func(ctx context.Context, log ToolHTTPRequest) (bool, error)
}

func (c *ClickhouseClient) List(ctx context.Context, opts ListToolLogsOptions) (*ListResult, error) {
	projectID := opts.ProjectID
	tsStart := opts.TsStart
	tsEnd := opts.TsEnd
	cursor := opts.Cursor
	pagination := opts.Pagination

	ctx, span := c.tracer.Start(ctx, "clickhouse.list_logs",
		trace.WithAttributes(
			attr.ProjectID(projectID),
			attr.TsStart(tsStart),
			attr.TsEnd(tsEnd),
			attr.Cursor(cursor),
			attr.Limit(pagination.Limit()),
			attr.SortOrder(pagination.SortOrder()),
		),
	)
	defer span.End()

	startTime := time.Now()

	query := listLogsQueryDesc
	if pagination.SortOrder() == "ASC" {
		query = listLogsQueryAsc
	}

	perPage := pagination.Limit() - 1 // Remove the +1 for actual page size
	rows, err := c.conn.Query(ctx, query,
		projectID,
		tsStart,
		tsEnd,
		cursor,
		pagination.Limit())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to query logs")
		c.logger.ErrorContext(ctx, "failed to query tool logs from ClickHouse",
			attr.SlogError(err),
			attr.SlogProjectID(projectID),
			attr.SlogTsStart(tsStart),
			attr.SlogTsEnd(tsEnd),
		)
		return nil, fmt.Errorf("query logs: %w", err)
	}

	defer o11y.LogDefer(ctx, c.logger, func() error { return rows.Close() })

	var results []ToolHTTPRequest
	for rows.Next() {
		var log ToolHTTPRequest
		if err = rows.ScanStruct(&log); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to scan row")
			c.logger.ErrorContext(ctx, "failed to scan row",
				attr.SlogError(err),
				attr.SlogProjectID(projectID),
			)
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		results = append(results, log)
	}

	if err = rows.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "rows iteration error")
		c.logger.ErrorContext(ctx, "error iterating rows",
			attr.SlogError(err),
			attr.SlogProjectID(projectID),
		)
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

	c.logger.InfoContext(ctx, "successfully listed tool logs",
		attr.SlogProjectID(projectID),
		attr.SlogValueInt(len(results)),
		attr.SlogPaginationHasNextPage(hasNextPage),
		attr.SlogClickhouseQueryDurationMs(float64(queryDuration.Milliseconds())),
	)

	return &ListResult{
		Logs: results,
		Pagination: PaginationMetadata{
			PerPage:        perPage,
			HasNextPage:    hasNextPage,
			NextPageCursor: nextPageCursor,
		},
	}, nil
}

func (c *ClickhouseClient) Log(ctx context.Context, log ToolHTTPRequest) error {
	allow, err := c.ShouldFlag(ctx, log)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to fetch feature flag", attr.SlogError(err))
		return nil
	}

	if !allow {
		return nil
	}

	ctx, span := c.tracer.Start(ctx, "clickhouse.log_http_request",
		trace.WithAttributes(
			attr.ToolID(log.ToolID),
			attr.ToolURN(log.ToolURN),
			attr.ProjectID(log.ProjectID),
			attr.OrganizationID(log.OrganizationID),
			attr.HTTPRequestMethod(log.HTTPMethod),
			attr.HTTPRoute(log.HTTPRoute),
			attr.HTTPResponseStatusCode(int(log.StatusCode)),
			attr.HTTPRequestDurationMs(log.DurationMs),
		),
	)
	defer span.End()

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

	err = c.conn.Exec(ctx, insertHttpRaw, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to insert log")
		c.logger.ErrorContext(ctx, "failed to insert HTTP log to ClickHouse",
			attr.SlogError(err),
			attr.SlogToolID(log.ToolID),
			attr.SlogToolURN(log.ToolURN),
			attr.SlogProjectID(log.ProjectID),
			attr.SlogHTTPRequestMethod(log.HTTPMethod),
			attr.SlogHTTPResponseStatusCode(int(log.StatusCode)),
		)
		return fmt.Errorf("insert http raw: %w", err)
	}

	insertDuration := time.Since(startTime)
	span.SetAttributes(
		attr.ClickhouseQueryDurationMs(float64(insertDuration.Milliseconds())),
		attr.HTTPRequestBodyBytes(int(log.RequestBodyBytes)),
		attr.HTTPResponseBodyBytes(int(log.ResponseBodyBytes)),
	)
	span.SetStatus(codes.Ok, "")

	return nil
}

func (c *ClickhouseClient) Close() error {
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("close clickhouse client: %w", err)
	}
	return nil
}

type ToolHTTPRequest struct {
	ID string    `ch:"id"` // UUID
	Ts time.Time `ch:"ts"` // DateTime64(3, 'UTC')

	// required multi-tenant keys
	OrganizationID string   `ch:"organization_id"` // UUID
	ProjectID      string   `ch:"project_id"`      // UUID
	DeploymentID   string   `ch:"deployment_id"`   // UUID
	ToolID         string   `ch:"tool_id"`         // UUID
	ToolURN        string   `ch:"tool_urn"`        // String
	ToolType       ToolType `ch:"tool_type"`       // LowCardinality(String)

	// correlation
	TraceID string `ch:"trace_id"` // FixedString(32)
	SpanID  string `ch:"span_id"`  // FixedString(16)

	// request metadata
	HTTPMethod string  `ch:"http_method"` // LowCardinality(String)
	HTTPRoute  string  `ch:"http_route"`  // String
	StatusCode int64   `ch:"status_code"` // Int64
	DurationMs float64 `ch:"duration_ms"` // Float64
	UserAgent  string  `ch:"user_agent"`  // LowCardinality(String)

	// request payload
	RequestHeaders   map[string]string `ch:"request_headers"`    // Map(String, String)
	RequestBodyBytes int64             `ch:"request_body_bytes"` // Int64

	// response payload
	ResponseHeaders   map[string]string `ch:"response_headers"`    // Map(String, String)
	ResponseBodyBytes int64             `ch:"response_body_bytes"` // Int64
}
