package proxy

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
)

const (
	MeterRequests        = "gram.remote_mcp.proxy.requests"
	MeterRequestDuration = "gram.remote_mcp.proxy.request.duration"
	MeterResponseBytes   = "gram.remote_mcp.proxy.response.bytes"
	MeterStreams         = "gram.remote_mcp.proxy.streams"
	MeterStreamDuration  = "gram.remote_mcp.proxy.stream.duration"
)

// StreamEndReason names why a relayed SSE stream ended.
type StreamEndReason string

const (
	// StreamEndUpstreamClosed means the remote MCP server ended the stream.
	StreamEndUpstreamClosed StreamEndReason = "upstream_closed"

	// StreamEndIdleTimeout means the remote MCP server sent nothing for the
	// proxy's streaming idle bound.
	StreamEndIdleTimeout StreamEndReason = "idle_timeout"

	// StreamEndRequestCanceled means the inbound request's context ended:
	// the client went away, or the server cancelled in-flight requests
	// after its shutdown deadline.
	StreamEndRequestCanceled StreamEndReason = "request_canceled"

	// StreamEndShutdown means the proxy closed the stream because the
	// server started shutting down.
	StreamEndShutdown StreamEndReason = "shutdown"

	// StreamEndError means relaying failed for any other reason, such as a
	// write to the client failing or an oversized event.
	StreamEndError StreamEndReason = "error"
)

// streamDurationBuckets spans the life of a relayed stream: POST responses
// usually finish in seconds to minutes, while listen streams that receive
// keepalives stay open for hours.
var streamDurationBuckets = []float64{1, 10, 30, 60, 120, 300, 600, 1800, 3600, 7200, 14400, 28800}

// StreamEnd describes one relayed SSE stream when it ends.
type StreamEnd struct {
	// HTTPMethod is the inbound request's method: GET for a standalone stream,
	// POST for a response or subscriptions/listen stream.
	HTTPMethod string

	// JSONRPCMethod is the method of the request that opened a POST stream.
	// It is client-supplied and is clamped before it becomes a dimension.
	JSONRPCMethod string

	// Reason is why the stream ended.
	Reason StreamEndReason

	// ResponseRelayed reports whether the JSON-RPC response to the request
	// that opened a POST stream had been relayed when the stream ended.
	ResponseRelayed bool

	// Resumable reports whether the upstream sent SSE event ids.
	Resumable bool

	// ResumeRequested reports whether the client sent Last-Event-ID.
	ResumeRequested bool

	// Duration is how long the stream was relayed.
	Duration time.Duration
}

// Metrics owns the counters and histograms recorded for each proxied request.
// A nil *Metrics is valid — Record becomes a no-op — so tests and callers
// that do not care about metrics can pass nil.
type Metrics struct {
	requests        metric.Int64Counter
	requestDuration metric.Float64Histogram
	responseBytes   metric.Int64Histogram
	streams         metric.Int64Counter
	streamDuration  metric.Float64Histogram
}

