package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

func TestRouteLabelerMiddlewareAddsHTTPRoute(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	mux := goahttp.NewMuxer()
	mux.Use(func(h http.Handler) http.Handler {
		return otelhttp.NewHandler(h, "http",
			otelhttp.WithMeterProvider(meterProvider),
		)
	})
	mux.Use(middleware.RouteLabelerMiddleware)

	mux.Handle("GET", "/api/v1/projects/{projectID}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects/abc123")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)

	var duration metricdata.Metrics
	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name == "http.server.request.duration" {
			duration = m
			break
		}
	}
	require.NotEmpty(t, duration.Name, "http.server.request.duration metric not found")

	metricdatatest.AssertHasAttributes(t, duration,
		attr.HTTPRoute("/api/v1/projects/{projectID}"),
	)
}

func TestRouteLabelerMiddlewareNoRouteOnUnmatchedRequest(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	mux := goahttp.NewMuxer()
	mux.Use(func(h http.Handler) http.Handler {
		return otelhttp.NewHandler(h, "http",
			otelhttp.WithMeterProvider(meterProvider),
		)
	})
	mux.Use(middleware.RouteLabelerMiddleware)

	mux.Handle("GET", "/api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/does-not-exist")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)

	var duration metricdata.Metrics
	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name == "http.server.request.duration" {
			duration = m
			break
		}
	}
	require.NotEmpty(t, duration.Name, "http.server.request.duration metric not found")

	hist, ok := duration.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)

	attrs := hist.DataPoints[0].Attributes
	_, found := attrs.Value(attr.HTTPRouteKey)
	require.False(t, found, "http.route should not be present for unmatched routes")
}
