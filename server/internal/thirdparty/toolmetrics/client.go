package toolmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

type ToolLogLevel string
type ToolType string

const (
	ToolTypeHttp     ToolType = "http"
	ToolTypeFunction ToolType = "function"
	ToolTypePrompt   ToolType = "prompt"
)

type ToolMetricsClient interface {
	Close() error
	// List tool call logs
	List(context.Context, string, ...any) ([]any, error)
	// Log tool call request/response
	Log(context.Context, ToolHTTPRequest) error
}

type StubToolMetricsClient struct{}

func (n *StubToolMetricsClient) List(context.Context, string, ...any) ([]any, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) Log(context.Context, ToolHTTPRequest) error {
	return nil
}

func (n *StubToolMetricsClient) Close() error {
	return nil
}

type ClickhouseClient struct {
	Conn   clickhouse.Conn
	Logger *slog.Logger
}

func (c *ClickhouseClient) List(context.Context, string, ...any) ([]any, error) {
	return nil, nil
}

func (c *ClickhouseClient) Log(ctx context.Context, log ToolHTTPRequest) error {
	args := []any{
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
