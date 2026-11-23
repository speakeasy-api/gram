package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type tracingRefusalInterceptor struct {
	name    string
	observe func(context.Context, error)
}

func (i tracingRefusalInterceptor) Name() string { return i.name }

func (i tracingRefusalInterceptor) InterceptToolsCallRefusal(ctx context.Context, err error) {
	i.observe(ctx, err)
}

func TestRunToolsCallRefusalInterceptors_Tracing(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	tracer := provider.Tracer("test")
	ctx, parent := tracer.Start(t.Context(), "parent")
	defer parent.End()
	refusal := errors.New("refused")
	var calls []string
	var contexts []trace.SpanContext
	p := &Proxy{Tracer: tracer}
	for _, name := range []string{"first", "second"} {
		p.ToolsCallRefusalInterceptors = append(p.ToolsCallRefusalInterceptors, tracingRefusalInterceptor{
			name: name,
			observe: func(ctx context.Context, err error) {
				require.Same(t, refusal, err)
				span := trace.SpanFromContext(ctx)
				require.True(t, span.IsRecording())
				calls = append(calls, name)
				contexts = append(contexts, span.SpanContext())
			},
		})
	}

	p.runToolsCallRefusalInterceptors(ctx, refusal)

	require.Equal(t, []string{"first", "second"}, calls)
	spans := recorder.Ended()
	require.Len(t, spans, 2)
	for i, span := range spans {
		require.Equal(t, "remotemcp.proxy.ToolsCallRefusalInterceptor", span.Name())
		require.Contains(t, span.Attributes(), attr.RemoteMCPProxyInterceptor(calls[i]))
		require.Equal(t, parent.SpanContext(), span.Parent())
		require.Equal(t, contexts[i], span.SpanContext())
	}
}
