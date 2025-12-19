package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"go.opentelemetry.io/otel/trace"
)

const (
	redactRevealPrefixLen = 3
	redactMinTokenLen     = 10
)

// ToolMetricsProvider defines the interface for tool metrics operations.
type ToolMetricsProvider interface {
	// List tool call logs
	ListHTTPRequests(ctx context.Context, opts repo.ListToolLogsOptions) (*repo.ListResult, error)
	// List structured tool logs
	ListToolLogs(ctx context.Context, params repo.ListToolLogsParams) (*repo.ToolLogsListResult, error)
	// List unified telemetry logs (new OTel-based table)
	ListTelemetryLogs(ctx context.Context, params repo.ListTelemetryLogsParams) ([]repo.TelemetryLog, error)
	// List trace summaries for distributed tracing
	ListTraces(ctx context.Context, params repo.ListTracesParams) ([]repo.TraceSummary, error)
	// List all logs for a specific trace ID
	ListLogsForTrace(ctx context.Context, params repo.ListLogsForTraceParams) ([]repo.TelemetryLog, error)
	// Log tool call request/response
	LogHTTPRequest(context.Context, repo.ToolHTTPRequest) error
	// ShouldLog returns true if the tool call should be logged
	ShouldLog(context.Context, string) (bool, error)
}

// ToolCallLogger represents a logging strategy for tool HTTP requests.
// Implementations may be backed by a real ToolHTTPRequest or behave as no-ops.
type ToolCallLogger interface {
	Enabled() bool
	Emit(ctx context.Context, logger *slog.Logger)
	RecordDurationMs(durationMs float64)
	RecordHTTPServerURL(url string)
	RecordHTTPMethod(method string)
	RecordHTTPRoute(route string)
	RecordStatusCode(code int)
	RecordUserAgent(agent string)
	RecordRequestHeaders(headers map[string]string, isSensitive bool)
	RecordResponseHeaders(headers map[string]string)
	RecordRequestBodyBytes(bytes int64)
	RecordResponseBodyBytes(bytes int64)
}

type toolCallLogger struct {
	entry    *repo.ToolHTTPRequest
	provider ToolMetricsProvider
	toolName string
	toolType repo.ToolType
}

var _ ToolCallLogger = (*toolCallLogger)(nil)

// NewToolCallLogger returns a ToolCallLogger that records tool calls when logging is
// enabled for the organization; otherwise it returns a no-op logger. When an error
// occurs while preparing the logger, a no-op logger is returned alongside the error.
func NewToolCallLogger(
	ctx context.Context,
	provider ToolMetricsProvider,
	organizationID string,
	info ToolInfo,
	toolName string,
	toolType repo.ToolType,
) (ToolCallLogger, error) {
	noop := NewNoopToolCallLogger()
	if provider == nil || toolType == "" {
		return noop, nil
	}

	shouldLog, err := provider.ShouldLog(ctx, organizationID)
	if err != nil {
		return noop, fmt.Errorf("failed to determine if organization is allowed to request log: %w", err)
	}
	if !shouldLog {
		return noop, nil
	}

	entry, err := newToolLog(ctx, info, toolType)
	if err != nil {
		return noop, err
	}

	return &toolCallLogger{
		entry:    entry,
		provider: provider,
		toolName: toolName,
		toolType: toolType,
	}, nil
}

func (l *toolCallLogger) Enabled() bool {
	return true
}

func (l *toolCallLogger) Emit(ctx context.Context, logger *slog.Logger) {
	if l.provider == nil || l.entry == nil || l.entry.ID == "" {
		return
	}
	EmitHTTPRequestLog(ctx, logger, l.provider, l.toolName, *l.entry)
}

func (l *toolCallLogger) RecordHTTPMethod(method string) {
	if l.entry == nil {
		return
	}
	l.entry.WithHTTPMethod(method)
}

func (l *toolCallLogger) RecordHTTPServerURL(url string) {
	// currently we onyl want to record this server URL for HTTP tool types
	// Not exposing fly function details unnecessarily
	if l.toolType != repo.ToolTypeHTTP {
		return
	}
	if l.entry == nil {
		return
	}
	l.entry.HTTPServerURL = url
}

func (l *toolCallLogger) RecordHTTPRoute(route string) {
	if l.entry == nil {
		return
	}
	l.entry.WithHTTPRoute(route)
}

func (l *toolCallLogger) RecordDurationMs(durationMs float64) {
	if l.entry == nil {
		return
	}
	l.entry.DurationMs = durationMs
}

func (l *toolCallLogger) RecordStatusCode(code int) {
	if l.entry == nil {
		return
	}
	l.entry.WithStatusCode(int64(code))
}

func (l *toolCallLogger) RecordUserAgent(agent string) {
	if l.entry == nil {
		return
	}
	l.entry.UserAgent = agent
}

func (l *toolCallLogger) RecordRequestHeaders(headers map[string]string, isSensitive bool) {
	if l.entry == nil {
		return
	}
	if len(headers) == 0 {
		return
	}
	if l.entry.RequestHeaders == nil {
		l.entry.RequestHeaders = make(map[string]string, len(headers))
	}
	for header, value := range headers {
		if isSensitive {
			value = redactToken(value)
		}
		l.entry.RequestHeaders[header] = value
	}
}

