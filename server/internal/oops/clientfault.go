package oops

import "errors"

// ClientFaulter is implemented by error types that can attribute themselves to
// the caller rather than to Gram or an upstream service: arguments naming a
// resource that does not exist, a payload an upstream rejected as malformed, or
// a credential whose scopes were never granted. Such an outcome is the expected
// answer to the request that was made, not a fault to page on.
//
// Error boundaries consult it through IsClientFault to pick a severity: a
// caller fault is answered with a 4xx and logged at warn, keeping error-level
// logs and errored spans — the signals error-rate monitors and SLOs key on —
// for failures Gram or an upstream is responsible for. High-volume caller
// mistakes would otherwise mask genuine regressions in the same component.
type ClientFaulter interface {
	// ClientFault reports whether this particular error value is the caller's
	// to fix. Implementations that carry an upstream status or error code
	// typically answer per code, so one type can cover both attributions.
	ClientFault() bool
}

// IsClientFault reports whether err, or any error it wraps, attributes itself
// to the caller. Errors that do not implement ClientFaulter are treated as
// server faults, so an unclassified failure keeps full error severity.
func IsClientFault(err error) bool {
	if err == nil {
		return false
	}

	// Preserve standard errors.As behavior, including custom As(any) hooks. Do
	// not return false when the first match is not a client fault: a later node
	// or sibling branch can still contain one.
	var faulter ClientFaulter
	if errors.As(err, &faulter) && faulter.ClientFault() {
		return true
	}

	if wrapped := errors.Unwrap(err); wrapped != nil {
		return IsClientFault(wrapped)
	}

	// errors.Unwrap only follows a single child. Inspect the []error shape
	// directly for multi-child error trees such as errors.Join.
	if wrapped, ok := any(err).(interface{ Unwrap() []error }); ok {
		for _, child := range wrapped.Unwrap() {
			if IsClientFault(child) {
				return true
			}
		}
	}

	return false
}
