package repo

import (
	"fmt"
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

// TelemetryLog represents a unified telemetry log entry (HTTP requests, function logs, etc.).
type TelemetryLog struct {
	// OTel Log Record Identity
	ID string `ch:"id"` // UUID

	// OTel Timestamp fields
	TimeUnixNano         int64 `ch:"time_unix_nano"`          // Int64
	ObservedTimeUnixNano int64 `ch:"observed_time_unix_nano"` // Int64

	// OTel Severity
	SeverityText *string `ch:"severity_text"` // LowCardinality(Nullable(String))

	// OTel Body (the actual log content/message)
	Body string `ch:"body"` // String

	// OTel Trace Context (for distributed tracing)
	TraceID *string `ch:"trace_id"` // Nullable(FixedString(32))
	SpanID  *string `ch:"span_id"`  // Nullable(FixedString(16))

	// OTel Attributes (log-level structured data - WHAT happened)
	Attributes string `ch:"attributes"` // JSON (stringified)

	// OTel Resource Attributes (WHO/WHERE generated this log)
	ResourceAttributes string `ch:"resource_attributes"` // JSON (stringified)

	// Denormalized Gram Fields (for fast filtering)
	GramProjectID    string  `ch:"gram_project_id"`    // UUID
	GramDeploymentID *string `ch:"gram_deployment_id"` // Nullable(UUID)
	GramFunctionID   *string `ch:"gram_function_id"`   // Nullable(UUID)
	GramURN          string  `ch:"gram_urn"`           // String
	ServiceName      string  `ch:"service_name"`       // LowCardinality(String)
	ServiceVersion   *string `ch:"service_version"`    // Nullable(String)

	// Denormalized HTTP Fields (Wide Event Pattern - for HTTP logs only, NULL for function logs)
	HTTPRequestMethod      *string `ch:"http_request_method"`       // LowCardinality(Nullable(String))
	HTTPResponseStatusCode *int32  `ch:"http_response_status_code"` // Nullable(Int32)
	HTTPRoute              *string `ch:"http_route"`                // Nullable(String)
	HTTPServerURL          *string `ch:"http_server_url"`           // Nullable(String)
}

// TraceSummary represents an aggregated view of a trace (one row per trace).
// Used for displaying a list of logs grouped by trace in the UI.
type TraceSummary struct {
	TraceID           string `ch:"trace_id"`             // FixedString(32)
	StartTimeUnixNano int64  `ch:"start_time_unix_nano"` // Int64 - earliest log timestamp
	LogCount          uint64 `ch:"log_count"`            // UInt64 - total logs in trace
	HTTPStatusCode    *int32 `ch:"http_status_code"`     // Nullable(Int32) - any HTTP status code
	GramURN           string `ch:"gram_urn"`             // String - any gram_urn from the trace
}
