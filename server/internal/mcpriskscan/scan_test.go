package mcpriskscan_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNoop_DoesNotPromoteCredentialOwnerToPrincipal(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	observer := mcpriskscan.NewNoop(provider, testenv.NewMeterProvider(t), testenv.NewLogger(t))
	var authCtx contextvalues.AuthContext
	authCtx.UserID = "credential-owner"
	ctx := contextvalues.SetAuthContext(t.Context(), &authCtx)
	event := mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, OrganizationID: "", ProjectID: "", ServerID: "",
		ToolsetID: "", ToolName: "ping", ResourceURI: "", PromptName: "", Phase: mcpriskscan.PhaseBeforeExecution,
		Payload: nil,
	}
	observer.Scan(ctx, event)
	observer.Scan(mcpidentity.NewValidatorBoundary().StampAPIKey(ctx), event)
	observer.Scan(mcpidentity.NewValidatorBoundary().StampAssistant(ctx), event)

	spans := recorder.Ended()
	require.Len(t, spans, 3)
	for i, wantKind := range []string{"", "api_key", "assistant"} {
		require.Equal(t, "mcp.risk.scan", spans[i].Name())
		attrs := make(map[attribute.Key]attribute.Value)
		for _, kv := range spans[i].Attributes() {
			attrs[kv.Key] = kv.Value
		}
		require.Empty(t, attrs[attr.UserIDKey].AsString())
		require.Equal(t, wantKind, attrs["gram.mcp.risk.scan.principal_kind"].AsString())
		require.Equal(t, i != 0, attrs["gram.mcp.risk.scan.identity_stamped"].AsBool())
		require.Equal(t, "ping", attrs[attr.ToolNameKey].AsString())
		require.Equal(t, mcpriskscan.PhaseBeforeExecution, attrs["gram.mcp.risk.scan.phase"].AsString())
	}
	require.Equal(t, "credential-owner", authCtx.UserID)
}

func TestNoop_DoesNotExportRequestPayload(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	observer := mcpriskscan.NewNoop(provider, testenv.NewMeterProvider(t), testenv.NewLogger(t))
	event := mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, OrganizationID: "", ProjectID: "", ServerID: "",
		ToolsetID: "", ToolName: "ping", ResourceURI: "", PromptName: "", Phase: mcpriskscan.PhaseBeforeExecution,
		Payload: nil,
	}
	observer.Scan(t.Context(), event)
	for _, payload := range []any{
		json.RawMessage(`{"private_argument":"sensitive-tool-input"}`),
		map[string]string{"private_argument": "sensitive-prompt-input"},
	} {
		event.Payload = payload
		observer.Scan(t.Context(), event)
	}

	spans := recorder.Ended()
	require.Len(t, spans, 3)
	for _, span := range spans[1:] {
		// Even a serialized or transformed payload must not add or alter attributes.
		require.ElementsMatch(t, spans[0].Attributes(), span.Attributes())
	}
}

func TestNoop_MetricsCountUnsampledScansWithBoundedDimensions(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
	})
	observer := mcpriskscan.NewNoop(tracerProvider, meterProvider, testenv.NewLogger(t))
	wantCounts := make(map[attribute.Set]int64)
	for _, seam := range []struct{ surface, phase string }{
		{mcpriskscan.SurfaceHostedMCP, mcpriskscan.PhaseBeforeExecution},
		{mcpriskscan.SurfaceRemoteMCP, mcpriskscan.PhaseBeforeExecution},
		{mcpriskscan.SurfaceResourceRead, mcpriskscan.PhaseBeforeRead},
		{mcpriskscan.SurfacePromptsGet, mcpriskscan.PhaseBeforeRender},
	} {
		attrs := attribute.NewSet(
			attribute.String("gram.mcp.risk.scan.surface", seam.surface),
			attribute.String("gram.mcp.risk.scan.phase", seam.phase),
		)
		wantCounts[attrs] = 2
		ctx := t.Context()
		for _, suffix := range []string{"first", "second"} {
			observer.Scan(ctx, mcpriskscan.Event{
				Surface: seam.surface, Phase: seam.phase,
				OrganizationID: "org-" + suffix, ProjectID: "project-" + suffix,
				ServerID: "server-" + suffix, ToolsetID: "toolset-" + suffix,
				ToolName: "tool-" + suffix, ResourceURI: "resource://" + suffix, PromptName: "prompt-" + suffix,
				Payload: map[string]string{"private_argument": suffix},
			})
			ctx = mcpidentity.NewValidatorBoundary().StampAPIKey(ctx)
		}
	}

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	collected := make(map[string]metricdata.Metrics)
	for _, instrument := range rm.ScopeMetrics[0].Metrics {
		collected[instrument.Name] = instrument
	}
	require.Contains(t, collected, "mcp.risk.scan")
	require.Contains(t, collected, "mcp.risk.scan.duration")
	require.Equal(t, "{scan}", collected["mcp.risk.scan"].Unit)
	require.Equal(t, "s", collected["mcp.risk.scan.duration"].Unit)

	scans, ok := collected["mcp.risk.scan"].Data.(metricdata.Sum[int64])
	require.True(t, ok, "scan volume must be an int64 counter")
	require.True(t, scans.IsMonotonic)
	counts := make(map[attribute.Set]int64)
	for _, point := range scans.DataPoints {
		counts[point.Attributes] = point.Value
	}
	require.Equal(t, wantCounts, counts, "identifiers, principal and payload must not create metric series")

	duration, ok := collected["mcp.risk.scan.duration"].Data.(metricdata.Histogram[float64])
	require.True(t, ok, "scan duration must be a seconds histogram")
	durationCounts := make(map[attribute.Set]int64)
	for _, point := range duration.DataPoints {
		durationCounts[point.Attributes] = int64(point.Count)
		require.Greater(t, point.Sum, float64(0), "record elapsed scan time, not a constant zero")
	}
	require.Equal(t, wantCounts, durationCounts, "every scan must record duration with the same bounded dimensions")
}
