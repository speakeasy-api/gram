package oops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func startRecordedSpan(t *testing.T) (context.Context, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("oops-test").Start(t.Context(), "test-span")
	t.Cleanup(func() { span.End() })
	return ctx, recorder
}

func TestShareableError_Log_LogsAtError(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)

	_ = E(CodeNotFound, nil, "resource not found").Log(ctx, logger)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "ERROR", entries[0].Level)
	require.Equal(t, "resource not found", entries[0].Msg)
	require.NotEmpty(t, entries[0].ErrorID)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Len(t, spans[0].Events(), 1, "Log should record the error as a span event")
}
