package oops

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubFaultError struct {
	client bool
}

func (e *stubFaultError) Error() string { return "stub fault" }

func (e *stubFaultError) ClientFault() bool { return e.client }

type wrappingStubFaultError struct {
	client bool
	cause  error
}

func (e *wrappingStubFaultError) Error() string { return "wrapping stub fault" }

func (e *wrappingStubFaultError) Unwrap() error { return e.cause }

func (e *wrappingStubFaultError) ClientFault() bool { return e.client }

type asStubFaultError struct {
	faulter ClientFaulter
}

func (e *asStubFaultError) Error() string { return "as stub fault" }

func (e *asStubFaultError) As(target any) bool {
	faulter, ok := target.(*ClientFaulter)
	if !ok {
		return false
	}

	*faulter = e.faulter
	return true
}

func TestIsClientFault_MatchesSelfAttributingError(t *testing.T) {
	t.Parallel()

	require.True(t, IsClientFault(&stubFaultError{client: true}))
	require.False(t, IsClientFault(&stubFaultError{client: false}))
}

func TestIsClientFault_TraversesWrappedErrors(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("execute platform tool: %w", fmt.Errorf("call upstream: %w", &stubFaultError{client: true}))
	require.True(t, IsClientFault(wrapped))
}

func TestIsClientFault_TraversesPastNonClientFaulter(t *testing.T) {
	t.Parallel()

	wrapped := &wrappingStubFaultError{
		client: false,
		cause:  &stubFaultError{client: true},
	}
	require.True(t, IsClientFault(wrapped), "an outer faulter must not hide a caller fault deeper in the chain")
}

func TestIsClientFault_TraversesJoinedErrors(t *testing.T) {
	t.Parallel()

	joined := errors.Join(
		&stubFaultError{client: false},
		fmt.Errorf("call upstream: %w", &stubFaultError{client: true}),
	)
	require.True(t, IsClientFault(joined))
}

func TestIsClientFault_HonorsCustomAs(t *testing.T) {
	t.Parallel()

	err := &asStubFaultError{faulter: &stubFaultError{client: true}}
	require.True(t, IsClientFault(err))
}

func TestIsClientFault_TreatsUnclassifiedErrorsAsServerFaults(t *testing.T) {
	t.Parallel()

	require.False(t, IsClientFault(nil))
	require.False(t, IsClientFault(errors.New("connection reset by peer")))
}

func TestIsClientFault_MatchesShareableErrorCause(t *testing.T) {
	t.Parallel()

	err := E(CodeBadRequest, &stubFaultError{client: true}, "tool call was rejected")
	require.True(t, IsClientFault(err), "a ShareableError must expose its cause's attribution")
}
