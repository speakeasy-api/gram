package o11y_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

var _ net.Error = timeoutError{}

func TestOutcomeFromErrorWithTimeout(t *testing.T) {
	t.Parallel()

	require.Equal(t, o11y.OutcomeSuccess, o11y.OutcomeFromErrorWithTimeout(nil))
	require.Equal(t, o11y.OutcomeCanceled, o11y.OutcomeFromErrorWithTimeout(context.Canceled))
	require.Equal(t, o11y.OutcomeCanceled, o11y.OutcomeFromErrorWithTimeout(fmt.Errorf("request: %w", context.Canceled)))
	require.Equal(t, o11y.OutcomeTimeout, o11y.OutcomeFromErrorWithTimeout(context.DeadlineExceeded))
	require.Equal(t, o11y.OutcomeTimeout, o11y.OutcomeFromErrorWithTimeout(fmt.Errorf("request: %w", context.DeadlineExceeded)))
	require.Equal(t, o11y.OutcomeTimeout, o11y.OutcomeFromErrorWithTimeout(fmt.Errorf("dial: %w", timeoutError{})))
	require.Equal(t, o11y.OutcomeFailure, o11y.OutcomeFromErrorWithTimeout(errors.New("socket hang up")))
}

func TestOutcomeFromErrorCollapsesTimeout(t *testing.T) {
	t.Parallel()

	// OutcomeFromError does not special-case timeouts: they stay OutcomeFailure
	// for callers that do not impose their own deadline.
	if got := o11y.OutcomeFromError(context.DeadlineExceeded); got != o11y.OutcomeFailure {
		t.Errorf("OutcomeFromError(DeadlineExceeded) = %q, want %q", got, o11y.OutcomeFailure)
	}
}
