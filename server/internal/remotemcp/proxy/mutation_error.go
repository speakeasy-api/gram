package proxy

import "fmt"

// MutationError signals that a typed-view setter (e.g.
// [ToolsCallRequest.SetArguments], [ToolsListResponse.SetTools]) failed to
// commit a mutation — most commonly because re-marshaling the typed
// payload back to wire bytes returned an error. Setters wrap their cause
// in this type so callers, and the proxy itself, can distinguish an
// internal mutation failure from an interceptor's intentional policy
// rejection.
//
// The proxy detects this error at every typed-interceptor return path
// and surfaces it as an HTTP 5xx via [oops.E] with [oops.CodeUnexpected],
// matching how [UserRequest.refreshBody] and
// [RemoteMessage.materializedBytes] failures are already handled at
// chain-end materialize time. Without this signal, a setter failure
// flowing through the normal rejection path would be rendered as a
// JSON-RPC InternalError envelope (HTTP 200), incorrectly framing an
// internal anomaly as a user-facing policy decision.
//
// Interceptors should not construct [MutationError] directly; they
// receive it from the setter return and either propagate it (the
// expected pattern) or recover with a fallback mutation that does
// succeed. Swallowing the error and returning nil from the interceptor
// is a bug — the mutation never reaches the wire and the upstream
// payload flows through unmodified.
type MutationError struct {
	// Op is a short human-readable label for the setter that failed —
	// e.g. "set tools", "set arguments" — included in the Error()
	// string and surfaced via traces and logs.
	Op string

	// Cause is the underlying error, typically from [json.Marshal] of
	// the typed view's parsed payload. Wrapped via [Unwrap] so
	// [errors.Is] and [errors.As] traversal reaches it.
	Cause error
}

func (e *MutationError) Error() string {
	return fmt.Sprintf("proxy interceptor mutation: %s: %v", e.Op, e.Cause)
}

func (e *MutationError) Unwrap() error {
	return e.Cause
}
