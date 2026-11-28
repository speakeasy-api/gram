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

// ErrUserBlocked marks a login refused because staff blocked the user from
// the platform (users.blacklisted_at).
var ErrUserBlocked = errors.New("user is blocked from the platform")

// ErrDuplicateEmailAlias marks a sign-up refused because the normalized
// Gmail or Googlemail address already belongs to another user.
var ErrDuplicateEmailAlias = errors.New("an account already exists for this email address")
