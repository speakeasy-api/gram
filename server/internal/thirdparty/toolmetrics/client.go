package toolmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Log attribute keys for tool metrics operations
const (
	attrTsStart          = "ts_start"
	attrTsEnd            = "ts_end"
	attrResultCount      = "result_count"
	attrHasNextPage      = "has_next_page"
	attrQueryDurationMs  = "query_duration_ms"
	attrInsertDurationMs = "insert_duration_ms"
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

type ToolMetricsClient interface {
	Close() error
	// List tool call logs
	List(ctx context.Context, projectID string, tsStart, tsEnd, cursor time.Time, pagination Pageable) (*ListResult, error)
	// Log tool call request/response
	Log(context.Context, ToolHTTPRequest) error
	// Feature feature flag provider
	Feature() feature.Provider
}

type PaginationMetadata struct {
	PerPage        int        `json:"per_page"`
	HasNextPage    bool       `json:"has_next_page"`
	NextPageCursor *time.Time `json:"next_page_cursor,omitempty"`
}

type ListResult struct {
	Logs       []ToolHTTPRequest  `json:"logs"`
	Pagination PaginationMetadata `json:"pagination"`
}

type StubToolMetricsClient struct{}

func (n *StubToolMetricsClient) Feature() feature.Provider {
	return &feature.InMemory{}
}

func (n *StubToolMetricsClient) List(_ context.Context, _ string, _ time.Time, _ time.Time, _ time.Time, p Pageable) (*ListResult, error) {
	return &ListResult{
		Logs: []ToolHTTPRequest{},
		Pagination: PaginationMetadata{
			PerPage:        p.Limit() - 1, // Remove the +1 we added for detection
			HasNextPage:    false,
			NextPageCursor: nil,
		},
	}, nil
}

func (n *StubToolMetricsClient) Log(_ context.Context, _ ToolHTTPRequest) error {
	return nil
}

func (n *StubToolMetricsClient) Close() error {
	return nil
}

type ClickhouseClient struct {
	Conn     clickhouse.Conn
	Logger   *slog.Logger
	Features feature.Provider
}

func (c *ClickhouseClient) Feature() feature.Provider {
	return c.Features
}

func (c *ClickhouseClient) List(ctx context.Context, projectID string, tsStart, tsEnd, cursor time.Time, pagination Pageable) (*ListResult, error) {
	tracer := otel.Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics")
	ctx, span := tracer.Start(ctx, "clickhouse.list_logs",
		trace.WithAttributes(
			attribute.String("project_id", projectID),
			attribute.String("ts_start", tsStart.Format(time.RFC3339)),
			attribute.String("ts_end", tsEnd.Format(time.RFC3339)),
			attribute.String("cursor", cursor.Format(time.RFC3339)),
			attribute.Int("limit", pagination.Limit()),
			attribute.String("sort_order", pagination.SortOrder()),
		),
	)
	defer span.End()

	startTime := time.Now()

	query := listLogsQueryDesc
	if pagination.SortOrder() == "ASC" {
		query = listLogsQueryAsc
	}

	perPage := pagination.Limit() - 1 // Remove the +1 for actual page size
	rows, err := c.Conn.Query(ctx, query, projectID,
		tsStart,
		tsEnd,
		cursor,
		pagination.Limit())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to query logs")
		c.Logger.ErrorContext(ctx, "failed to query tool logs from ClickHouse",
			attr.SlogError(err),
			attr.SlogProjectID(projectID),
			slog.String(attrTsStart, tsStart.Format(time.RFC3339)),
			slog.String(attrTsEnd, tsEnd.Format(time.RFC3339)),
		)
		return nil, fmt.Errorf("query logs: %w", err)
	}

	defer func(rows driver.Rows, logger *slog.Logger) {
		err = rows.Close()
		if err != nil {
			logger.ErrorContext(ctx, "failed to close rows", attr.SlogError(err))
		}
	}(rows, c.Logger)

	var results []ToolHTTPRequest
	for rows.Next() {
		var log ToolHTTPRequest
		if err = rows.ScanStruct(&log); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to scan row")
			c.Logger.ErrorContext(ctx, "failed to scan row",
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
		c.Logger.ErrorContext(ctx, "error iterating rows",
			attr.SlogError(err),
			attr.SlogProjectID(projectID),
		)
		return nil, fmt.Errorf("rows error: %w", err)
	}

	// Calculate pagination metadata
	hasNextPage := len(results) > perPage

	// Trim to actual page size if we fetched extra for detection
	if hasNextPage {
		results = results[:perPage]
	}

	var nextPageCursor *time.Time
	if len(results) > 0 {
		// Next cursor is the timestamp of the last record
		if hasNextPage {
			lastTs := results[len(results)-1].Ts
			nextPageCursor = &lastTs
		}
	}

	queryDuration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("result_count", len(results)),
		attribute.Bool("has_next_page", hasNextPage),
		attribute.Float64("query_duration_ms", float64(queryDuration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "")

	c.Logger.InfoContext(ctx, "successfully listed tool logs",
		attr.SlogProjectID(projectID),
		slog.Int(attrResultCount, len(results)),
		slog.Bool(attrHasNextPage, hasNextPage),
		slog.Float64(attrQueryDurationMs, float64(queryDuration.Milliseconds())),
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
	tracer := otel.Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics")
	ctx, span := tracer.Start(ctx, "clickhouse.log_http_request",
		trace.WithAttributes(
			attribute.String("tool_id", log.ToolID),
			attribute.String("tool_urn", log.ToolURN),
			attribute.String("project_id", log.ProjectID),
			attribute.String("organization_id", log.OrganizationID),
			attribute.String("http_method", log.HTTPMethod),
			attribute.String("http_route", log.HTTPRoute),
			attribute.Int("status_code", int(log.StatusCode)),
			attribute.Float64("duration_ms", log.DurationMs),
		),
	)
	defer span.End()

	startTime := time.Now()

	args := []any{
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
		log.ClientIPv4,
		log.RequestHeaders,
		log.RequestBody,
		log.RequestBodySkip,
		log.RequestBodyBytes,
		log.ResponseHeaders,
		log.ResponseBody,
		log.ResponseBodySkip,
		log.ResponseBodyBytes,
	}

	err := c.Conn.Exec(ctx, insertHttpRaw, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to insert log")
		c.Logger.ErrorContext(ctx, "failed to insert HTTP log to ClickHouse",
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
		attribute.Float64("insert_duration_ms", float64(insertDuration.Milliseconds())),
		attribute.Int64("request_body_bytes", int64(log.RequestBodyBytes)),   //nolint:gosec // uint64 to int64 conversion is safe for body sizes
		attribute.Int64("response_body_bytes", int64(log.ResponseBodyBytes)), //nolint:gosec // uint64 to int64 conversion is safe for body sizes
	)
	span.SetStatus(codes.Ok, "")

	c.Logger.DebugContext(ctx, "successfully logged HTTP request to ClickHouse",
		attr.SlogToolID(log.ToolID),
		attr.SlogToolURN(log.ToolURN),
		attr.SlogProjectID(log.ProjectID),
		attr.SlogHTTPRequestMethod(log.HTTPMethod),
		attr.SlogHTTPResponseStatusCode(int(log.StatusCode)),
		slog.Float64(attrInsertDurationMs, float64(insertDuration.Milliseconds())),
	)

	return nil
}

func (c *ClickhouseClient) Close() error {
	c.Logger.InfoContext(context.Background(), "closing ClickHouse connection")
	err := c.Conn.Close()
	if err != nil {
		c.Logger.ErrorContext(context.Background(), "failed to close ClickHouse connection",
			attr.SlogError(err),
		)
		return fmt.Errorf("close: %w", err)
	}
	c.Logger.InfoContext(context.Background(), "successfully closed ClickHouse connection")
	return nil
}

type ToolHTTPRequest struct {
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
	StatusCode uint16  `ch:"status_code"` // UInt16
	DurationMs float64 `ch:"duration_ms"` // Float64
	UserAgent  string  `ch:"user_agent"`  // LowCardinality(String)
	ClientIPv4 string  `ch:"client_ipv4"` // IPv4

	// request payload
	RequestHeaders   map[string]string `ch:"request_headers"`    // Map(String, String)
	RequestBody      *string           `ch:"request_body"`       // Nullable(String)
	RequestBodySkip  *string           `ch:"request_body_skip"`  // Nullable(String)
	RequestBodyBytes uint64            `ch:"request_body_bytes"` // UInt64

	// response payload
	ResponseHeaders   map[string]string `ch:"response_headers"`    // Map(String, String)
	ResponseBody      *string           `ch:"response_body"`       // Nullable(String)
	ResponseBodySkip  *string           `ch:"response_body_skip"`  // Nullable(String)
	ResponseBodyBytes uint64            `ch:"response_body_bytes"` // UInt64
}
