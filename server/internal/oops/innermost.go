package oops

import "errors"

// Innermost is the innermost ShareableError err wraps, err's own when it wraps
// no other, or nil when err carries none. Every ShareableError's public
// message is written to be client-safe, and the innermost one names the
// actionable cause. Each step searches the cause with errors.As semantics, so
// a ShareableError under errors.Join or another multi-error is found too; of
// several siblings, the first in depth-first order wins.
//
// The walk stops at a ShareableError it has already seen, and after
// maxInnermostDepth steps, so a cause whose As hook keeps producing
// ShareableErrors cannot make it loop forever.
func Innermost(err error) *ShareableError {
	innermost, ok := errors.AsType[*ShareableError](err)
	if !ok {
		return nil
	}
	seen := map[*ShareableError]bool{innermost: true}
	for range maxInnermostDepth {
		next, ok := errors.AsType[*ShareableError](innermost.Unwrap())
		if !ok || seen[next] {
			return innermost
		}
		seen[next] = true
		innermost = next
	}
	return innermost
}

// maxInnermostDepth bounds Innermost's walk. Real cause chains are a few
// ShareableErrors deep.
const maxInnermostDepth = 64
