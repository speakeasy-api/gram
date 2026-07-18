package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

// newDeviceTelemetryHandler builds the otelhttp → HookDeviceTelemetry chain
// around a trivial handler. Requests are served directly (no network client)
// so tests can craft arbitrary header bytes the way a raw peer could.
func newDeviceTelemetryHandler(t *testing.T) (http.Handler, *tracetest.InMemoryExporter) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	t.Cleanup(func() { require.NoError(t, tracerProvider.Shutdown(context.Background())) })

	inner := middleware.HookDeviceTelemetry(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	return otelhttp.NewHandler(inner, "http", otelhttp.WithTracerProvider(tracerProvider)), exporter
}

func spanAttrs(t *testing.T, exporter *tracetest.InMemoryExporter) map[attribute.Key]attribute.Value {
	t.Helper()
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	out := make(map[attribute.Key]attribute.Value, len(spans[0].Attributes))
	for _, kv := range spans[0].Attributes {
		out[kv.Key] = kv.Value
	}
	return out
}

func TestHookDeviceTelemetryStampsSpanAttributes(t *testing.T) {
	t.Parallel()

	handler, exporter := newDeviceTelemetryHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/rpc/hooks.ingest", nil)
	req.Header.Set("X-Gram-Device-Os", "darwin")
	req.Header.Set("X-Gram-Device-Arch", "arm64")
	req.Header.Set("X-Gram-Device-Binary-Version", "1.2.3")
	req.Header.Set("X-Gram-Device-Harness", "claude")
	req.Header.Set("X-Gram-Device-Harness-Variant", "cli")
	req.Header.Set("X-Gram-Device-Harness-Version", "2.0.1")
	req.Header.Set("X-Gram-Device-Elapsed-Ms", "42")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	attrs := spanAttrs(t, exporter)
	require.Equal(t, "darwin", attrs[attr.HookDeviceOSKey].AsString())
	require.Equal(t, "arm64", attrs[attr.HookDeviceArchKey].AsString())
	require.Equal(t, "1.2.3", attrs[attr.HookDeviceBinaryVersionKey].AsString())
	require.Equal(t, "claude", attrs[attr.HookDeviceHarnessKey].AsString())
	require.Equal(t, "cli", attrs[attr.HookDeviceHarnessVariantKey].AsString())
	require.Equal(t, "2.0.1", attrs[attr.HookDeviceHarnessVersionKey].AsString())
	require.Equal(t, int64(42), attrs[attr.HookDeviceElapsedMsKey].AsInt64())
}

func TestHookDeviceTelemetrySanitizesUntrustedValues(t *testing.T) {
	t.Parallel()

	handler, exporter := newDeviceTelemetryHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/rpc/hooks.ingest", nil)
	// Overlong values are truncated; non-printable-ASCII values are dropped;
	// negative elapsed values are dropped.
	req.Header.Set("X-Gram-Device-Os", strings.Repeat("a", 200))
	req.Header.Set("X-Gram-Device-Arch", "arm\x0164")
	req.Header.Set("X-Gram-Device-Elapsed-Ms", "-5")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	attrs := spanAttrs(t, exporter)
	require.Len(t, attrs[attr.HookDeviceOSKey].AsString(), 64)
	_, found := attrs[attr.HookDeviceArchKey]
	require.False(t, found, "control-character value should be dropped")
	_, found = attrs[attr.HookDeviceElapsedMsKey]
	require.False(t, found, "negative elapsed should be dropped")
}

func TestHookDeviceTelemetryIgnoresNonHookRoutes(t *testing.T) {
	t.Parallel()

	handler, exporter := newDeviceTelemetryHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/rpc/other.method", nil)
	req.Header.Set("X-Gram-Device-Os", "darwin")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	attrs := spanAttrs(t, exporter)
	_, found := attrs[attr.HookDeviceOSKey]
	require.False(t, found, "device attributes must not be stamped on non-hook routes")
}
