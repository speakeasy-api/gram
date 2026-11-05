package httpstream_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/httpstream"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{name: "method and path", pattern: "GET /mcp/{mcpSlug}", want: "/mcp/{mcpSlug}"},
		{name: "path only", pattern: "/chat/turnstream", want: "/chat/turnstream"},
		{name: "no pattern", pattern: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", http.NoBody)
			req.Pattern = tc.pattern
			require.Equal(t, tc.want, httpstream.Route(req))
		})
	}
}

func TestRetryDelayStaysWithinJitter(t *testing.T) {
	t.Parallel()

	for range 100 {
		delay := httpstream.RetryDelay()
		require.GreaterOrEqual(t, delay, httpstream.RetryMin)
		require.Less(t, delay, httpstream.RetryMin+httpstream.RetryJitter)
	}
}

func TestRetryFieldIsACompleteEvent(t *testing.T) {
	t.Parallel()

	require.Equal(t, "retry: 1500\n\n", string(httpstream.RetryField(1500*time.Millisecond)))
}

func TestMetricsRecordEnd(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	metrics := httpstream.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/id", http.NoBody)
	req.Pattern = "POST /mcp/{mcpSlug}"
	started := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	metrics.RecordEnd(t.Context(), req, httpstream.EndMaxAge, started, started.Add(55*time.Minute))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)
	recorded := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, httpstream.MeterStreamDuration, recorded.Name)
	metricdatatest.AssertHasAttributes(t, recorded,
		attr.HTTPRoute("/mcp/{mcpSlug}"),
		attr.HTTPRequestMethod(http.MethodPost),
		attr.HTTPStreamEndReason(string(httpstream.EndMaxAge)),
	)

	hist, ok := recorded.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	require.InDelta(t, 3300, hist.DataPoints[0].Sum, 0)
}

func TestNilMetricsRecordEndIsANoOp(t *testing.T) {
	t.Parallel()

	var metrics *httpstream.Metrics
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", http.NoBody)
	require.NotPanics(t, func() { metrics.RecordEnd(t.Context(), req, httpstream.EndCompleted, time.Now(), time.Now()) })
}
