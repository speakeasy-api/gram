package mcpmetrics

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewMetrics(t *testing.T) {
	t.Parallel()

	t.Run("creates_metrics_with_valid_meter", func(t *testing.T) {
		t.Parallel()
		meter := testenv.NewMeterProvider(t).Meter("test")
		logger := testenv.NewLogger(t)

		m := NewMetrics(meter, logger)
		require.NotNil(t, m)
		require.NotNil(t, m.mcpToolCallCounter)
		require.NotNil(t, m.mcpRequestDuration)
		require.NotNil(t, m.identityCoverage)
	})
}

func TestMetrics_RecordMCPToolCall(t *testing.T) {
	t.Parallel()

	t.Run("records_tool_call_with_valid_counter", func(t *testing.T) {
		t.Parallel()
		meter := testenv.NewMeterProvider(t).Meter("test")
		logger := testenv.NewLogger(t)
		m := NewMetrics(meter, logger)

		// Should not panic
		m.RecordMCPToolCall(context.Background(), "org-123", "https://mcp.example.com", "test-tool")
	})

	t.Run("handles_nil_counter_gracefully", func(t *testing.T) {
		t.Parallel()
		m := &Metrics{
			mcpToolCallCounter: nil,
		}

		// Should not panic when counter is nil
		m.RecordMCPToolCall(context.Background(), "org-123", "https://mcp.example.com", "test-tool")
	})
}

func TestMetrics_RecordMCPRequestDuration(t *testing.T) {
	t.Parallel()

	t.Run("records_duration_with_valid_histogram", func(t *testing.T) {
		t.Parallel()
		meter := testenv.NewMeterProvider(t).Meter("test")
		logger := testenv.NewLogger(t)
		m := NewMetrics(meter, logger)

		// Should not panic
		m.RecordMCPRequestDuration(context.Background(), "tools/call", "https://mcp.example.com", 100*time.Millisecond)
	})

	t.Run("handles_nil_histogram_gracefully", func(t *testing.T) {
		t.Parallel()
		m := &Metrics{
			mcpRequestDuration: nil,
		}

		// Should not panic when histogram is nil
		m.RecordMCPRequestDuration(context.Background(), "tools/call", "https://mcp.example.com", 100*time.Millisecond)
	})
}

func TestNewMetrics_CreatesOAuthFlowCounters(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	m := NewMetrics(meter, testenv.NewLogger(t))
	require.NotNil(t, m)
	require.NotNil(t, m.oauthFlowStartedCounter)
	require.NotNil(t, m.oauthFlowCompletedCounter)
	require.NotNil(t, m.oauthFlowFailedCounter)
	require.NotNil(t, m.oauthFlowDeclinedCounter)
	require.NotNil(t, m.oauthRefreshTokenReplayServedCounter)
}

func TestMetrics_RecordOAuthFlowStarted(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	m := NewMetrics(meter, testenv.NewLogger(t))

	// Should not panic with a valid counter.
	m.RecordOAuthFlowStarted(t.Context(), "issuer-1", "mcp-slug-1")
}

func TestMetrics_RecordOAuthFlowCompleted(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	m := NewMetrics(meter, testenv.NewLogger(t))

	m.RecordOAuthFlowCompleted(t.Context(), "issuer-1", "mcp-slug-1")
}

func TestMetrics_RecordOAuthFlowFailed(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	m := NewMetrics(meter, testenv.NewLogger(t))

	m.RecordOAuthFlowFailed(t.Context(), "issuer-1", "mcp-slug-1", OAuthFlowStageToken)
}

func TestMetrics_RecordOAuthFlowDeclined(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	m := NewMetrics(meter, testenv.NewLogger(t))

	m.RecordOAuthFlowDeclined(t.Context(), "issuer-1", "mcp-slug-1", OAuthFlowStageConsent)
}

func TestMetrics_RecordOAuthRefreshTokenReplayServed(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	m := NewMetrics(meter, testenv.NewLogger(t))

	m.RecordOAuthRefreshTokenReplayServed(t.Context(), "issuer-1", "mcp-slug-1")
}

func TestMetrics_RecordOAuthFlow_NilCountersDoNotPanic(t *testing.T) {
	t.Parallel()

	m := &Metrics{}

	// All four must be nil-safe (counter construction can fail at startup).
	m.RecordOAuthFlowStarted(t.Context(), "issuer-1", "mcp-slug-1")
	m.RecordOAuthFlowCompleted(t.Context(), "issuer-1", "mcp-slug-1")
	m.RecordOAuthFlowFailed(t.Context(), "issuer-1", "mcp-slug-1", OAuthFlowStageConsent)
	m.RecordOAuthFlowDeclined(t.Context(), "issuer-1", "mcp-slug-1", OAuthFlowStageIDPCallback)
	m.RecordOAuthRefreshTokenReplayServed(t.Context(), "issuer-1", "mcp-slug-1")
}

// TestRequestCounterRecord_NilSafety pins the documented contract that a nil
// *RequestCounter, and one whose instrument failed to construct, are both
// safe to record against.
func TestRequestCounterRecord_NilSafety(t *testing.T) {
	t.Parallel()

	var c *RequestCounter
	c.Record(t.Context(), mcpversions.Version20260728, "tools/list", SurfaceHosting)
}

func TestRequestCounterRecord_RecordsWithoutError(t *testing.T) {
	t.Parallel()

	c := NewRequestCounter(testenv.NewMeterProvider(t).Meter("test"), testenv.NewLogger(t))
	c.Record(t.Context(), mcpversions.Version20260728, "tools/list", SurfaceHosting)
	c.Record(t.Context(), "", "initialize", SurfacePlatform)
	c.Record(t.Context(), "garbage-version", "tasks/create", SurfaceHosting)
}

