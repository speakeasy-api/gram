package idjag

import (
	"errors"
	"fmt"
)

// Error is a typed rejection. Callers may log Err but must not echo it to an
// OAuth client: it can contain key-set URLs or storage details.
type Error struct {
	// Reason is the public-safe classification of the rejection.
	Reason Reason
	// Err is the underlying internal error.
	Err error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Reason, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

func reject(reason Reason, err error) error { return &Error{Reason: reason, Err: err} }

// ErrNotProvisioned is returned by a subject store when no active directory
// identity maps to an active Gram member in the same organization.
var ErrNotProvisioned = errors.New("enterprise identity is not provisioned")

// ErrNoTrustedIssuer is returned when the user session issuer has no active,
// organization-accessible trusted issuer.
var ErrNoTrustedIssuer = errors.New("user session issuer has no trusted remote issuer")
