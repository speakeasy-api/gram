package tool_metrics

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
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

	// Execute the request
	resp, err := h.rt.RoundTrip(req)

	// Calculate duration
	durationMs := time.Since(startTime).Seconds() * 1000

	// Extract tool information from context
	tool, ok := ctx.Value("tool").(*ToolInfo)
	if !ok {
		// If no tool context, just pass through
		return resp, err
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
		statusCode = uint16(resp.StatusCode)
	} else if err != nil {
		statusCode = uint16(oops.StatusCodes[oops.CodeGatewayError])
	}

	// Read the request body for logging
	var reqBodyBytes []byte
	if req.Body != nil {
		reqBodyBytes, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(reqBodyBytes))
	}

	// Read the response body for logging
	var respBodyBytes []byte
	var respBodyBuffer bytes.Buffer
	if resp != nil && resp.Body != nil {
		// Use TeeReader to capture the response body while still allowing it to be read
		teeReader := io.TeeReader(resp.Body, &respBodyBuffer)
		respBodyBytes, _ = io.ReadAll(teeReader)
		resp.Body = io.NopCloser(bytes.NewReader(respBodyBytes))
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

	// Extract client IP from request
	clientIP := req.RemoteAddr
	if clientIP == "" {
		clientIP = "0.0.0.0"
	}

	// Log this individual HTTP attempt to ClickHouse
	logErr := h.tcm.Log(ctx, ToolHTTPRequest{
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
		RequestBodySkip:   nil,
		RequestBodyBytes:  uint64(len(reqBodyBytes)),
		ResponseHeaders:   responseHeaders,
		ResponseBody:      conv.Ptr(respBodyBuffer.String()),
		ResponseBodySkip:  nil,
		ResponseBodyBytes: uint64(len(respBodyBytes)),
	})

	if logErr != nil {
		h.logger.ErrorContext(ctx, "failed to log HTTP attempt to ClickHouse",
			attr.SlogError(logErr),
			attr.SlogToolID(tool.ID),
			attr.SlogHTTPRequestMethod(req.Method),
			attr.SlogURLOriginal(req.URL.String()),
		)
	}

	return resp, err
}

const ToolInfoContextKey = "tool_info_context_key"

// ToolInfo represents the minimal tool information needed for logging
type ToolInfo struct {
	ID             string
	Urn            string
	Name           string
	ProjectID      string
	DeploymentID   string
	OrganizationID string
}