// TestMetricsRecordMCPRequest_ForwardsToCensus pins that the service-side
// entry point records through the same census counter the proxy interceptor
// uses, and is nil-receiver safe.
func TestMetricsRecordMCPRequest_ForwardsToCensus(t *testing.T) {
	t.Parallel()

	m := NewMetrics(testenv.NewMeterProvider(t).Meter("test"), testenv.NewLogger(t))
	require.NotNil(t, m.requestCensus)
	m.RecordMCPRequest(t.Context(), mcpversions.Version20260728, "tools/list", SurfaceHosting)

	var nilMetrics *Metrics
	nilMetrics.RecordMCPRequest(t.Context(), mcpversions.Version20260728, "tools/list", SurfaceHosting)
}

// TestRequestCounterRecord_PinsInstrumentAndDimensions pins the census wiring
// a noop meter cannot see: the instrument name, the three attribute keys, and
// that a known version passes through the clamp intact.
func TestRequestCounterRecord_PinsInstrumentAndDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")

	c := NewRequestCounter(meter, testenv.NewLogger(t))
	c.Record(t.Context(), mcpversions.Version20260728, "tools/list", SurfaceHosting)

	metricdatatest.AssertHasAttributes(t, collectMetric(t, reader, InstrumentMCPRequest),
		attr.MCPNegotiatedProtocolVersion(mcpversions.Version20260728),
		attr.McpMethod("tools/list"),
		attr.McpSurface(string(SurfaceHosting)),
		attr.NetworkSurface(NetworkSurfacePublic),
	)
}

// TestRequestCounterRecord_ClampsAtRecordSite pins that the clamps are
// applied inside Record rather than trusted to callers: an unknown method and
// an unknown version must land in their bounded buckets, so a hostile client
// cannot mint unbounded series.
func TestRequestCounterRecord_ClampsAtRecordSite(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")

	c := NewRequestCounter(meter, testenv.NewLogger(t))
	c.Record(t.Context(), "garbage-version", "rpc.discover", SurfacePlatform)

	metricdatatest.AssertHasAttributes(t, collectMetric(t, reader, InstrumentMCPRequest),
		attr.MCPNegotiatedProtocolVersion(mcpversions.Other),
		attr.McpMethod(mcprequests.MethodOther),
		attr.McpSurface(string(SurfacePlatform)),
		attr.NetworkSurface(NetworkSurfacePublic),
	)
}

func TestRequestCounterRecordUsesTrustedPrivateOrigin(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	ctx := requestorigin.WithContext(t.Context(), requestorigin.Origin{
		Surface:          requestorigin.SurfacePrivateNetwork,
		BaseURL:          "https://private.example",
		OrganizationID:   "<ORG_ID>",
		NetworkIngressID: uuid.New(),
	})

	NewRequestCounter(meter, testenv.NewLogger(t)).Record(ctx, mcpversions.Version20260728, "tools/list", SurfaceHosting)

	metricdatatest.AssertHasAttributes(t, collectMetric(t, reader, InstrumentMCPRequest),
		attr.MCPNegotiatedProtocolVersion(mcpversions.Version20260728),
		attr.McpMethod("tools/list"),
		attr.McpSurface(string(SurfaceHosting)),
		attr.NetworkSurface(NetworkSurfacePrivate),
	)
}

// TestRecordMCPRequestDuration_ClampsMethodLabel pins the cardinality fix on
// the pre-existing histogram: the method label is clamped at the record site.
func TestRecordMCPRequestDuration_ClampsMethodLabel(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")

	m := NewMetrics(meter, testenv.NewLogger(t))
	m.RecordMCPRequestDuration(t.Context(), "rpc.discover", "mcp.example.com/mcp/demo", 100*time.Millisecond)

	metricdatatest.AssertHasAttributes(t, collectMetric(t, reader, "mcp.request.duration"),
		attr.McpMethod(mcprequests.MethodOther),
		attr.McpURL("mcp.example.com/mcp/demo"),
	)
}

// TestRecordMCPRequestRejected_PinsInstrumentAndDimensions pins the wiring a
// noop meter cannot see: the instrument name and the three attribute keys the
// dashboard and the AIS-618 monitors query by.
func TestRecordMCPRequestRejected_PinsInstrumentAndDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")

	m := NewMetrics(meter, testenv.NewLogger(t))
	m.RecordMCPRequestRejected(t.Context(), "invalid_remote_session", "mcp.example.com/mcp/demo", SurfaceMeta)

	got := collectMetric(t, reader, InstrumentMCPRequestRejected)
	metricdatatest.AssertHasAttributes(t, got,
		attr.OAuthFailureReason("invalid_remote_session"),
		attr.McpURL("mcp.example.com/mcp/demo"),
		attr.McpSurface(string(SurfaceMeta)),
	)

	sum, ok := got.Data.(metricdata.Sum[int64])
	require.True(t, ok, "rejected instrument must be an int64 counter")
	require.Len(t, sum.DataPoints, 1)
	require.Equal(t, int64(1), sum.DataPoints[0].Value)
}

// TestRecordMCPRequestRejected_NilSafe pins the documented contract that a
// nil *Metrics, and one whose instrument failed to construct, are both safe
// to record against.
func TestRecordMCPRequestRejected_NilSafe(t *testing.T) {
	t.Parallel()

	var nilMetrics *Metrics
	nilMetrics.RecordMCPRequestRejected(t.Context(), "no_credentials", "mcp.example.com/mcp/demo", SurfaceHosting)

	empty := &Metrics{}
	empty.RecordMCPRequestRejected(t.Context(), "no_credentials", "mcp.example.com/mcp/demo", SurfaceHosting)
}

// collectMetric drains the reader and returns the named metric, failing the
// test when it was never recorded.
func collectMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Metrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)

	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}
