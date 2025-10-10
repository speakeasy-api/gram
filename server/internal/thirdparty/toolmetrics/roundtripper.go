package toolmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Log attribute keys for HTTP round trip operations
const (
	attrDurationMs    = "duration_ms"
	attrPanic         = "panic"
	attrRequestBytes  = "request_bytes"
	attrResponseBytes = "response_bytes"
)

// sensitiveHeaders is a list of header names that should be redacted from logs
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"cookie":              true,
	"set-cookie":          true,
	"x-api-key":           true,
	"api-key":             true,
	"apikey":              true,
	"proxy-authorization": true,
	"www-authenticate":    true,
	"x-auth-token":        true,
	"x-csrf-token":        true,
	"x-session-token":     true,
}

// filterSensitiveHeaders removes sensitive headers from the map
func filterSensitiveHeaders(headers map[string]string) map[string]string {
	filtered := make(map[string]string, len(headers))
	for key, value := range headers {
		if sensitiveHeaders[strings.ToLower(key)] {
			filtered[key] = "[REDACTED]"
		} else {
			filtered[key] = value
		}
	}
	return filtered
}

// HTTPLoggingRoundTripper wraps an http.RoundTripper and logs HTTP requests to ClickHouse
type HTTPLoggingRoundTripper struct {
	rt     http.RoundTripper
	tcm    ToolMetricsClient
	logger *slog.Logger
}

// NewHTTPLoggingRoundTripper creates a new RoundTripper that logs HTTP requests to ClickHouse
func NewHTTPLoggingRoundTripper(rt http.RoundTripper, tcm ToolMetricsClient, logger *slog.Logger) *HTTPLoggingRoundTripper {
	return &HTTPLoggingRoundTripper{
		rt:     rt,
		tcm:    tcm,
		logger: logger,
	}
}

// RoundTrip implements http.RoundTripper interface
func (h *HTTPLoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	tracer := otel.Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics")

	// Start a span for the HTTP logging round trip
	ctx, span := tracer.Start(ctx, "tool.http.roundtrip",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.url", req.URL.String()),
			attribute.String("http.host", req.URL.Host),
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

	var clientIP string

	httpTrace := &httptrace.ClientTrace{
		GetConn: nil,
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Conn != nil {
				remote := info.Conn.RemoteAddr().String()
				if host, _, err := net.SplitHostPort(remote); err == nil {
					clientIP = host
					span.SetAttributes(attribute.String("net.peer.ip", clientIP))
				}
			}
		},
		PutIdleConn:          nil,
		GotFirstResponseByte: nil,
		Got100Continue:       nil,
		Got1xxResponse:       nil,
		DNSStart:             nil,
		DNSDone:              nil,
		ConnectStart:         nil,
		ConnectDone:          nil,
		TLSHandshakeStart:    nil,
		TLSHandshakeDone:     nil,
		WroteHeaderField:     nil,
		WroteHeaders:         nil,
		Wait100Continue:      nil,
		WroteRequest:         nil,
	}
	req = req.WithContext(httptrace.WithClientTrace(ctx, httpTrace))

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
		attribute.String("tool.id", tool.ID),
		attribute.String("tool.urn", tool.Urn),
		attribute.String("tool.name", tool.Name),
		attribute.String("tool.project_id", tool.ProjectID),
		attribute.String("tool.deployment_id", tool.DeploymentID),
		attribute.String("tool.organization_id", tool.OrganizationID),
		attribute.String("http.route", tool.HTTPRoute),
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
			slog.Float64(attrDurationMs, durationMs),
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
			attribute.Int("http.status_code", statusCode),
			attribute.Float64("http.duration_ms", durationMs),
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
			slog.Float64(attrDurationMs, durationMs),
			attr.SlogToolURN(tool.Urn),
		)
	}

	// Get request headers and filter sensitive ones
	requestHeaders := make(map[string]string)
	for key, values := range req.Header {
		for _, value := range values {
			requestHeaders[key] = value
		}
	}
	requestHeaders = filterSensitiveHeaders(requestHeaders)

	// Get response headers and filter sensitive ones
	responseHeaders := make(map[string]string)
	if resp != nil {
		for key, values := range resp.Header {
			for _, value := range values {
				responseHeaders[key] = value
			}
		}
		responseHeaders = filterSensitiveHeaders(responseHeaders)
	}

	toolID := tool.ID
	method := req.Method
	url := req.URL.String()

	logHTTPRequest := func(respBodyBytes int) {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					h.logger.ErrorContext(ctx, "panic in HTTP request logging goroutine",
						slog.Any(attrPanic, r),
						attr.SlogURLOriginal(url),
						attr.SlogToolURN(toolID),
						attr.SlogHTTPRequestMethod(method),
					)
				}
			}()

			logCtx := context.WithoutCancel(ctx)

			// Get final request body size now that request body has been read and closed
			var requestBodyBytes = conv.PtrValOr(requestBodyBytesPtr, 0)

			// We are not logging the request or response bodies for a number of reasons:
			// - They may be too large and will inflate our ClickHouse table, making it slower to query and hard to estimate the size
			// - They might contain sensitive information such as PII or API keys, then we'd have to redact them
			httpRequest := ToolHTTPRequest{
				Ts:                time.Now().UTC(),
				OrganizationID:    tool.OrganizationID,
				ProjectID:         tool.ProjectID,
				DeploymentID:      tool.DeploymentID,
				ToolID:            tool.ID,
				ToolURN:           tool.Urn,
				ToolType:          HTTPToolType,
				TraceID:           traceID,
				SpanID:            spanID,
				HTTPMethod:        req.Method,
				HTTPRoute:         tool.HTTPRoute,
				StatusCode:        uint16(statusCode),
				DurationMs:        durationMs,
				UserAgent:         req.UserAgent(),
				ClientIPv4:        clientIP,
				RequestHeaders:    requestHeaders,
				RequestBody:       nil,
				RequestBodySkip:   nil,
				RequestBodyBytes:  int64(requestBodyBytes),
				ResponseHeaders:   responseHeaders,
				ResponseBody:      nil,
				ResponseBodySkip:  nil,
				ResponseBodyBytes: int64(respBodyBytes),
			}

			logErr := h.tcm.Log(logCtx, httpRequest)
			if logErr != nil {
				h.logger.ErrorContext(logCtx, "failed to log HTTP attempt to ClickHouse",
					attr.SlogError(logErr),
					attr.SlogURLOriginal(url),
					attr.SlogToolURN(toolID),
					attr.SlogToolName(tool.ProjectID),
					attr.SlogHTTPRequestMethod(method),
				)
			} else {
				h.logger.DebugContext(logCtx, "successfully logged HTTP request to ClickHouse",
					attr.SlogURLOriginal(url),
					attr.SlogToolURN(toolID),
					attr.SlogHTTPRequestMethod(method),
					slog.Int(attrRequestBytes, requestBodyBytes),
					slog.Int(attrResponseBytes, respBodyBytes),
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
	HTTPRoute      string
}
