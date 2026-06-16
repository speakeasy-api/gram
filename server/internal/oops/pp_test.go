package oops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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

	_ = E(CodeNotFound, nil, "resource not found").LogError(ctx, logger)

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

func TestShareableError_Log_ClientCanceledLogsAtInfo(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	// A client disconnect cancels the request context itself.
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	// Wrap context.Canceled to prove detection is by sentinel, not identity.
	cause := fmt.Errorf("list tool variations: %w", context.Canceled)
	_ = E(CodeUnexpected, cause, "failed to list tool variations").LogError(ctx, logger)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "INFO", entries[0].Level, "client cancellation must not log at error level")
	require.Equal(t, "failed to list tool variations", entries[0].Msg)
	require.NotEmpty(t, entries[0].ErrorID)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code, "client cancellation must not mark the span as errored")
	require.Empty(t, spans[0].Events(), "client cancellation must not record an exception event")
}

func TestShareableError_Log_AppCanceledWithLiveContextLogsAtError(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	// The request context is still live: the context.Canceled cause came from a
	// derived context an errgroup or the handler itself canceled, not from the
	// client disconnecting. This must keep full error severity.
	ctx, recorder := startRecordedSpan(t)

	cause := fmt.Errorf("errgroup sibling: %w", context.Canceled)
	_ = E(CodeUnexpected, cause, "failed to fan out").LogError(ctx, logger)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "ERROR", entries[0].Level, "an application-initiated cancellation must stay at error level")

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Len(t, spans[0].Events(), 1)
}

func TestShareableError_Log_DeadlineExceededLogsAtError(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)

	cause := fmt.Errorf("query row: %w", context.DeadlineExceeded)
	_ = E(CodeUnexpected, cause, "query failed").LogError(ctx, logger)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "ERROR", entries[0].Level, "deadline exceeded must remain at error level")

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Len(t, spans[0].Events(), 1)
}

func TestShareableError_LogWarn_LogsAtWarn(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)

	_ = E(CodeUnauthorized, errors.New("bad signature"), "unauthorized").LogWarn(ctx, logger)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "WARN", entries[0].Level)
	require.Equal(t, "unauthorized", entries[0].Msg)
	require.NotEmpty(t, entries[0].ErrorID)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code, "LogWarn must not mark the span as errored")
	require.Empty(t, spans[0].Events(), "LogWarn must not record an exception event")
}

func TestShareableError_LogWarn_ClientCanceledLogsAtInfo(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	// A client disconnect cancels the request context itself.
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	cause := fmt.Errorf("authorize token: %w", context.Canceled)
	_ = E(CodeUnauthorized, cause, "unauthorized").LogWarn(ctx, logger)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "INFO", entries[0].Level, "client cancellation must demote LogWarn to info")

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code, "LogWarn must not mark the span as errored")
	require.Empty(t, spans[0].Events())
}

func TestShareableError_LogWarn_AppCanceledWithLiveContextLogsAtWarn(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	// The request context is still live: a context.Canceled cause that did not
	// originate from a client disconnect must not be demoted.
	ctx, recorder := startRecordedSpan(t)

	cause := fmt.Errorf("errgroup sibling: %w", context.Canceled)
	_ = E(CodeUnauthorized, cause, "unauthorized").LogWarn(ctx, logger)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "WARN", entries[0].Level, "a live request context must keep LogWarn at warn level")

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code, "LogWarn must never mark the span as errored")
	require.Empty(t, spans[0].Events())
}

func TestShareableError_LogInfo_LogsAtInfo(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)

	_ = E(CodeNotFound, nil, "resource not found").LogInfo(ctx, logger)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "INFO", entries[0].Level)
	require.Equal(t, "resource not found", entries[0].Msg)
	require.NotEmpty(t, entries[0].ErrorID)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code, "LogInfo must not mark the span as errored")
	require.Empty(t, spans[0].Events(), "LogInfo must not record an exception event")
}

func TestShareableError_LogInfo_ClientCanceledLogsAtInfo(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	cause := fmt.Errorf("authorize token: %w", context.Canceled)
	_ = E(CodeUnauthorized, cause, "unauthorized").LogInfo(ctx, logger)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "INFO", entries[0].Level, "LogInfo stays at info under client cancellation")

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code, "LogInfo must not mark the span as errored")
	require.Empty(t, spans[0].Events())
}

func TestShareableError_HTTPStatus_Canceled(t *testing.T) {
	t.Parallel()

	canceledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	liveCtx := t.Context()

	canceled := E(CodeUnexpected, fmt.Errorf("query row: %w", context.Canceled), "boom")
	require.Equal(t, 499, canceled.HTTPStatus(canceledCtx), "client disconnect maps to 499")
	require.Equal(t, http.StatusInternalServerError, canceled.HTTPStatus(liveCtx), "a canceled cause with a live request context stays a 500")

	deadline := E(CodeUnexpected, fmt.Errorf("query row: %w", context.DeadlineExceeded), "boom")
	require.Equal(t, http.StatusInternalServerError, deadline.HTTPStatus(canceledCtx))

	plain := E(CodeUnexpected, errors.New("boom"), "boom")
	require.Equal(t, http.StatusInternalServerError, plain.HTTPStatus(canceledCtx))
}

func TestShareableError_AsGoa_ClientCanceledIsNotFault(t *testing.T) {
	t.Parallel()

	canceledCtx, cancel := context.WithCancel(t.Context())
	cancel()

	canceled := E(CodeUnexpected, fmt.Errorf("query row: %w", context.Canceled), "boom").AsGoa(canceledCtx)
	require.False(t, canceled.Fault, "client cancellation must not be a server fault")
	require.False(t, canceled.Temporary, "client cancellation must not be retryable")
	require.Equal(t, string(CodeCanceled), canceled.Name)

	plain := E(CodeUnexpected, errors.New("boom"), "boom").AsGoa(canceledCtx)
	require.True(t, plain.Fault, "a genuine unexpected error must remain a server fault")
}

func TestShareableError_AsGoa_AppCanceledWithLiveContextIsFault(t *testing.T) {
	t.Parallel()

	// context.Canceled cause but the request context is still live: an
	// application-initiated cancellation that must keep its 500 fault behavior.
	appCanceled := E(CodeUnexpected, fmt.Errorf("errgroup sibling: %w", context.Canceled), "boom").AsGoa(t.Context())
	require.True(t, appCanceled.Fault, "application-initiated cancellation must remain a server fault")
	require.True(t, appCanceled.Temporary, "an unexpected error stays retryable")
	require.Equal(t, string(CodeUnexpected), appCanceled.Name)
}