func (l *toolCallLogger) RecordResponseHeaders(headers map[string]string) {
	if l.entry == nil {
		return
	}
	if len(headers) == 0 {
		return
	}
	if l.entry.ResponseHeaders == nil {
		l.entry.ResponseHeaders = make(map[string]string, len(headers))
	}
	maps.Copy(l.entry.ResponseHeaders, headers)
}

func (l *toolCallLogger) RecordRequestBodyBytes(bytes int64) {
	if l.entry == nil {
		return
	}
	l.entry.RequestBodyBytes = bytes
}

func (l *toolCallLogger) RecordResponseBodyBytes(bytes int64) {
	if l.entry == nil {
		return
	}
	l.entry.ResponseBodyBytes = bytes
}

// NewNoopToolCallLogger creates a ToolCallLogger that drops all logging information.
func NewNoopToolCallLogger() ToolCallLogger {
	return &noopToolCallLogger{}
}

type noopToolCallLogger struct {
}

var _ ToolCallLogger = (*noopToolCallLogger)(nil)

func (l *noopToolCallLogger) Emit(context.Context, *slog.Logger) {}

func (l *noopToolCallLogger) RecordDurationMs(float64)                     {}
func (l *noopToolCallLogger) RecordHTTPServerURL(string)                   {}
func (l *noopToolCallLogger) RecordHTTPMethod(string)                      {}
func (l *noopToolCallLogger) RecordHTTPRoute(string)                       {}
func (l *noopToolCallLogger) RecordStatusCode(int)                         {}
func (l *noopToolCallLogger) RecordUserAgent(string)                       {}
func (l *noopToolCallLogger) RecordRequestHeaders(map[string]string, bool) {}
func (l *noopToolCallLogger) RecordResponseHeaders(map[string]string)      {}
func (l *noopToolCallLogger) RecordRequestBodyBytes(int64)                 {}
func (l *noopToolCallLogger) RecordResponseBodyBytes(int64)                {}
func (l *noopToolCallLogger) Enabled() bool                                { return false }

// newToolLog initializes a ToolHTTPRequest with common metadata before the HTTP round tripper executes.
func newToolLog(ctx context.Context, tool ToolInfo, toolType repo.ToolType) (*repo.ToolHTTPRequest, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate tool http request id: %w", err)
	}

	spanCtx := trace.SpanFromContext(ctx).SpanContext()
	var traceID, spanID string
	if spanCtx.HasTraceID() {
		traceID = spanCtx.TraceID().String()
	}
	if spanCtx.HasSpanID() {
		spanID = spanCtx.SpanID().String()
	}

	return &repo.ToolHTTPRequest{
		ID:                id.String(),
		Ts:                time.Unix(id.Time().UnixTime()),
		OrganizationID:    tool.OrganizationID,
		ProjectID:         tool.ProjectID,
		DeploymentID:      tool.DeploymentID,
		ToolID:            tool.ID,
		ToolURN:           tool.Urn,
		ToolType:          toolType,
		TraceID:           traceID,
		SpanID:            spanID,
		StatusCode:        0,
		DurationMs:        0,
		UserAgent:         "",
		HTTPServerURL:     "",
		HTTPMethod:        "",
		HTTPRoute:         "",
		RequestHeaders:    map[string]string{},
		RequestBodyBytes:  0,
		ResponseHeaders:   map[string]string{},
		ResponseBodyBytes: 0,
	}, nil
}

// EmitHTTPRequestLog logs the provided HTTP request using the tool metrics provider.
// Errors are reported through the supplied logger. Logging happens asynchronously to
// avoid blocking the caller and the request struct is copied to prevent data races.
func EmitHTTPRequestLog(
	ctx context.Context,
	logger *slog.Logger,
	provider ToolMetricsProvider,
	toolName string,
	request repo.ToolHTTPRequest,
) {
	if provider == nil || request.ID == "" {
		return
	}

	go func() {
		logCtx := context.WithoutCancel(ctx)

		if err := provider.LogHTTPRequest(logCtx, request); err != nil {
			logger.ErrorContext(logCtx,
				"failed to log HTTP attempt to ClickHouse",
				attr.SlogError(err),
				attr.SlogToolURN(request.ToolURN),
				attr.SlogToolName(toolName),
				attr.SlogHTTPRequestMethod(request.HTTPMethod),
			)
			return
		}

		logger.DebugContext(logCtx,
			"logged HTTP request to ClickHouse",
			attr.SlogToolURN(request.ToolURN),
			attr.SlogToolName(toolName),
			attr.SlogHTTPRequestMethod(request.HTTPMethod),
			attr.SlogHTTPResponseStatusCode(int(request.StatusCode)),
		)
	}()
}

// reasonable redaction of tokens function for tool call logs
func redactToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return trimmed
	}

	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"bearer ", "basic "} {
		if strings.HasPrefix(lower, prefix) {
			actualPrefix := trimmed[:len(prefix)]
			remainder := strings.TrimSpace(trimmed[len(prefix):])
			if len(remainder) < redactMinTokenLen {
				return actualPrefix + "***"
			}
			return actualPrefix + remainder[:redactRevealPrefixLen] + "***"
		}
	}

	if len(trimmed) < redactMinTokenLen {
		return "***"
	}

	return trimmed[:redactRevealPrefixLen] + "***"
}
