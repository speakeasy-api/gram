package proxy

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	MeterRequests        = "gram.remote_mcp.proxy.requests"
	MeterRequestDuration = "gram.remote_mcp.proxy.request.duration"
	MeterResponseBytes   = "gram.remote_mcp.proxy.response.bytes"
)

// Metrics owns the counters and histograms recorded for each proxied request.
// A nil *Metrics is valid — Record becomes a no-op — so tests and callers
// that do not care about metrics can pass nil.
type Metrics struct {
	requests        metric.Int64Counter
	requestDuration metric.Float64Histogram
	responseBytes   metric.Int64Histogram
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

	return &Metrics{
		requests:        requests,
		requestDuration: requestDuration,
		responseBytes:   responseBytes,
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
func (m *Metrics) Record(ctx context.Context, serverID string, method string, upstreamStatus int, responseBytes int64, duration time.Duration) {
	if m == nil {
		return
	}

	labels := []attribute.KeyValue{
		attr.HTTPRequestMethod(method),
		attr.RemoteMCPProxyRemoteStatusClass(statusClass(upstreamStatus)),
	}
	if serverID != "" {
		labels = append(labels, attr.RemoteMCPServerID(serverID))
	}

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
