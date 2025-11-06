package toolmetrics

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// allowedHeaders is a list of standard HTTP header names that are safe to log.
// We use an allowlist approach since we're a proxy and don't want to accidentally
// log custom headers from upstream services that might contain PII or sensitive data.
var allowedHeaders = map[string]bool{
	// Content negotiation
	"accept":           true,
	"accept-encoding":  true,
	"accept-language":  true,
	"content-type":     true,
	"content-length":   true,
	"content-encoding": true,

	// Caching
	"cache-control":       true,
	"etag":                true,
	"if-match":            true,
	"if-none-match":       true,
	"if-modified-since":   true,
	"if-unmodified-since": true,
	"last-modified":       true,
	"age":                 true,
	"expires":             true,
	"pragma":              true,
	"vary":                true,

	// CORS
	"access-control-allow-origin":      true,
	"access-control-allow-methods":     true,
	"access-control-allow-headers":     true,
	"access-control-expose-headers":    true,
	"access-control-max-age":           true,
	"access-control-allow-credentials": true,
	"origin":                           true,

	// Connection and transfer
	"connection":        true,
	"host":              true,
	"user-agent":        true,
	"referer":           true,
	"te":                true,
	"trailer":           true,
	"transfer-encoding": true,
	"upgrade":           true,

	// Content location
	"location":         true,
	"content-location": true,

	// Range requests
	"range":         true,
	"accept-ranges": true,
	"content-range": true,

	// Others
	"date":        true,
	"server":      true,
	"allow":       true,
	"retry-after": true,
}

// filterAllowedHeaders keeps only headers from the allowlist and filters out unknown headers.
// This protects against logging custom headers that might contain PII or sensitive data.
func filterAllowedHeaders(headers map[string]string) map[string]string {
	filtered := make(map[string]string)
	for key, value := range headers {
		if allowedHeaders[strings.ToLower(key)] {
			filtered[key] = value
		}
	}
	return filtered
}

// ToolCallLogRoundTripper wraps an http.RoundTripper and logs HTTP requests to ClickHouse
type ToolCallLogRoundTripper struct {
	rt       http.RoundTripper
	logger   *slog.Logger
	tracer   trace.Tracer
	logEntry *ToolHTTPRequest
}

// NewToolCallLogRoundTripper creates a new RoundTripper that logs HTTP requests to ClickHouse
func NewToolCallLogRoundTripper(rt http.RoundTripper, logger *slog.Logger, tracer trace.Tracer, logEntry *ToolHTTPRequest) *ToolCallLogRoundTripper {
	return &ToolCallLogRoundTripper{
		rt:       rt,
		logger:   logger,
		tracer:   tracer,
		logEntry: logEntry,
	}
}

