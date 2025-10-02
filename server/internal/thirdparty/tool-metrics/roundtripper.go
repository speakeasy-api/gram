package tool_metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"go.opentelemetry.io/otel/trace"
)

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

	// Execute the request
	resp, err := base.RoundTrip(req)

	// Calculate duration
	durationMs := time.Since(startTime).Seconds() * 1000

	// Extract tool information from context
	tool, ok := ctx.Value(ToolInfoContextKey).(*ToolInfo)
	if !ok {
		// If no tool context, we can't log this request
		return resp, fmt.Errorf("no tool found: %w", err)
	}

	// Read the request body from the context
	reqBodyBytes, ok := ctx.Value(RequestBodyContextKey).([]byte)
	if !ok {
		h.logger.ErrorContext(ctx, "failed to read request body from context")
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

	// Determine status code
	statusCode := uint16(0)
	if resp != nil {
		statusCode = uint16(resp.StatusCode) //nolint:gosec // response codes aren't that large
	} else if err != nil { // request timeout, etc.
		statusCode = uint16(oops.StatusCodes[oops.CodeGatewayError]) //nolint:gosec // response codes aren't that large
	}

	// Check if this is a streaming response (SSE)
	isStreaming := false
	var respBodyBuffer *bytes.Buffer
	var respBodyBytes []byte

	if resp != nil && resp.Body != nil {
		contentType := resp.Header.Get("Content-Type")
		isStreaming = strings.HasPrefix(contentType, "text/event-stream")

		if isStreaming {
			resp.Body = &capturingReadCloser{
				ReadCloser: resp.Body,
				buffer:     respBodyBuffer,
				onClose: func() {
					h.logger.DebugContext(ctx, "finished reading streaming response body")
				},
			}
		} else {
			// For non-streaming responses, read the entire body
			respBodyBytes, err = io.ReadAll(resp.Body)
			if err != nil {
				h.logger.ErrorContext(ctx, "failed to read response body", attr.SlogError(err))
			}

			resp.Body = io.NopCloser(bytes.NewReader(respBodyBytes))
		}
	}

	// Get request headers
	requestHeaders := make(map[string]string)
	for key, values := range req.Header {
		for _, value := range values {
			requestHeaders[key] = value
		}
	}

	// Get response headers
	responseHeaders := make(map[string]string)
	if resp != nil {
		for key, values := range resp.Header {
			for _, value := range values {
				responseHeaders[key] = value
			}
		}
	}

	var requestBodySkip *string = nil // where will this come from?

	// Prepare response body data for logging
	var responseBody *string
	var responseBodySkip *string
	var responseBodyBytes uint64

	if isStreaming {
		// For streaming responses, mark as truncated
		responseBody = conv.Ptr(respBodyBuffer.String())
		responseBodySkip = nil
		responseBodyBytes = uint64(len(respBodyBuffer.Bytes()))
	} else {
		// For non-streaming responses, log the full body
		responseBody = conv.Ptr(string(respBodyBytes))
		responseBodySkip = nil
		responseBodyBytes = uint64(len(respBodyBytes))
	}

	// Log this individual HTTP attempt to ClickHouse asynchronously
	httpRequest := ToolHTTPRequest{
		Ts:                time.Now(),
		OrganizationID:    tool.OrganizationID,
		ProjectID:         tool.ProjectID,
		DeploymentID:      tool.DeploymentID,
		ToolID:            tool.ID,
		ToolURN:           tool.Urn,
		ToolType:          ToolTypeHttp,
		TraceID:           traceID,
		SpanID:            spanID,
		HTTPMethod:        req.Method,
		HTTPRoute:         req.URL.Path,
		StatusCode:        statusCode,
		DurationMs:        durationMs,
		UserAgent:         req.UserAgent(),
		ClientIPv4:        clientIP,
		RequestHeaders:    requestHeaders,
		RequestBody:       conv.Ptr(string(reqBodyBytes)),
		RequestBodySkip:   requestBodySkip,
		RequestBodyBytes:  uint64(len(reqBodyBytes)),
		ResponseHeaders:   responseHeaders,
		ResponseBody:      responseBody,
		ResponseBodySkip:  responseBodySkip,
		ResponseBodyBytes: responseBodyBytes,
	}

	toolID := tool.ID
	method := req.Method
	url := req.URL.String()

	// Log asynchronously to avoid blocking the response
	go func() {
		// Create a detached context to prevent cancellation when request completes
		logCtx := context.WithoutCancel(ctx)

		logErr := h.tcm.Log(logCtx, httpRequest)
		if logErr != nil {
			h.logger.ErrorContext(logCtx, "failed to log HTTP attempt to ClickHouse",
				attr.SlogError(logErr),
				attr.SlogToolID(toolID),
				attr.SlogHTTPRequestMethod(method),
				attr.SlogURLOriginal(url),
			)
		} else if isStreaming {
			h.logger.DebugContext(logCtx, "logged streaming HTTP response to ClickHouse",
				attr.SlogToolID(toolID),
				attr.SlogHTTPRequestMethod(method),
				attr.SlogURLOriginal(url),
			)
		}
	}()

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
