package mcpriskscan_test

import (
	"bytes"
	"context"
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
	evaluator := mcpriskscan.NewNoop(provider, testenv.NewMeterProvider(t), testenv.NewLogger(t))
	var authCtx contextvalues.AuthContext
	authCtx.UserID = "credential-owner"
	ctx := contextvalues.SetAuthContext(t.Context(), &authCtx)
	event := mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, OrganizationID: "", ProjectID: "", ServerID: "",
		ToolsetID: "", ToolName: "ping", ResourceURI: "", PromptName: "",
		Method: mcpriskscan.MethodToolsCall,
	}
	evaluator.Scan(ctx, mcpriskscan.NewRequest(ctx, event, mcpriskscan.BorrowPayload(nil)))
	apiKeyCtx := mcpidentity.NewValidatorBoundary().StampAPIKey(ctx, "key_test")
	evaluator.Scan(apiKeyCtx, mcpriskscan.NewRequest(apiKeyCtx, event, mcpriskscan.BorrowPayload(nil)))
	assistantCtx := mcpidentity.NewValidatorBoundary().StampAssistant(ctx)
	evaluator.Scan(assistantCtx, mcpriskscan.NewRequest(assistantCtx, event, mcpriskscan.BorrowPayload(nil)))

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
		require.Equal(t, mcpriskscan.PhaseRequest, attrs["gram.mcp.risk.scan.phase"].AsString())
	}
	require.Equal(t, "credential-owner", authCtx.UserID)
}

