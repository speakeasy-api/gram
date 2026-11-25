package identity

import "errors"

// ErrCodeRejected identifies an invalid or expired authorization code.
var ErrCodeRejected = errors.New("identity provider rejected authorization code")

// providerError keeps authentication provider responses out of logs, traces, and browser
// errors. Authentication errors can contain a pending token or echo a code.
// The cause remains available for deliberate typed handling, not display.
type providerError struct {
	operation string
	cause     error
}

func (e *providerError) Error() string {
	return "identity provider " + e.operation + " failed"
}

func (e *providerError) Unwrap() error {
	return e.cause
}
