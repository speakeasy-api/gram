package oops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
)

func TestLogError_LogsPlainErrorAndRecordsSpan(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	err := fmt.Errorf("load widgets: %w", errors.New("database unavailable"))

	got := LogError(ctx, logger, err, slog.Group("attrs", slog.String("operation", "load")))

	require.Same(t, err, got)
	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "ERROR", entries[0].Level)
	require.Equal(t, err.Error(), entries[0].Msg)
	require.Equal(t, err.Error(), entries[0].Error)
	require.Equal(t, "load", entries[0].Attrs["operation"])
	require.Empty(t, entries[0].ErrorID)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Equal(t, err.Error(), spans[0].Status().Description)
	require.Len(t, spans[0].Events(), 1)
}

func TestLogError_LogsShareableErrorDetails(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	err := E(CodeUnexpected, errors.New("database unavailable"), "unable to load widgets")

	got := LogError(ctx, logger, err)

	require.Same(t, err, got)
	require.True(t, err.SpanHandled())
	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "ERROR", entries[0].Level)
	require.Equal(t, "unable to load widgets", entries[0].Msg)
	require.Equal(t, "unable to load widgets: database unavailable", entries[0].Error)
	require.Equal(t, err.id, entries[0].ErrorID)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Equal(t, err.String(), spans[0].Status().Description)
	require.Len(t, spans[0].Events(), 1)
}

func TestLogError_PreservesWrapperContextAroundShareableError(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	shareable := E(CodeUnexpected, errors.New("database unavailable"), "unable to load widgets")
	err := fmt.Errorf("refresh dashboard: %w", shareable)

	got := LogError(ctx, logger, err)

	require.Same(t, err, got)
	require.True(t, shareable.SpanHandled())
	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, err.Error(), entries[0].Msg)
	require.Equal(t, err.Error(), entries[0].Error)
	require.Equal(t, shareable.id, entries[0].ErrorID)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, err.Error(), spans[0].Status().Description)
	require.Len(t, spans[0].Events(), 1)
}

func TestLogError_ClientCancellationLogsAtInfoWithoutRecordingSpan(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	err := fmt.Errorf("read request body: %w", context.Canceled)

	got := LogError(ctx, logger, err)

	require.Same(t, err, got)
	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "INFO", entries[0].Level)
	require.Equal(t, err.Error(), entries[0].Error)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	require.Empty(t, spans[0].Events())
}

func TestLogError_CancellationWithLiveContextRemainsError(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	err := fmt.Errorf("errgroup sibling: %w", context.Canceled)

	got := LogError(ctx, logger, err)

	require.Same(t, err, got)
	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "ERROR", entries[0].Level)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Len(t, spans[0].Events(), 1)
}

func TestLogWarn_LogsPlainErrorWithoutRecordingSpan(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	err := errors.New("invalid credentials")

	got := LogWarn(ctx, logger, err)

	require.Same(t, err, got)
	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "WARN", entries[0].Level)
	require.Equal(t, err.Error(), entries[0].Msg)
	require.Equal(t, err.Error(), entries[0].Error)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	require.Empty(t, spans[0].Events())
}

func TestLogWarn_ShareableClientCancellationLogsAtInfo(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	err := E(CodeUnexpected, fmt.Errorf("read request body: %w", context.Canceled), "request canceled")

	got := LogWarn(ctx, logger, err)

	require.Same(t, err, got)
	require.True(t, err.SpanHandled())
	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "INFO", entries[0].Level)
	require.Equal(t, err.id, entries[0].ErrorID)
	require.Equal(t, err.String(), entries[0].Error)

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	require.Empty(t, spans[0].Events())
}

func TestLogErrorAndLogWarn_NilErrorDoNothing(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()
	ctx, recorder := startRecordedSpan(t)

	require.NoError(t, LogError(ctx, logger, nil))
	require.NoError(t, LogWarn(ctx, logger, nil))
	require.Empty(t, logBuf.String())

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	require.Empty(t, spans[0].Events())
}
