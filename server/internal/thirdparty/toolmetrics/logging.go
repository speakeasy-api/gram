package toolmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel/trace"
)

// ToolCallLogger represents a logging strategy for tool HTTP requests.
// Implementations may be backed by a real ToolHTTPRequest or behave as no-ops.
type ToolCallLogger interface {
	Enabled() bool
	Emit(ctx context.Context, logger *slog.Logger)
	RequestBodyBytes() int64
	ResponseBodyBytes() int64
	RecordDurationMs(durationMs float64)
	RecordHTTPMethod(method string)
	RecordHTTPRoute(route string)
	RecordStatusCode(code int)
	RecordUserAgent(agent string)
	RecordRequestHeaders(headers map[string]string)
	RecordResponseHeaders(headers map[string]string)
	RecordRequestBodyBytes(bytes int64)
	RecordResponseBodyBytes(bytes int64)
}

type toolCallLogger struct {
	entry    *ToolHTTPRequest
	provider ToolMetricsProvider
	toolName string
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
	toolType ToolType,
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

func (l *toolCallLogger) RequestBodyBytes() int64 {
	if l.entry == nil {
		return 0
	}
	return l.entry.RequestBodyBytes
}

func (l *toolCallLogger) ResponseBodyBytes() int64 {
	if l.entry == nil {
		return 0
	}
	return l.entry.ResponseBodyBytes
}

func (l *toolCallLogger) RecordHTTPMethod(method string) {
	if l.entry == nil {
		return
	}
	l.entry.WithHTTPMethod(method)
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

func (l *toolCallLogger) RecordRequestHeaders(headers map[string]string) {
	if l.entry == nil {
		return
	}
	l.entry.RequestHeaders = headers
}

func (l *toolCallLogger) RecordResponseHeaders(headers map[string]string) {
	if l.entry == nil {
		return
	}
	l.entry.ResponseHeaders = headers
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

func (l *noopToolCallLogger) RequestBodyBytes() int64                 { return 0 }
func (l *noopToolCallLogger) ResponseBodyBytes() int64                { return 0 }
func (l *noopToolCallLogger) RecordDurationMs(float64)                {}
func (l *noopToolCallLogger) RecordHTTPMethod(string)                 {}
func (l *noopToolCallLogger) RecordHTTPRoute(string)                  {}
func (l *noopToolCallLogger) RecordStatusCode(int)                    {}
func (l *noopToolCallLogger) RecordUserAgent(string)                  {}
func (l *noopToolCallLogger) RecordRequestHeaders(map[string]string)  {}
func (l *noopToolCallLogger) RecordResponseHeaders(map[string]string) {}
func (l *noopToolCallLogger) RecordRequestBodyBytes(int64)            {}
func (l *noopToolCallLogger) RecordResponseBodyBytes(int64)           {}
func (l *noopToolCallLogger) Enabled() bool                           { return false }

// newToolLog initializes a ToolHTTPRequest with common metadata before the HTTP round tripper executes.
func newToolLog(ctx context.Context, tool ToolInfo, toolType ToolType) (*ToolHTTPRequest, error) {
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

	return &ToolHTTPRequest{
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
		HTTPMethod:        "",
		HTTPRoute:         "",
		RequestHeaders:    map[string]string{},
		RequestBodyBytes:  0,
		ResponseHeaders:   map[string]string{},
		ResponseBodyBytes: 0,
	}, nil
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

// EmitHTTPRequestLog logs the provided HTTP request using the tool metrics provider.
// Errors are reported through the supplied logger. Logging happens asynchronously to
// avoid blocking the caller and the request struct is copied to prevent data races.
func EmitHTTPRequestLog(
	ctx context.Context,
	logger *slog.Logger,
	provider ToolMetricsProvider,
	toolName string,
	request ToolHTTPRequest,
) {
	if provider == nil || request.ID == "" {
		return
	}

	go func() {
		logCtx := context.WithoutCancel(ctx)

		if err := provider.Log(logCtx, request); err != nil {
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
