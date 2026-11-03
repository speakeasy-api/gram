package codemode

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRunnerConnectionTraceOmitsTransportErrorDetails(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	ctx, parent := provider.Tracer("test").Start(t.Context(), "request")
	defer parent.End()
	const sensitive = "credential-do-not-log"
	client, err := NewRunnerClient("http://127.0.0.1:1", strings.Repeat("t", 32), &net.Dialer{
		ControlContext: func(context.Context, string, string, syscall.RawConn) error {
			return errors.New(sensitive)
		},
	})
	require.NoError(t, err)
	_, _, err = client.connect(ctx)
	require.ErrorContains(t, err, sensitive)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	require.Equal(t, "codemode.runner.connect", span.Name())
	require.Equal(t, parent.SpanContext().TraceID(), span.SpanContext().TraceID())
	require.Equal(t, parent.SpanContext().SpanID(), span.Parent().SpanID())
	require.Equal(t, codes.Error, span.Status().Code)
	require.Equal(t, "error", span.Status().Description)
	require.Len(t, span.Attributes(), 1)
	require.Equal(t, attr.CodeOutcome("error"), span.Attributes()[0])
	require.Empty(t, span.Events(), "transport errors must not become exception events")
}
