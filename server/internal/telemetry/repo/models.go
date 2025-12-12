package repo

import (
	"fmt"
	"time"
)

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

// WithStatusCode sets the HTTP status code on the log entry.
func (t *ToolHTTPRequest) WithStatusCode(code int64) *ToolHTTPRequest {
	t.StatusCode = code
	return t
}

// WithHTTPMethod sets the HTTP method on the log entry.
func (t *ToolHTTPRequest) WithHTTPMethod(method string) *ToolHTTPRequest {
	t.HTTPMethod = method
	return t
}

// WithHTTPRoute sets the HTTP route on the log entry.
func (t *ToolHTTPRequest) WithHTTPRoute(route string) *ToolHTTPRequest {
	t.HTTPRoute = route
	return t
}

// ToolLog represents a log entry from the tool_logs table.
type ToolLog struct {
	ID         string    `ch:"id"`         // UUID
	Timestamp  time.Time `ch:"timestamp"`  // DateTime64(3, 'UTC')
	Instance   string    `ch:"instance"`   // String
	Level      string    `ch:"level"`      // LowCardinality(String)
	Source     string    `ch:"source"`     // LowCardinality(String)
	RawLog     string    `ch:"raw_log"`    // String
	Message    *string   `ch:"message"`    // Nullable(String)
	Attributes string    `ch:"attributes"` // JSON

	ProjectID    string `ch:"project_id"`    // UUID
	DeploymentID string `ch:"deployment_id"` // UUID
	FunctionID   string `ch:"function_id"`   // UUID
}

// HTTPRequestListResult contains the result of an HTTP request list operation.
type HTTPRequestListResult struct {
	Logs       []ToolHTTPRequest  `json:"logs"`
	Pagination PaginationMetadata `json:"pagination"`
}

// ToolLogsListResult contains the result of a tool logs list operation.
type ToolLogsListResult struct {
	Logs       []ToolLog          `json:"logs"`
	Pagination PaginationMetadata `json:"pagination"`
}

// ListResult contains the result of a list operation.
type ListResult struct {
	Logs       []ToolHTTPRequest  `json:"logs"`
	Pagination PaginationMetadata `json:"pagination"`
}
