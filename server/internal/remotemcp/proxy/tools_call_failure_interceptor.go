package proxy

import (
	"context"
	"net/http"
)

// ToolsCallFailure describes a forwarded "tools/call" that ended without a
// reply reaching the [ToolsCallResponseInterceptor] chain: the remote MCP
// server could not be reached, answered with an empty or non-JSON-RPC body
// (an error status relayed verbatim included), had its response refused by
// [Proxy.UpstreamResponseInterceptor], or ended its stream before the reply.
type ToolsCallFailure struct {
	// Request is the tools/call request that failed.
	Request *ToolsCallRequest

	// StatusCode is the remote MCP server's HTTP status, or zero when no
	// response arrived.
	StatusCode int

	// Header is the remote MCP server's response header, or nil when no
	// response arrived.
	Header http.Header

	// Err is the error the proxy returned to its caller, or nil when the
	// proxy answered the user itself.
	Err error
}

// ToolsCallFailureInterceptor is told about each [ToolsCallFailure] once the
// proxy is done with the request. It only observes: the user's answer is
// already decided.
//
// It does not run for notifications, for calls the user canceled, for calls
// rejected before they were sent, or for calls whose reply reached the
// [ToolsCallResponseInterceptor] chain — those report a JSON-RPC error or a
// tool error there.
type ToolsCallFailureInterceptor interface {
	// InterceptToolsCallFailure is called with the failed call.
	InterceptToolsCallFailure(ctx context.Context, failure *ToolsCallFailure)

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

// ForwardSetupKind distinguishes local setup from an upstream transport failure.
type ForwardSetupKind uint8

const (
	ForwardSetupInternal ForwardSetupKind = iota
	ForwardSetupConfiguration
	ForwardSetupCaller
)

// ForwardSetupError preserves the response-facing error while naming why no request was sent.
type ForwardSetupError struct {
	Err  error
	Kind ForwardSetupKind
}

func (e *ForwardSetupError) Error() string { return e.Err.Error() }
func (e *ForwardSetupError) Unwrap() error { return e.Err }
