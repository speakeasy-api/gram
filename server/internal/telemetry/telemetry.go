package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"go.opentelemetry.io/otel/trace"
)

const (
	redactRevealPrefixLen = 3
	redactMinTokenLen     = 10
)

// ToolMetricsProvider defines the interface for tool metrics operations.
type ToolMetricsProvider interface {
	// List unified telemetry logs (new OTel-based table)
	ListTelemetryLogs(ctx context.Context, params repo.ListTelemetryLogsParams) ([]repo.TelemetryLog, error)
	// List trace summaries for distributed tracing
	ListTraces(ctx context.Context, params repo.ListTracesParams) ([]repo.TraceSummary, error)
	// Insert telemetry log
	InsertTelemetryLog(ctx context.Context, params repo.InsertTelemetryLogParams) error
}

// PosthogClient defines the interface for capturing events in PostHog.
type PosthogClient interface {
	CaptureEvent(ctx context.Context, eventName string, distinctID string, eventProperties map[string]interface{}) error
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
	featuresClient *productfeatures.Client,
	organizationID string,
	info ToolInfo,
	toolName string,
	toolType repo.ToolType,
) (ToolCallLogger, error) {
	noop := NewNoopToolCallLogger()
	if provider == nil || toolType == "" {
		return noop, nil
	}

	shouldLog, err := featuresClient.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureLogs)
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

	params := toolReqToTelemetryLog(*l.entry)
	createTelemetryLog(ctx, logger, l.provider, params)
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

// createTelemetryLog logs a telemetry entry using the tool metrics provider.
// Errors are reported through the supplied logger. Logging happens asynchronously to
// avoid blocking the caller and the params struct is copied to prevent data races.
func createTelemetryLog(
	ctx context.Context,
	logger *slog.Logger,
	provider ToolMetricsProvider,
	params repo.InsertTelemetryLogParams,
) {
	if provider == nil || params.ID == "" {
		return
	}

	go func() {
		logCtx := context.WithoutCancel(ctx)

		if err := provider.InsertTelemetryLog(logCtx, params); err != nil {
			logger.ErrorContext(logCtx,
				"failed to insert telemetry log to ClickHouse",
				attr.SlogError(err),
				attr.SlogToolURN(params.GramURN),
			)
			return
		}

		logger.DebugContext(logCtx,
			"logged telemetry entry to ClickHouse",
			attr.SlogToolURN(params.GramURN),
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

// toolReqToTelemetryLog converts a ToolHTTPRequest to InsertTelemetryLogParams
// for inserting into the telemetry_logs table.
func toolReqToTelemetryLog(req repo.ToolHTTPRequest) repo.InsertTelemetryLogParams {
	// Convert timestamp to Unix nanoseconds
	timeUnixNano := req.Ts.UnixNano()

	// Determine severity based on status code
	var severityText *string
	errorText := "ERROR"
	warnText := "WARN"
	infoText := "INFO"
	if req.StatusCode >= 500 {
		severityText = &errorText
	} else if req.StatusCode >= 400 {
		severityText = &warnText
	} else {
		severityText = &infoText
	}

	// Create body message
	body := fmt.Sprintf("%s %s -> %d (%.2fms)", req.HTTPMethod, req.HTTPRoute, req.StatusCode, req.DurationMs)

	// Build attributes struct and marshal to JSON
	type attributes struct {
		HTTPServerURL        string            `json:"http.server.url"`
		HTTPRoute            string            `json:"http.route"`
		HTTPRequestMethod    string            `json:"http.request.method"`
		HTTPRequestBodyBytes int64             `json:"http.request.body.bytes"`
		HTTPRequestHeaders   map[string]string `json:"http.request.headers"`
		HTTPStatusCode       int64             `json:"http.response.status_code"`
		HTTPResponseBytes    int64             `json:"http.response.body.bytes"`
		HTTPResponseHeaders  map[string]string `json:"http.response.headers"`
		HTTPDurationMs       float64           `json:"http.duration_ms"`
		UserAgent            string            `json:"user_agent"`
	}

	attrs := attributes{
		HTTPServerURL:        req.HTTPServerURL,
		HTTPRoute:            req.HTTPRoute,
		HTTPRequestMethod:    req.HTTPMethod,
		HTTPRequestBodyBytes: req.RequestBodyBytes,
		HTTPRequestHeaders:   req.RequestHeaders,
		HTTPStatusCode:       req.StatusCode,
		HTTPResponseBytes:    req.ResponseBodyBytes,
		HTTPResponseHeaders:  req.ResponseHeaders,
		HTTPDurationMs:       req.DurationMs,
		UserAgent:            req.UserAgent,
	}

	attrsB, err := json.Marshal(attrs)
	if err != nil {
		// Fallback to empty JSON object if marshalling fails
		attrsB = []byte("{}")
	}

	// Build resource attributes struct and marshal to JSON
	type resourceAttributes struct {
		ServiceName    string `json:"service.name"`
		ProjectID      string `json:"gram.project.id"`
		DeploymentID   string `json:"gram.deployment.id"`
		ToolID         string `json:"gram.tool.id"`
		ToolURN        string `json:"gram.tool.urn"`
		ToolType       string `json:"gram.tool.type"`
		OrganizationID string `json:"gram.organization.id"`
	}

	resAttrs := resourceAttributes{
		ServiceName:    "gram-server",
		ProjectID:      req.ProjectID,
		DeploymentID:   req.DeploymentID,
		ToolID:         req.ToolID,
		ToolURN:        req.ToolURN,
		ToolType:       string(req.ToolType),
		OrganizationID: req.OrganizationID,
	}

	resAttrsB, err := json.Marshal(resAttrs)
	if err != nil {
		// Fallback to empty JSON object if marshalling fails
		resAttrsB = []byte("{}")
	}

	var traceID, spanID *string
	if req.TraceID != "" {
		traceID = &req.TraceID
	}
	if req.SpanID != "" {
		spanID = &req.SpanID
	}

	var deploymentID *string
	if req.DeploymentID != "" {
		deploymentID = &req.DeploymentID
	}

	// #nosec G115 -- StatusCode is validated to be within HTTP status code range (0-599)
	statusCode := int32(req.StatusCode)

	return repo.InsertTelemetryLogParams{
		ID:                     req.ID,
		TimeUnixNano:           timeUnixNano,
		ObservedTimeUnixNano:   timeUnixNano,
		SeverityText:           severityText,
		Body:                   body,
		TraceID:                traceID,
		SpanID:                 spanID,
		Attributes:             string(attrsB),
		ResourceAttributes:     string(resAttrsB),
		GramProjectID:          req.ProjectID,
		GramDeploymentID:       deploymentID,
		GramFunctionID:         nil, // HTTP logs don't have function IDs
		GramURN:                req.ToolURN,
		ServiceName:            "gram-server",
		ServiceVersion:         nil,
		HTTPRequestMethod:      &req.HTTPMethod,
		HTTPResponseStatusCode: &statusCode,
		HTTPRoute:              &req.HTTPRoute,
		HTTPServerURL:          &req.HTTPServerURL,
	}
}
