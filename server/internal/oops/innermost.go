package oops

import "errors"

// Innermost is the innermost ShareableError err wraps, err's own when it wraps
// no other, or nil when err carries none. Every ShareableError's public
// message is written to be client-safe, and the innermost one names the
// actionable cause.
func Innermost(err error) *ShareableError {
	var innermost *ShareableError
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		if shareable, ok := errors.AsType[*ShareableError](cur); ok {
			innermost = shareable
		}
	}
	return innermost
}
