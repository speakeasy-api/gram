package identity

// codeExchangeError keeps provider responses out of logs, traces, and browser
// errors. Authentication errors can contain a pending token or echo a code.
// The cause remains available for deliberate typed handling, not display.
type codeExchangeError struct {
	cause error
}

func (e *codeExchangeError) Error() string {
	return "identity provider code exchange failed"
}

func (e *codeExchangeError) Unwrap() error {
	return e.cause
}
