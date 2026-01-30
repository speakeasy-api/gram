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
	GramChatID       *string `ch:"gram_chat_id"`       // Nullable(String)
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

// ChatSummary represents an aggregated view of a chat session (one row per gram_chat_id).
// Used for displaying a list of chat sessions in the UI.
type ChatSummary struct {
	GramChatID        string  `ch:"gram_chat_id"`
	StartTimeUnixNano int64   `ch:"start_time_unix_nano"`
	EndTimeUnixNano   int64   `ch:"end_time_unix_nano"`
	LogCount          uint64  `ch:"log_count"`
	ToolCallCount     uint64  `ch:"tool_call_count"`
	UserID            *string `ch:"user_id"`
	TotalInputTokens  int64   `ch:"total_input_tokens"`
	TotalOutputTokens int64   `ch:"total_output_tokens"`
}

// MetricsSummaryRow represents aggregated AI metrics from ClickHouse.
// Used for the getAIMetrics endpoint.
type MetricsSummaryRow struct {
	// Cardinality metrics (project scope only)
	TotalChats        uint64 `ch:"total_chats"`
	DistinctModels    uint64 `ch:"distinct_models"`
	DistinctProviders uint64 `ch:"distinct_providers"`

	// Token metrics
	TotalInputTokens  int64   `ch:"total_input_tokens"`
	TotalOutputTokens int64   `ch:"total_output_tokens"`
	TotalTokens       int64   `ch:"total_tokens"`
	AvgTokensPerReq   float64 `ch:"avg_tokens_per_request"`

	// Chat request metrics
	TotalChatRequests uint64  `ch:"total_chat_requests"`
	AvgChatDurationMs float64 `ch:"avg_chat_duration_ms"`

	// Resolution status
	FinishReasonStop      uint64 `ch:"finish_reason_stop"`
	FinishReasonToolCalls uint64 `ch:"finish_reason_tool_calls"`

	// Tool call metrics
	TotalToolCalls    uint64  `ch:"total_tool_calls"`
	ToolCallSuccess   uint64  `ch:"tool_call_success"`
	ToolCallFailure   uint64  `ch:"tool_call_failure"`
	AvgToolDurationMs float64 `ch:"avg_tool_duration_ms"`

	// Model breakdown (map of model name -> count)
	Models map[string]uint64 `ch:"models"`

	// Tool breakdowns (maps of tool URN -> count)
	ToolCounts        map[string]uint64 `ch:"tool_counts"`
	ToolSuccessCounts map[string]uint64 `ch:"tool_success_counts"`
	ToolFailureCounts map[string]uint64 `ch:"tool_failure_counts"`
}
