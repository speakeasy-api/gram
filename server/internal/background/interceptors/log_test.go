package interceptors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
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

func TestLogWorkflowResult_Timeout(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logWorkflowResult(logger, temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_SCHEDULE_TO_START, nil))

	require.Equal(t, []capturedLog{{level: "info", message: "workflow timed out"}}, logger.calls)
}

func TestLogWorkflowResult_BenignApplicationError(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logWorkflowResult(logger, temporal.NewApplicationErrorWithOptions(
		"DNS record not found",
		"CustomDomainDNSNotFound",
		temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Category:     temporal.ApplicationErrorCategoryBenign,
		},
	))

	require.Equal(t, []capturedLog{{level: "info", message: "workflow finished with expected error"}}, logger.calls)
}

func TestLogWorkflowResult_RegularApplicationError(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logWorkflowResult(logger, temporal.NewApplicationError("infra exploded", "db"))

	require.Equal(t, []capturedLog{{level: "error", message: "workflow failed"}}, logger.calls)
}

func TestLogWorkflowResult_WrappedCanceled(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logWorkflowResult(logger, fmt.Errorf("verify custom domain: %w", temporal.NewCanceledError()))

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

func TestLogActivityResult_ContextCanceled(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logActivityResult(logger, context.Canceled)

	require.Equal(t, []capturedLog{{level: "info", message: "activity canceled"}}, logger.calls)
}

func TestLogActivityResult_DeadlineExceeded(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logActivityResult(logger, context.DeadlineExceeded)

	require.Equal(t, []capturedLog{{level: "info", message: "activity timed out"}}, logger.calls)
}

func TestLogActivityResult_BenignApplicationError(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	logActivityResult(logger, temporal.NewApplicationErrorWithOptions(
		"cursor rejected the configured api key",
		"AIUsagePollFailed",
		temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Category:     temporal.ApplicationErrorCategoryBenign,
		},
	))

	require.Equal(t, []capturedLog{{level: "info", message: "activity finished with expected error"}}, logger.calls)
}
