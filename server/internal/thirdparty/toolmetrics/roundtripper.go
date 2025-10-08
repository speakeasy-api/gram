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
	"go.opentelemetry.io/otel/trace"
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
	startTime := time.Now()

	base := h.rt
	if base == nil {
		base = http.DefaultTransport
	}

	var clientIP string

	httpTrace := &httptrace.ClientTrace{ //nolint:exhaustruct // only need the capture the server IP
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Conn != nil {
				remote := info.Conn.RemoteAddr().String()
				if host, _, err := net.SplitHostPort(remote); err == nil {
					clientIP = host
				}
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), httpTrace))

	requestBodyBytesPtr, ok := ctx.Value(RequestBodyContextKey).(*uint64)
	if !ok {
		requestBodyBytesPtr = nil
	}

	resp, err := base.RoundTrip(req)

	durationMs := time.Since(startTime).Seconds() * 1000

	// Extract tool information from context
	tool, ok := ctx.Value(ToolInfoContextKey).(*ToolInfo)
	if !ok {
		// If no tool context, we can't log this request
		if err != nil {
			return resp, fmt.Errorf("no tool context: %w", err)
		}
		return resp, fmt.Errorf("no tool context")
	}

	// If the request failed, wrap and return the error; E.g., request timeout, etc.
	if err != nil {
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

	statusCode := uint16(0)
	if resp != nil {
		statusCode = uint16(resp.StatusCode) //nolint:gosec // response codes aren't that large
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

	logHTTPRequest := func(respBodyBytes uint64) {
		go func() {
			logCtx := context.WithoutCancel(ctx)

			// Get final request body size now that request body has been read and closed
			var requestBodyBytes = conv.PtrValOr(requestBodyBytesPtr, uint64(0))

			httpRequest := ToolHTTPRequest{
				Ts:                time.Now().UTC(),
				OrganizationID:    tool.OrganizationID,
				ProjectID:         tool.ProjectID,
				DeploymentID:      tool.DeploymentID,
				ToolID:            tool.ID,
				ToolURN:           tool.Urn,
				ToolType:          ToolTypeHttp,
				TraceID:           traceID,
				SpanID:            spanID,
				HTTPMethod:        req.Method,
				HTTPRoute:         tool.HTTPRoute,
				StatusCode:        statusCode,
				DurationMs:        durationMs,
				UserAgent:         req.UserAgent(),
				ClientIPv4:        clientIP,
				RequestHeaders:    requestHeaders,
				RequestBody:       nil,
				RequestBodySkip:   nil,
				RequestBodyBytes:  requestBodyBytes,
				ResponseHeaders:   responseHeaders,
				ResponseBody:      nil,
				ResponseBodySkip:  nil,
				ResponseBodyBytes: respBodyBytes,
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
