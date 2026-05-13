package interceptors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	sdklog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type capturedLog struct {
	level   string
	message string
}

type captureLogger struct {
	calls []capturedLog
}

var _ sdklog.Logger = (*captureLogger)(nil)

func (l *captureLogger) Debug(msg string, _ ...any) {
	l.calls = append(l.calls, capturedLog{level: "debug", message: msg})
}

func (l *captureLogger) Info(msg string, _ ...any) {
	l.calls = append(l.calls, capturedLog{level: "info", message: msg})
}

func (l *captureLogger) Warn(msg string, _ ...any) {
	l.calls = append(l.calls, capturedLog{level: "warn", message: msg})
}

func (l *captureLogger) Error(msg string, _ ...any) {
	l.calls = append(l.calls, capturedLog{level: "error", message: msg})
}

func TestLogWorkflowResult_Success(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logWorkflowResult(logger, nil)

	require.Equal(t, []capturedLog{{level: "info", message: "workflow finished"}}, logger.calls)
}

func TestLogWorkflowResult_GenericError(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logWorkflowResult(logger, errors.New("boom"))

	require.Equal(t, []capturedLog{{level: "error", message: "workflow failed"}}, logger.calls)
}

func TestLogWorkflowResult_ContinueAsNew(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logWorkflowResult(logger, &workflow.ContinueAsNewError{})

	require.Equal(t, []capturedLog{{level: "info", message: "workflow continuing as new"}}, logger.calls)
}

func TestLogWorkflowResult_Canceled(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logWorkflowResult(logger, temporal.NewCanceledError())

	require.Equal(t, []capturedLog{{level: "info", message: "workflow canceled"}}, logger.calls)
}

func TestLogActivityResult_Success(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logActivityResult(logger, nil)

	require.Equal(t, []capturedLog{{level: "info", message: "activity finished"}}, logger.calls)
}

func TestLogActivityResult_GenericError(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logActivityResult(logger, errors.New("boom"))

	require.Equal(t, []capturedLog{{level: "error", message: "activity failed"}}, logger.calls)
}

func TestLogActivityResult_Canceled(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logActivityResult(logger, temporal.NewCanceledError())

	require.Equal(t, []capturedLog{{level: "info", message: "activity canceled"}}, logger.calls)
}
