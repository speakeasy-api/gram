package telemetry

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	redactRevealPrefixLen = 3
	redactMinTokenLen     = 10
)

// allowedNonSensitiveHeaders is a list of standard HTTP header names that are safe to log.
// We use an allowlist approach since we're a proxy and don't want to accidentally
// log custom headers from upstream services that might contain PII or sensitive data.
// sensitive headers added by the security proxy are redacted for logging at a different level
var allowedNonSensitiveHeaders = map[string]bool{
	// Content negotiation
	"accept-encoding":  true,
	"accept-language":  true,
	"content-type":     true,
	"content-length":   true,
	"content-encoding": true,

	// Caching
	"cache-control": true,
	"etag":          true,
	"last-modified": true,
	"age":           true,
	"expires":       true,
	"pragma":        true,
	"vary":          true,

	// Connection and transfer
	"user-agent": true,
	"referer":    true,

	// Content location
	"location":         true,
	"content-location": true,

	// Range requests
	"range":         true,
	"accept-ranges": true,
	"content-range": true,

	// Others
	"server":      true,
	"allow":       true,
	"retry-after": true,

	// Gram specific headers
	"x-gram-proxy": true,
}

// filterAllowedHeaders keeps only headers from the allowlist and filters out unknown headers.
// This protects against logging custom headers that might contain PII or sensitive data.
func filterAllowedHeaders(headers map[string]string) map[string]string {
	filtered := make(map[string]string)
	for key, value := range headers {
		if allowedNonSensitiveHeaders[strings.ToLower(key)] {
			filtered[key] = value
		}
	}
	return filtered
}

// ToolCallLogRoundTripper wraps an http.RoundTripper and logs HTTP requests to ClickHouse
type ToolCallLogRoundTripper struct {
	AttrRecorder HTTPLogAttributes
	rt           http.RoundTripper
	logger       *slog.Logger
	tracer       trace.Tracer
	toolInfo     ToolInfo
}

// NewToolCallLogRoundTripper creates a new RoundTripper that logs HTTP requests to ClickHouse
func NewToolCallLogRoundTripper(
	rt http.RoundTripper,
	logger *slog.Logger,
	tracer trace.Tracer,
	toolInfo ToolInfo,
	recorder HTTPLogAttributes) *ToolCallLogRoundTripper {

	return &ToolCallLogRoundTripper{
		AttrRecorder: recorder,

		rt:       rt,
		logger:   logger,
		tracer:   tracer,
		toolInfo: toolInfo,
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

	// Capture request headers up front so we have them even if the round trip fails.
	requestHeaders := make(map[string]string)
	for key, values := range req.Header {
		for _, value := range values {
			requestHeaders[key] = value
		}
	}
	requestHeaders = filterAllowedHeaders(requestHeaders)
	// Construct full server URL with scheme
	serverURL := req.URL.Scheme + "://" + req.URL.Host
	h.AttrRecorder.RecordServerURL(serverURL, repo.ToolTypeHTTP)
	h.AttrRecorder.RecordMethod(req.Method)
	h.AttrRecorder.RecordRoute(req.URL.Path)
	h.AttrRecorder.RecordUserAgent(req.UserAgent())
	h.AttrRecorder.RecordRequestHeaders(requestHeaders, false)

	resp, err := base.RoundTrip(req)
	// record duration before handling err
	duration := time.Since(startTime).Seconds()
	h.AttrRecorder.RecordDuration(duration)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "HTTP request failed")
		h.logger.ErrorContext(ctx, "HTTP roundtrip failed",
			attr.SlogError(err),
			attr.SlogURLOriginal(req.URL.String()),
			attr.SlogHTTPRequestMethod(req.Method),
			attr.SlogToolURN(h.toolInfo.URN),
			attr.SlogHTTPClientRequestDuration(duration),
		)

		h.AttrRecorder.RecordStatusCode(0)

		return nil, fmt.Errorf("roundtrip: %w", err)
	}

	logBody := fmt.Sprintf("%s %s -> %d (%.2fs)",
		req.Method, req.URL.Path, resp.StatusCode, duration)

	h.AttrRecorder.RecordMessageBody(logBody)

	// Add tool attributes to span
	span.SetAttributes(
		attr.ToolID(h.toolInfo.ID),
		attr.ToolURN(h.toolInfo.URN),
		attr.ToolName(h.toolInfo.Name),
		attr.ProjectID(h.toolInfo.ProjectID),
		attr.DeploymentID(h.toolInfo.DeploymentID),
		attr.OrganizationID(h.toolInfo.OrganizationID),
		attr.HTTPRoute(req.URL.Path),
	)

	span.SetAttributes(
		attr.HTTPResponseStatusCode(resp.StatusCode),
		attr.HTTPClientRequestDuration(duration),
	)

	// Set span status based on HTTP status code
	if resp.StatusCode >= 500 {
		span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", resp.StatusCode))
	} else {
		span.SetStatus(codes.Ok, "")
	}

	// Get response headers and keep only allowed ones
	responseHeaders := make(map[string]string)
	for key, values := range resp.Header {
		for _, value := range values {
			responseHeaders[key] = value
		}
	}
	responseHeaders = filterAllowedHeaders(responseHeaders)

	h.AttrRecorder.RecordStatusCode(resp.StatusCode)
	h.AttrRecorder.RecordResponseHeaders(responseHeaders)

	return resp, nil
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