// NewMetrics constructs the counter and histograms served by the proxy. Errors
// from the meter are logged and individual instruments are left nil; Record
// handles nil instruments so partial construction still produces usable
// metrics.
func NewMetrics(meter metric.Meter, logger *slog.Logger) *Metrics {
	ctx := context.Background()

	requests, err := meter.Int64Counter(
		MeterRequests,
		metric.WithDescription("Number of requests forwarded to a remote MCP server"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(MeterRequests), attr.SlogError(err))
	}

	requestDuration, err := meter.Float64Histogram(
		MeterRequestDuration,
		metric.WithDescription("Duration of a request forwarded to a remote MCP server, in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(MeterRequestDuration), attr.SlogError(err))
	}

	responseBytes, err := meter.Int64Histogram(
		MeterResponseBytes,
		metric.WithDescription("Bytes relayed from a remote MCP server back to the client"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(
			1024,
			10*1024,
			100*1024,
			1024*1024,
			10*1024*1024,
			50*1024*1024,
		),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(MeterResponseBytes), attr.SlogError(err))
	}

	streams, err := meter.Int64Counter(
		MeterStreams,
		metric.WithDescription("Number of SSE streams relayed from a remote MCP server, counted when each ends"),
		metric.WithUnit("{stream}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(MeterStreams), attr.SlogError(err))
	}

	streamDuration, err := meter.Float64Histogram(
		MeterStreamDuration,
		metric.WithDescription("How long an SSE stream from a remote MCP server was relayed, in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(streamDurationBuckets...),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(MeterStreamDuration), attr.SlogError(err))
	}

	return &Metrics{
		requests:        requests,
		requestDuration: requestDuration,
		responseBytes:   responseBytes,
		streams:         streams,
		streamDuration:  streamDuration,
	}
}

// Record emits one sample per instrument for the proxied request. upstreamStatus
// is 0 when the upstream HTTP call never produced a status (timeout, DNS,
// blocked IP, etc.); the status class label collapses these into "error" so
// dashboards can alert on them without per-error-kind cardinality.
//
// Convention: the status_class label reflects the upstream's HTTP status,
// not the user-facing outcome. When a response-side interceptor rejects an
// upstream 200 (e.g. the upstream returned data but we substituted a
// JSON-RPC error envelope for the user), this metric still reports "2xx"
// because the metric measures upstream health. User-facing outcomes are
// available via span status (set to error on rejection) and the
// gram.remote_mcp.proxy.requests counter dimensioned by error class.
func (m *Metrics) Record(ctx context.Context, identity ServerIdentity, method string, upstreamStatus int, responseBytes int64, duration time.Duration) {
	if m == nil {
		return
	}

	labels := []attribute.KeyValue{
		attr.HTTPRequestMethod(method),
		attr.RemoteMCPProxyRemoteStatusClass(statusClass(upstreamStatus)),
	}
	labels = identity.AppendAttributes(labels)

	attrsOpt := metric.WithAttributes(labels...)

	if m.requests != nil {
		m.requests.Add(ctx, 1, attrsOpt)
	}
	if m.requestDuration != nil {
		m.requestDuration.Record(ctx, duration.Seconds(), attrsOpt)
	}
	if m.responseBytes != nil {
		m.responseBytes.Record(ctx, responseBytes, attrsOpt)
	}
}

// RecordStreamEnd emits one sample per instrument for a relayed SSE stream
// that ended. The duration histogram omits the server ids the counter
// carries, so its hour-scale buckets are not multiplied per server.
func (m *Metrics) RecordStreamEnd(ctx context.Context, identity ServerIdentity, end StreamEnd) {
	if m == nil {
		return
	}

	labels := []attribute.KeyValue{
		attr.HTTPRequestMethod(end.HTTPMethod),
		attr.RemoteMCPProxyStreamEndReason(string(end.Reason)),
	}
	if m.streamDuration != nil {
		m.streamDuration.Record(ctx, end.Duration.Seconds(), metric.WithAttributes(labels...))
	}

	if m.streams != nil {
		labels = append(labels,
			attr.McpMethod(mcprequests.ClampMethod(end.JSONRPCMethod)),
			attr.RemoteMCPProxyStreamResponseRelayed(end.ResponseRelayed),
			attr.RemoteMCPProxyStreamResumable(end.Resumable),
			attr.RemoteMCPProxyStreamResumeRequested(end.ResumeRequested),
		)
		m.streams.Add(ctx, 1, metric.WithAttributes(identity.AppendAttributes(labels)...))
	}
}

// statusClass buckets an upstream HTTP status code into a low-cardinality
// label. Zero — meaning no response was received — maps to "error" so
// operationally-interesting error cases share a label with upstream errors
// that never produced a status.
func statusClass(code int) string {
	switch {
	case code == 0:
		return "error"
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}
