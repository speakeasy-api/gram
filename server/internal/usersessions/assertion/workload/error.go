package workload

import (
	"errors"
	"fmt"
)

// Reason is a stable diagnostic label for a rejected workload assertion.
type Reason string

const (
	ReasonAssertionMissing       Reason = "assertion_missing"
	ReasonMalformed              Reason = "assertion_malformed"
	ReasonTypeNotBearer          Reason = "assertion_type_not_bearer"
	ReasonKeyUnknown             Reason = "assertion_key_unknown"
	ReasonKeyUnresolvable        Reason = "assertion_key_unresolvable"
	ReasonSignatureInvalid       Reason = "assertion_signature_invalid"
	ReasonSubjectMismatch        Reason = "assertion_subject_mismatch"
	ReasonAudienceMismatch       Reason = "assertion_audience_mismatch"
	ReasonExpiryMissing          Reason = "assertion_expiry_missing"
	ReasonExpired                Reason = "assertion_expired"
	ReasonNotYetValid            Reason = "assertion_not_yet_valid"
	ReasonLifetimeTooLong        Reason = "assertion_lifetime_too_long"
	ReasonReplayed               Reason = "assertion_replayed"
	ReasonReplayStoreUnavailable Reason = "assertion_replay_store_unavailable"
	ReasonVerifierMisconfigured  Reason = "assertion_verifier_misconfigured"
)

// Error is a typed rejection. Its cause is for diagnostics, never an OAuth
// response to the presenting party.
type Error struct {
	Reason Reason
	Err    error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Reason, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

func reject(reason Reason, format string, args ...any) error {
	return &Error{Reason: reason, Err: fmt.Errorf(format, args...)}
}

func rejectWith(reason Reason, err error) error {
	return &Error{Reason: reason, Err: err}
}

// ReasonOf returns the rejection reason, or an empty value for another error.
func ReasonOf(err error) Reason {
	if typed, ok := errors.AsType[*Error](err); ok {
		return typed.Reason
	}
	return ""
}
