package toolmetrics

import (
	"context"
	"fmt"
	"time"
)

// ToolLogLevel represents the log level for tool logs.
type ToolLogLevel string

// ToolType represents the type of tool.
type ToolType string

const (
	ToolTypeHTTP        ToolType = "http"
	ToolTypeFunction    ToolType = "function"
	ToolTypePrompt      ToolType = "prompt"
	ToolTypeExternalMCP ToolType = "external_mcp"
)

func (t *ToolType) Scan(src any) error {
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

// ToolHTTPRequest represents an HTTP request/response log entry.
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
	HTTPMethod    string  `ch:"http_method"`     // LowCardinality(String)
	HTTPServerURL string  `ch:"http_server_url"` // String
	HTTPRoute     string  `ch:"http_route"`      // String
	StatusCode    int64   `ch:"status_code"`     // Int64
	DurationMs    float64 `ch:"duration_ms"`     // Float64
	UserAgent     string  `ch:"user_agent"`      // LowCardinality(String)

	// request payload
	RequestHeaders   map[string]string `ch:"request_headers"`    // Map(String, String)
	RequestBodyBytes int64             `ch:"request_body_bytes"` // Int64

	// response payload
	ResponseHeaders   map[string]string `ch:"response_headers"`    // Map(String, String)
	ResponseBodyBytes int64             `ch:"response_body_bytes"` // Int64
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

// PaginationMetadata contains pagination metadata for list results.
type PaginationMetadata struct {
	PerPage        int     `json:"per_page"`
	HasNextPage    bool    `json:"has_next_page"`
	NextPageCursor *string `json:"next_page_cursor,omitempty"`
}

// ListResult contains the result of a list operation.
type ListResult struct {
	Logs       []ToolHTTPRequest  `json:"logs"`
	Pagination PaginationMetadata `json:"pagination"`
}

// ToolMetricsProvider defines the interface for tool metrics operations.
type ToolMetricsProvider interface {
	// List tool call logs
	List(ctx context.Context, opts ListToolLogsOptions) (*ListResult, error)
	// Log tool call request/response
	Log(context.Context, ToolHTTPRequest) error
	// ShouldLog returns true if the tool call should be logged
	ShouldLog(context.Context, string) (bool, error)
}
