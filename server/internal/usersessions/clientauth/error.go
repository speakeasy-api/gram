package clientauth

import "fmt"

// Error is an assertion rejection. Every failure carries a Reason for
// telemetry; the message is for logs and never reaches the client, because
// callers answer with a single indistinguishable invalid_client so that
// /token cannot be used to tell a real client_id from an imaginary one.
type Error struct {
	// Reason is the stable label to record for this rejection.
	Reason Reason

	// err is the underlying cause, for log lines. It may name key set URLs
	// or library internals and must never be echoed to the client.
	err error
}

func (e *Error) Error() string {
	if e.err == nil {
		return string(e.Reason)
	}
	return fmt.Sprintf("%s: %s", e.Reason, e.err)
}

func (e *Error) Unwrap() error { return e.err }

// reject builds an Error with a formatted cause.
func reject(reason Reason, format string, args ...any) *Error {
	return &Error{Reason: reason, err: fmt.Errorf(format, args...)}
}

// rejectWith builds an Error wrapping an existing cause, keeping it
// reachable through errors.Is and errors.As.
func rejectWith(reason Reason, err error) *Error {
	return &Error{Reason: reason, err: err}
}
