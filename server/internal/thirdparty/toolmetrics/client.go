package toolmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

type ToolLogLevel string
type ToolType string

const (
	ToolTypeHttp     ToolType = "http"
	ToolTypeFunction ToolType = "function"
	ToolTypePrompt   ToolType = "prompt"
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
	Conn   clickhouse.Conn
	Logger *slog.Logger
}

func (c *ClickhouseClient) List(ctx context.Context, projectID string, tsStart, tsEnd, cursor time.Time, pagination Pageable) (*ListResult, error) {
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
		c.Logger.DebugContext(ctx, "scan row")
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
		return fmt.Errorf("insert http raw: %w", err)
	}

	return nil
}

func (c *ClickhouseClient) Close() error {
	err := c.Conn.Close()
	if err != nil {
		return fmt.Errorf("close: %w", err)
	}
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
