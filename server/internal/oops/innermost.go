package oops

import "errors"

// Innermost is the innermost ShareableError err wraps, err's own when it wraps
// no other, or nil when err carries none. Every ShareableError's public
// message is written to be client-safe, and the innermost one names the
// actionable cause. Each step searches the cause with errors.As semantics, so
// a ShareableError under errors.Join or another multi-error is found too; of
// several siblings, the first in depth-first order wins.
func Innermost(err error) *ShareableError {
	innermost, ok := errors.AsType[*ShareableError](err)
	if !ok {
		return nil
	}
	for {
		next, ok := errors.AsType[*ShareableError](innermost.Unwrap())
		if !ok {
			return innermost
		}
		innermost = next
	}
}
