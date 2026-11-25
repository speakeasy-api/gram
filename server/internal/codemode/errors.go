package codemode

import "errors"

var (
	// ErrCapacity reports that the caller, organization or runner pool stayed
	// full for the whole bounded admission wait. No program was submitted, so
	// the request can be retried.
	ErrCapacity = errors.New("code runtime capacity exceeded")

	// ErrRuntimeUnavailable reports that no runner can start, for example
	// while a deploy has not yet installed the sandbox template. No program
	// was submitted.
	ErrRuntimeUnavailable = errors.New("code runtime unavailable")

	// ErrAlreadyActive reports that the same caller, session and request ID
	// is already executing.
	ErrAlreadyActive = errors.New("code execution request already active")

	// ErrCancelledBeforeSubmit reports a cancellation that arrived after
	// admission but before the program reached a runner.
	ErrCancelledBeforeSubmit = errors.New("code execution cancelled before submission")
)
