package o11y

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func newDropViewMeterProvider(t *testing.T) (*sdkmetric.MeterProvider, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithView(dropUnqueriedInstruments),
	)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	return provider, reader
}

func collectMetricNames(t *testing.T, reader *sdkmetric.ManualReader) []string {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	var names []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names = append(names, m.Name)
		}
	}
	slices.Sort(names)
	return names
}

func TestDropUnqueriedInstruments(t *testing.T) {
	t.Parallel()

	provider, reader := newDropViewMeterProvider(t)
	meter := provider.Meter("test")

	kept := []string{
		"http.client.request.duration",
		"http.server.request.duration",
		"temporal_activity_execution_latency",
		"temporal_workflow_completed",
		"temporal_workflow_endtoend_latency",
	}
	for _, name := range slices.Concat(unqueriedInstruments, kept) {
		histogram, err := meter.Float64Histogram(name)
		require.NoError(t, err)
		histogram.Record(t.Context(), 1)
	}

	require.Equal(t, kept, collectMetricNames(t, reader))
}

func TestDropUnqueriedInstrumentsCoversOTelHTTP(t *testing.T) {
	t.Parallel()

	provider, reader := newDropViewMeterProvider(t)

	server := httptest.NewServer(otelhttp.NewHandler(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}),
		"test",
		otelhttp.WithMeterProvider(provider),
	))
	t.Cleanup(server.Close)

	client := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport, otelhttp.WithMeterProvider(provider))}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader("body"))
	require.NoError(t, err)
	res, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	// An otelhttp upgrade that adds or renames an instrument changes this list.
	// Decide whether the new metric is worth exporting before updating it.
	require.Equal(t, []string{"http.client.request.duration", "http.server.request.duration"}, collectMetricNames(t, reader))
}
