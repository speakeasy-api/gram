package o11y_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// captureLogger returns a logger writing JSON records into buf, so a test can
// assert on the level, message, and attributes a helper emits.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	}))
}

func TestLogDefer_Success_LogsNothing(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := o11y.LogDefer(t.Context(), captureLogger(&buf), "failed to close widget", func() error {
		return nil
	})

	require.NoError(t, err)
	require.Empty(t, buf.String(), "a successful deferred call must not log")
}

func TestLogDefer_Failure_LogsMessageAndError(t *testing.T) {
	t.Parallel()

	cbErr := errors.New("boom")

	var buf bytes.Buffer
	err := o11y.LogDefer(t.Context(), captureLogger(&buf), "failed to close widget", func() error {
		return cbErr
	})

	require.ErrorIs(t, err, cbErr, "LogDefer must return the callback error unchanged")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))

	// The caller-supplied message is what makes the record diagnosable. Before
	// AIM-160 this was the literal string "error" for every callsite, which
	// rendered the health digest's top error type meaningless.
	require.Equal(t, "failed to close widget", record["msg"])
	require.Equal(t, slog.LevelError.String(), record["level"])
	require.Equal(t, "boom", record["error.message"])
}