// RoundTrip implements http.RoundTripper interface
func (h *ToolCallLogRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	// Start a span for the HTTP logging round trip
	ctx, span := h.tracer.Start(ctx, "tool.http.roundtrip",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attr.HTTPRequestMethod(req.Method),
			attr.URLFull(req.URL.String()),
		),
	)
	defer span.End()

	// Update request context with span
	req = req.WithContext(ctx)

	startTime := time.Now()

	base := h.rt
	if base == nil {
		base = http.DefaultTransport
	}

	requestBodyBytesPtr, ok := ctx.Value(RequestBodyContextKey).(*int)
	if !ok {
		requestBodyBytesPtr = nil
	}

	// Capture request headers up front so we have them even if the round trip fails.
	requestHeaders := make(map[string]string)
	for key, values := range req.Header {
		for _, value := range values {
			requestHeaders[key] = value
		}
	}
	requestHeaders = filterAllowedHeaders(requestHeaders)

	if h.logEntry != nil {
		h.logEntry.HTTPMethod = req.Method
		h.logEntry.HTTPRoute = req.URL.Path
		h.logEntry.UserAgent = req.UserAgent()
		h.logEntry.RequestHeaders = requestHeaders
	}

	resp, err := base.RoundTrip(req)

	duration := time.Since(startTime).Seconds()

	// Extract tool information from context
	tool, ok := ctx.Value(ToolInfoContextKey).(*ToolInfo)
	if !ok {
		// If no tool context, we can't log this request
		noToolCtxErr := fmt.Errorf("no tool context")
		span.RecordError(noToolCtxErr)
		span.SetStatus(codes.Error, "missing tool context")
		h.logger.WarnContext(ctx, "HTTP request missing tool context",
			attr.SlogURLOriginal(req.URL.String()),
			attr.SlogHTTPRequestMethod(req.Method),
		)
		if err != nil {
			return resp, fmt.Errorf("%w: %w", noToolCtxErr, err)
		}
		return resp, noToolCtxErr
	}

	// Add tool attributes to span
	span.SetAttributes(
		attr.ToolID(tool.ID),
		attr.ToolURN(tool.Urn),
		attr.ToolName(tool.Name),
		attr.ProjectID(tool.ProjectID),
		attr.DeploymentID(tool.DeploymentID),
		attr.OrganizationID(tool.OrganizationID),
		attr.HTTPRoute(req.URL.Path),
	)

	if h.logEntry != nil {
		h.logEntry.DurationMs = duration * 1000
		h.logEntry.RequestBodyBytes = int64(conv.PtrValOr(requestBodyBytesPtr, 0))
	}

	// If the request failed, wrap and return the error; E.g., request timeout, etc.
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "HTTP request failed")
		h.logger.ErrorContext(ctx, "HTTP roundtrip failed",
			attr.SlogError(err),
			attr.SlogURLOriginal(req.URL.String()),
			attr.SlogHTTPRequestMethod(req.Method),
			attr.SlogToolURN(tool.Urn),
			attr.SlogHTTPClientRequestDuration(duration),
		)

		if h.logEntry != nil {
			h.logEntry.StatusCode = 0
			h.logEntry.ResponseHeaders = nil
			h.logEntry.ResponseBodyBytes = 0
		}

		return resp, fmt.Errorf("roundtrip: %w", err)
	}

	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
		span.SetAttributes(
			attr.HTTPResponseStatusCode(statusCode),
			attr.HTTPClientRequestDuration(duration),
		)

		// Set span status based on HTTP status code
		if statusCode >= 500 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		h.logger.DebugContext(ctx, "HTTP request completed",
			attr.SlogHTTPRequestMethod(req.Method),
			attr.SlogURLOriginal(req.URL.String()),
			attr.SlogHTTPResponseStatusCode(statusCode),
			attr.SlogHTTPClientRequestDuration(duration),
			attr.SlogToolURN(tool.Urn),
		)
	}

	// Get response headers and keep only allowed ones
	responseHeaders := make(map[string]string)
	if resp != nil {
		for key, values := range resp.Header {
			for _, value := range values {
				responseHeaders[key] = value
			}
		}
		responseHeaders = filterAllowedHeaders(responseHeaders)
	}

	if h.logEntry != nil {
		h.logEntry.StatusCode = int64(statusCode)
		h.logEntry.ResponseHeaders = responseHeaders
	}

	if resp == nil || resp.Body == nil {
		return resp, nil
	}

	if h.logEntry != nil {
		// Wraps the response body to count bytes and update log entry when the body is closed
		resp.Body = NewCountingReadCloser(resp.Body, func(respBodyBytes int) {
			h.logEntry.ResponseBodyBytes = int64(respBodyBytes)
		})
	}

	return resp, nil
}

type contextKey string

const ToolInfoContextKey contextKey = "tool_info_context_key"
const RequestBodyContextKey contextKey = "request_body_context_key"

// ToolInfo represents the minimal tool information needed for logging
type ToolInfo struct {
	ID             string
	Urn            string
	Name           string
	ProjectID      string
	DeploymentID   string
	OrganizationID string
}