func TestNoop_DoesNotExportRequestPayload(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	evaluator := mcpriskscan.NewNoop(provider, testenv.NewMeterProvider(t), testenv.NewLogger(t))
	event := mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, OrganizationID: "", ProjectID: "", ServerID: "",
		ToolsetID: "", ToolName: "ping", ResourceURI: "", PromptName: "",
		Method: mcpriskscan.MethodToolsCall,
	}
	evaluator.Scan(t.Context(), mcpriskscan.NewRequest(t.Context(), event, mcpriskscan.BorrowPayload(nil)))
	payload := []byte(`{"private_argument":"sensitive-tool-input"}`)
	original := bytes.Clone(payload)
	evaluator.Scan(t.Context(), mcpriskscan.NewRequest(t.Context(), event, mcpriskscan.BorrowPayload(payload)))
	require.Equal(t, original, payload, "the no-op must not mutate borrowed payload bytes")

	spans := recorder.Ended()
	require.Len(t, spans, 2)
	for _, span := range spans {
		for _, kv := range span.Attributes() {
			require.NotContains(t, kv.Value.Emit(), "sensitive-tool-input")
		}
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
	evaluator := mcpriskscan.NewNoop(tracerProvider, meterProvider, testenv.NewLogger(t))
	wantCounts := make(map[attribute.Set]int64)
	for _, seam := range []struct{ surface, method string }{
		{mcpriskscan.SurfaceHostedMCP, mcpriskscan.MethodToolsCall},
		{mcpriskscan.SurfaceRemoteMCP, mcpriskscan.MethodToolsCall},
		{mcpriskscan.SurfaceHostedMCP, mcpriskscan.MethodResourcesRead},
		{mcpriskscan.SurfaceHostedMCP, mcpriskscan.MethodPromptsGet},
	} {
		attrs := attribute.NewSet(
			attribute.String("gram.mcp.risk.scan.surface", seam.surface),
			attribute.String("gram.mcp.risk.scan.method", seam.method),
			attribute.String("gram.mcp.risk.scan.decision", "allow"),
		)
		wantCounts[attrs] = 2
		ctx := t.Context()
		for _, suffix := range []string{"first", "second"} {
			evaluator.Scan(ctx, mcpriskscan.NewRequest(ctx, mcpriskscan.Event{
				Surface: seam.surface, Method: seam.method,
				OrganizationID: "org-" + suffix, ProjectID: "project-" + suffix,
				ServerID: "server-" + suffix, ToolsetID: "toolset-" + suffix,
				ToolName: "tool-" + suffix, ResourceURI: "resource://" + suffix, PromptName: "prompt-" + suffix,
			}, mcpriskscan.BorrowPayload([]byte(suffix))))
			ctx = mcpidentity.NewValidatorBoundary().StampAPIKey(ctx, "key_test")
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
	require.Equal(t, wantCounts, counts, "phase, identifiers, principal and payload must not create metric series")

	duration, ok := collected["mcp.risk.scan.duration"].Data.(metricdata.Histogram[float64])
	require.True(t, ok, "scan duration must be a seconds histogram")
	durationCounts := make(map[attribute.Set]int64)
	for _, point := range duration.DataPoints {
		durationCounts[point.Attributes] = int64(point.Count)
		require.Greater(t, point.Sum, float64(0), "record elapsed scan time, not a constant zero")
	}
	require.Equal(t, wantCounts, durationCounts, "every scan must record duration with the same bounded dimensions")
}

func TestEvaluator_ClaimsAnOwnedSubjectOnce(t *testing.T) {
	t.Parallel()
	var calls int
	evaluator := mcpriskscan.NewEvaluator(mcpriskscan.ObserverFunc(func(_ context.Context, subject mcpriskscan.Subject) {
		calls++
		require.True(t, subject.EvaluationOwner())
	}))
	subject := mcpriskscan.NewRequest(t.Context(), mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP,
		Method:  mcpriskscan.MethodToolsCall,
	}, mcpriskscan.BorrowPayload([]byte(`{"query":"once"}`)))

	evaluator.Scan(t.Context(), subject)
	evaluator.Scan(t.Context(), subject)

	require.Equal(t, 1, calls)
}

func TestEvaluator_IgnoresNonOwnerSurface(t *testing.T) {
	t.Parallel()
	var calls int
	evaluator := mcpriskscan.NewEvaluator(mcpriskscan.ObserverFunc(func(context.Context, mcpriskscan.Subject) {
		calls++
	}))
	subject := mcpriskscan.NewRequest(t.Context(), mcpriskscan.Event{
		Surface: "meta_mcp",
		Method:  mcpriskscan.MethodToolsCall,
	}, mcpriskscan.BorrowPayload([]byte(`{"query":"delegated"}`)))

	require.False(t, subject.EvaluationOwner())
	evaluator.Scan(t.Context(), subject)
	require.Zero(t, calls, "meta routes must delegate evaluation to the concrete member seam")
}

func TestNewResponse_ReusesExecutionAndTrustedPrincipal(t *testing.T) {
	t.Parallel()
	ctx := mcpidentity.NewValidatorBoundary().StampAPIKey(t.Context(), "key_test")
	request := mcpriskscan.NewRequest(ctx, mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceRemoteMCP,
		Method:  mcpriskscan.MethodToolsCall,
	}, mcpriskscan.BorrowPayload([]byte(`{"query":"request"}`)))
	response := mcpriskscan.NewResponse(request, mcpriskscan.BorrowPayload([]byte(`{"result":"terminal"}`)))

	require.Equal(t, request.Event.ExecutionID(), response.Event.ExecutionID())
	require.Equal(t, mcpriskscan.PhaseResponse, response.Event.Phase())
	require.Equal(t, mcpidentity.KindAPIKey, response.Event.Principal().Kind())
	require.True(t, response.Event.IdentityStamped())
	require.Equal(t, mcpriskscan.PayloadAvailable, response.Payload.Availability())
	require.JSONEq(t, `{"result":"terminal"}`, string(response.Payload.Bytes()))
	require.True(t, response.EvaluationOwner())

	var calls int
	evaluator := mcpriskscan.NewEvaluator(mcpriskscan.ObserverFunc(func(context.Context, mcpriskscan.Subject) {
		calls++
	}))
	evaluator.Scan(ctx, response)
	evaluator.Scan(ctx, mcpriskscan.NewResponse(request, mcpriskscan.BorrowPayload([]byte(`{"result":"duplicate terminal"}`))))
	require.Equal(t, 1, calls, "repeated terminal events must share the response-phase claim")
}

func TestBorrowPayload_IsCompleteOrUnavailable(t *testing.T) {
	t.Parallel()
	atLimit := bytes.Repeat([]byte{'x'}, mcpriskscan.MaxPayloadBytes)
	available := mcpriskscan.BorrowPayload(atLimit)
	require.Equal(t, mcpriskscan.PayloadAvailable, available.Availability())
	require.Len(t, available.Bytes(), mcpriskscan.MaxPayloadBytes)

	oversized := mcpriskscan.BorrowPayload(append(atLimit, 'x'))
	require.Equal(t, mcpriskscan.PayloadOversized, oversized.Availability())
	require.Nil(t, oversized.Bytes(), "oversized content must not be truncated")
}
