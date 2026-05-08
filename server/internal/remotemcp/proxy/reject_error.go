package proxy

import (
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// JSON-RPC 2.0 error codes used by the proxy when synthesizing rejection
// responses. The negative range follows the spec: -32700..-32600 are
// reserved (parse error, invalid request, etc.); -32000..-32099 is the
// "server error" band for implementation-defined codes. Mirrors the codes
// used by the existing /mcp endpoint so observers see consistent codes
// across the two surfaces.
const (
	RejectCodeParseError     = -32700
	RejectCodeInvalidRequest = -32600
	RejectCodeMethodNotFound = -32601
	RejectCodeInvalidParams  = -32602
	RejectCodeInternalError  = -32603

	// RejectCodeServerError is the default code for proxy-imposed policy
	// rejections that do not map cleanly to a spec-defined code (e.g.
	// "blocked by tool-usage policy"). It sits in the -32000..-32099
	// implementation-defined band reserved by JSON-RPC 2.0.
	RejectCodeServerError = -32000
)

// RejectError is the typed rejection shape an interceptor can return when it
// wants the proxy to synthesize a JSON-RPC error response (or, on the SSE
// path, a JSON-RPC error event) the user's MCP runtime can correlate and
// surface cleanly. Returning a non-RejectError plain error from an
// interceptor still rejects the message; the proxy maps it through
// [RejectErrorFromCause] using a default mapping.
type RejectError struct {
	// Code is the JSON-RPC error code carried back to the user. Use one of
	// the RejectCode* constants above when applicable.
	Code int

	// Message is the human-readable summary of the rejection. Surfaces in
	// the JSON-RPC error response's "message" field, so treat it as
	// user-facing. Interceptor authors should avoid putting internal
	// details (DB error strings, stack traces, secret-derived data) here;
	// log those via the interceptor's logger instead and pass a sanitized
	// summary as Message.
	Message string

	// Data is an optional structured payload. Surfaces in the JSON-RPC
	// error response's "data" field. Must be JSON-marshalable. Same
	// user-facing constraint as Message — sanitize before populating.
	Data any
}

// Error implements [error]. The string form is intended for logs, not for
// user surfaces — the user-facing string is RejectError.Message.
func (e *RejectError) Error() string {
	return fmt.Sprintf("jsonrpc reject %d: %s", e.Code, e.Message)
}

// RejectErrorFromCause coerces an arbitrary error into a *RejectError so
// the proxy can always synthesize a spec-shaped rejection event when an
// interceptor returns an error. Walks the error chain via [errors.As] so a
// typed *RejectError still surfaces correctly even when the proxy's run*
// helpers wrap it with oops.E during invocation logging — the mapping is:
//
//   - A *RejectError anywhere in the chain is returned as-is. This is the
//     common path for interceptors that opt into typed rejection: the
//     interceptor's RejectError survives the run-helper's oops.E wrap and
//     its Code/Message/Data flow through to the JSON-RPC envelope.
//   - An [oops.ShareableError] is mapped to a JSON-RPC code by Gram's
//     domain-error class, mirroring the table used by the /mcp endpoint's
//     own error-shape conversion.
//   - Anything else falls back to RejectCodeInternalError with a generic
//     message — interceptors that want a richer rejection should return
//     *RejectError directly.
func RejectErrorFromCause(err error) *RejectError {
	if reject, ok := errors.AsType[*RejectError](err); ok {
		return reject
	}

	if oopsErr, ok := errors.AsType[*oops.ShareableError](err); ok {
		return &RejectError{
			Code:    rejectCodeForOops(oopsErr.Code),
			Message: oopsErr.Error(),
			Data:    nil,
		}
	}

	return &RejectError{
		Code:    RejectCodeInternalError,
		Message: "interceptor rejected message",
		Data:    nil,
	}
}

// rejectCodeForOops mirrors the mapping used by the /mcp endpoint's
// NewErrorFromCause so an oops.ShareableError leaving an interceptor
// produces the same JSON-RPC code regardless of which surface (the public
// /mcp server or the /x/mcp proxy) it traversed.
func rejectCodeForOops(code oops.Code) int {
	switch code {
	case oops.CodeBadRequest:
		return RejectCodeParseError
	case oops.CodeUnauthorized,
		oops.CodeForbidden,
		oops.CodeConflict,
		oops.CodeUnsupportedMedia,
		oops.CodeNotFound:
		return RejectCodeInvalidRequest
	case oops.CodeInvalid:
		return RejectCodeInvalidParams
	case oops.CodeUnexpected:
		return RejectCodeInternalError
	default:
		return RejectCodeInternalError
	}
}
