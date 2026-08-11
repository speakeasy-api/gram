package o11y_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

var _ net.Error = timeoutError{}

func TestOutcomeFromErrorWithTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want o11y.Outcome
	}{
		{name: "nil is success", err: nil, want: o11y.OutcomeSuccess},
		{name: "deadline exceeded is timeout", err: context.DeadlineExceeded, want: o11y.OutcomeTimeout},
		{
			name: "wrapped deadline exceeded is timeout",
			err:  fmt.Errorf("openrouter object completion: %w", context.DeadlineExceeded),
			want: o11y.OutcomeTimeout,
		},
		{name: "net timeout is timeout", err: fmt.Errorf("dial: %w", timeoutError{}), want: o11y.OutcomeTimeout},
		{name: "canceled is failure not timeout", err: context.Canceled, want: o11y.OutcomeFailure},
		{name: "generic error is failure", err: errors.New("socket hang up"), want: o11y.OutcomeFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := o11y.OutcomeFromErrorWithTimeout(tt.err); got != tt.want {
				t.Errorf("OutcomeFromErrorWithTimeout(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestOutcomeFromErrorCollapsesTimeout(t *testing.T) {
	t.Parallel()

	// OutcomeFromError does not special-case timeouts: they stay OutcomeFailure
	// for callers that do not impose their own deadline.
	if got := o11y.OutcomeFromError(context.DeadlineExceeded); got != o11y.OutcomeFailure {
		t.Errorf("OutcomeFromError(DeadlineExceeded) = %q, want %q", got, o11y.OutcomeFailure)
	}
}
