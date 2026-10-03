package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTimeoutError struct{}

func (stubTimeoutError) Error() string   { return "i/o timeout" }
func (stubTimeoutError) Timeout() bool   { return true }
func (stubTimeoutError) Temporary() bool { return true }

var _ net.Error = stubTimeoutError{}

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want FailureReason
	}{
		{name: "nil", err: nil, want: ReasonNone},
		{
			name: "insufficient credits",
			err:  &HTTPError{StatusCode: http.StatusPaymentRequired, Err: ErrInsufficientCredits},
			want: ReasonInsufficientCredits,
		},
		{
			name: "insufficient credits wrapped",
			err:  fmt.Errorf("openrouter object completion: %w", &HTTPError{StatusCode: http.StatusPaymentRequired, Err: ErrInsufficientCredits}),
			want: ReasonInsufficientCredits,
		},
		{
			name: "rate limited",
			err:  &HTTPError{StatusCode: http.StatusTooManyRequests, Err: ErrRateLimited},
			want: ReasonRateLimited,
		},
		{
			name: "platform key disabled",
			err:  fmt.Errorf("resolve key: %w", ErrPlatformKeyDisabled),
			want: ReasonKeyDisabled,
		},
		{
			name: "content policy",
			err:  &HTTPError{StatusCode: http.StatusForbidden, Err: ErrContentPolicy},
			want: ReasonContentPolicy,
		},
		{
			name: "bad request",
			err:  &HTTPError{StatusCode: http.StatusBadRequest, Err: ErrBadRequest},
			want: ReasonBadRequest,
		},
		{
			name: "history corruption is a bad request",
			err:  &HTTPError{StatusCode: http.StatusUnprocessableEntity, Err: ErrHistoryCorruptionCandidate},
			want: ReasonBadRequest,
		},
		{
			name: "server error",
			err:  &HTTPError{StatusCode: http.StatusInternalServerError, Err: nil},
			want: ReasonUpstreamUnavailable,
		},
		{
			name: "provider overloaded",
			err:  &HTTPError{StatusCode: 529, Err: nil},
			want: ReasonUpstreamUnavailable,
		},
		{
			name: "edge network timeout is an upstream outage, not our deadline",
			err:  &HTTPError{StatusCode: 524, Err: nil},
			want: ReasonUpstreamUnavailable,
		},
		{
			name: "unauthorized",
			err:  &HTTPError{StatusCode: http.StatusUnauthorized, Err: nil},
			want: ReasonUnauthorized,
		},
		{
			name: "unclassified forbidden is a credential problem",
			err:  &HTTPError{StatusCode: http.StatusForbidden, Err: nil},
			want: ReasonUnauthorized,
		},
		{
			name: "upstream request timeout",
			err:  &HTTPError{StatusCode: http.StatusRequestTimeout, Err: nil},
			want: ReasonTimeout,
		},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: ReasonTimeout},
		{
			name: "deadline exceeded wrapped",
			err:  fmt.Errorf("judge call: %w", context.DeadlineExceeded),
			want: ReasonTimeout,
		},
		{name: "network timeout", err: fmt.Errorf("dial: %w", stubTimeoutError{}), want: ReasonTimeout},
		{name: "canceled", err: fmt.Errorf("judge call: %w", context.Canceled), want: ReasonCanceled},
		{name: "unclassified", err: errors.New("socket hang up"), want: ReasonError},
		{
			name: "unclassified 4xx",
			err:  &HTTPError{StatusCode: http.StatusNotFound, Err: nil},
			want: ReasonError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, Classify(tt.err))
		})
	}
}

// A caller that walks away must not be reported as an upstream fault: an
// ordinary deploy cancels in-flight scans and would otherwise look like an
// OpenRouter outage on the availability dashboards.
func TestClassifyPrefersCancellationOverStatus(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: %w", context.Canceled, &HTTPError{StatusCode: http.StatusInternalServerError, Err: nil})
	require.Equal(t, ReasonCanceled, Classify(err))
}

func TestFailureReasonDegraded(t *testing.T) {
	t.Parallel()

	degraded := []FailureReason{
		ReasonInsufficientCredits,
		ReasonRateLimited,
		ReasonUnauthorized,
		ReasonKeyDisabled,
		ReasonUpstreamUnavailable,
		ReasonBadRequest,
		ReasonTimeout,
		ReasonError,
	}
	for _, reason := range degraded {
		assert.Truef(t, reason.Degraded(), "%s should count as degraded analysis", reason)
	}

	notDegraded := []FailureReason{ReasonNone, ReasonCanceled, ReasonContentPolicy}
	for _, reason := range notDegraded {
		assert.Falsef(t, reason.Degraded(), "%s should not count as degraded analysis", reason)
	}
}

func TestIsUpstreamUnavailable(t *testing.T) {
	t.Parallel()

	assert.True(t, IsUpstreamUnavailable(&HTTPError{StatusCode: http.StatusBadGateway, Err: nil}))
	assert.True(t, IsUpstreamUnavailable(fmt.Errorf("call: %w", &HTTPError{StatusCode: 529, Err: nil})))
	assert.False(t, IsUpstreamUnavailable(&HTTPError{StatusCode: http.StatusPaymentRequired, Err: ErrInsufficientCredits}))
	assert.False(t, IsUpstreamUnavailable(errors.New("socket hang up")))
}

func TestIsRateLimited(t *testing.T) {
	t.Parallel()

	assert.True(t, IsRateLimited(&HTTPError{StatusCode: http.StatusTooManyRequests, Err: ErrRateLimited}))
	assert.False(t, IsRateLimited(&HTTPError{StatusCode: http.StatusTooManyRequests, Err: nil}))
}
