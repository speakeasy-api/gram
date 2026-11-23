package httpstream

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// MeterStreamDuration is the histogram of how long long-lived HTTP responses
// stayed open, by route, method and end reason.
const MeterStreamDuration = "gram.http.stream.duration"

// durationBuckets spans the life of a stream, in seconds. The boundaries
// cluster around DefaultMaxAge (3300) and the load balancer's 3600s deadline,
// so the histogram tells a stream the server rotated from one the load
// balancer cut.
var durationBuckets = []float64{1, 10, 30, 60, 120, 300, 600, 1200, 1800, 2400, 3000, 3300, 3500, 3600, 3700, 7200}

// Metrics records how long-lived HTTP responses end. A nil *Metrics is valid:
// RecordEnd becomes a no-op, so handlers built without metrics need no
// checks.
type Metrics struct {
	duration metric.Float64Histogram
}

// NewMetrics constructs the stream duration histogram on meter. An error from
// the meter is logged and leaves the instrument nil, which RecordEnd
// tolerates.
func NewMetrics(meter metric.Meter, logger *slog.Logger) *Metrics {
	duration, err := meter.Float64Histogram(
		MeterStreamDuration,
		metric.WithDescription("How long a long-lived HTTP response stayed open, in seconds, by how it ended"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(MeterStreamDuration), attr.SlogError(err))
	}

	return &Metrics{duration: duration}
}

// RecordEnd records one stream that ended for reason. started is when the
// handler received the request, not when the stream began: the load
// balancer's deadline runs from receipt, so that is the age that matters.
func (m *Metrics) RecordEnd(ctx context.Context, r *http.Request, reason EndReason, started time.Time, now time.Time) {
	if m == nil || m.duration == nil {
		return
	}

	m.duration.Record(ctx, now.Sub(started).Seconds(), metric.WithAttributes(
		attr.HTTPRoute(Route(r)),
		attr.HTTPRequestMethod(r.Method),
		attr.HTTPStreamEndReason(string(reason)),
	))
}
