package toolmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
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

// HTTPLoggingRoundTripper wraps an http.RoundTripper and logs HTTP requests to ClickHouse
type HTTPLoggingRoundTripper struct {
	rt     http.RoundTripper
	tcm    ToolMetricsProvider
	logger *slog.Logger
	tracer trace.Tracer
}

// NewHTTPLoggingRoundTripper creates a new RoundTripper that logs HTTP requests to ClickHouse
func NewHTTPLoggingRoundTripper(rt http.RoundTripper, tcm ToolMetricsProvider, logger *slog.Logger, tracer trace.Tracer) *HTTPLoggingRoundTripper {
	// t := otel.Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics")
	return &HTTPLoggingRoundTripper{
		rt:     rt,
		tcm:    tcm,
		logger: logger,
		tracer: tracer,
	}
}

// RoundTrip implements http.RoundTripper interface
func (h *HTTPLoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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

	resp, err := base.RoundTrip(req)

	durationMs := time.Since(startTime).Seconds() * 1000

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

	// If the request failed, wrap and return the error; E.g., request timeout, etc.
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "HTTP request failed")
		h.logger.ErrorContext(ctx, "HTTP roundtrip failed",
			attr.SlogError(err),
			attr.SlogURLOriginal(req.URL.String()),
			attr.SlogHTTPRequestMethod(req.Method),
			attr.SlogToolURN(tool.Urn),
			attr.SlogHTTPRequestDuration(durationMs),
		)
		return resp, fmt.Errorf("roundtrip: %w", err)
	}

	// Extract trace information
	spanCtx := trace.SpanContextFromContext(ctx)
	var traceID, spanID string
	if spanCtx.HasTraceID() {
		traceID = spanCtx.TraceID().String()
	}
	if spanCtx.HasSpanID() {
		spanID = spanCtx.SpanID().String()
	}

	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
		span.SetAttributes(
			attr.HTTPResponseStatusCode(statusCode),
			attr.HTTPRequestDurationMs(durationMs),
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
			attr.SlogHTTPRequestDuration(durationMs),
			attr.SlogToolURN(tool.Urn),
		)
	}

	// Get request headers and keep only allowed ones
	requestHeaders := make(map[string]string)
	for key, values := range req.Header {
		for _, value := range values {
			requestHeaders[key] = value
		}
	}
	requestHeaders = filterAllowedHeaders(requestHeaders)

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

	method := req.Method
	url := req.URL.String()

	logHTTPRequest := func(respBodyBytes int) {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					h.logger.ErrorContext(ctx, "panic in HTTP request logging goroutine",
						attr.SlogErrorMessage(fmt.Sprintf("%v", r)),
						attr.SlogURLOriginal(url),
						attr.SlogToolURN(tool.Urn),
						attr.SlogHTTPRequestMethod(method),
					)
				}
			}()

			logCtx := context.WithoutCancel(ctx)

			// Get final request body size now that request body has been read and closed
			var requestBodyBytes = conv.PtrValOr(requestBodyBytesPtr, 0)

			id, err := uuid.NewV7()
			if err != nil {
				h.logger.ErrorContext(logCtx, "failed to generate UUID for HTTP request logging",
					attr.SlogURLOriginal(url),
					attr.SlogToolURN(tool.Urn),
					attr.SlogHTTPRequestMethod(method),
				)
				return
			}

			// We are not logging the request or response bodies for a number of reasons:
			// - They may be too large and will inflate our ClickHouse table, making it slower to query and hard to estimate the size
			// - They might contain sensitive information such as PII or API keys, then we'd have to redact them
			httpRequest := ToolHTTPRequest{
				ID:                id.String(),
				Ts:                time.Unix(id.Time().UnixTime()),
				OrganizationID:    tool.OrganizationID,
				ProjectID:         tool.ProjectID,
				DeploymentID:      tool.DeploymentID,
				ToolID:            tool.ID,
				ToolURN:           tool.Urn,
				ToolType:          HTTPToolType,
				TraceID:           traceID,
				SpanID:            spanID,
				HTTPMethod:        req.Method,
				HTTPRoute:         req.URL.Path,
				StatusCode:        int64(statusCode),
				DurationMs:        durationMs,
				UserAgent:         req.UserAgent(),
				RequestHeaders:    requestHeaders,
				RequestBodyBytes:  int64(requestBodyBytes),
				ResponseHeaders:   responseHeaders,
				ResponseBodyBytes: int64(respBodyBytes),
			}

			logErr := h.tcm.Log(logCtx, httpRequest)
			if logErr != nil {
				h.logger.ErrorContext(logCtx, "failed to log HTTP attempt to ClickHouse",
					attr.SlogError(logErr),
					attr.SlogURLOriginal(url),
					attr.SlogToolURN(tool.Urn),
					attr.SlogToolName(tool.Name),
					attr.SlogHTTPRequestMethod(method),
				)
			} else {
				h.logger.DebugContext(logCtx, "successfully logged HTTP request to ClickHouse",
					attr.SlogURLOriginal(url),
					attr.SlogToolURN(tool.Urn),
					attr.SlogHTTPRequestMethod(method),
					attr.SlogHTTPRequestBodyBytes(requestBodyBytes),
					attr.SlogHTTPResponseBodyBytes(respBodyBytes),
				)
			}
		}()
	}

	if resp == nil || resp.Body == nil {
		// No response body (e.g., 204 No Content), log immediately
		logHTTPRequest(0)
		return resp, nil
	}

	// Wraps the response body to count bytes and log when the body is closed
	resp.Body = NewCountingReadCloser(resp.Body, logHTTPRequest)

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
