package remotemcp

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// ToolsCallStripToolsetIDInterceptor removes the Gram-injected
// [shadowmcp.XGramToolsetIDField] property from tools/call arguments before
// the proxy forwards them upstream, so the remote MCP server sees its own
// declared argument shape rather than Gram's envelope.
//
// Gram no longer injects that property into tools/list schemas (DNO-603).
// The strip stays because MCP clients cache tool schemas per session: a
// caller that listed tools before the injection was removed keeps echoing
// the property back for the life of its session. Delete this interceptor in
// a later contract step, once those cached schemas have aged out.
//
// The strip runs for every project. It previously sat behind
// [shadowmcp.Client.IsEnabledForProject], but gating a strip on a
// 15-minute Redis-cached, per-project policy lookup that fails closed to
// "off" meant a policy toggle or a transient cache/DB outage could forward
// Gram's envelope upstream mid-session — exactly the leak the strip exists
// to prevent. Running it always also keeps a Redis GET off the proxy hot
// path.
//
// It is not, however, unconditional in the strict sense: the hot-path
// byte-scan below matches raw wire bytes, so a caller that escapes the key
// (`x-gram-toolset-id`) slips a decoded [shadowmcp.XGramToolsetIDField]
// property past it. That is not a leak — the whole arguments payload is
// caller-supplied and nothing of Gram's is exposed — just a caller
// declining a courtesy scrub of their own request.
//
// One accepted consequence: an upstream tool that genuinely declares its
// own [shadowmcp.XGramToolsetIDField] argument has it silently dropped.
// For projects with a tool-identity policy enabled that collision was
// already resolved in Gram's favour, since the injector deliberately
// overwrote such a property. Every other project is newly affected,
// because neither inject nor strip used to run there. No known upstream
// server declares the property.
type ToolsCallStripToolsetIDInterceptor struct {
	logger *slog.Logger
}

var _ proxy.ToolsCallRequestInterceptor = (*ToolsCallStripToolsetIDInterceptor)(nil)

// toolsetIDFieldBytes is the byte form of the property name, hoisted so the
// hot-path scan below doesn't re-convert the string on every tool call.
var toolsetIDFieldBytes = []byte(shadowmcp.XGramToolsetIDField)

// NewToolsCallStripToolsetIDInterceptor constructs the strip interceptor.
// It holds no per-server or per-project state: the property is Gram's own
// envelope regardless of which server the request routed to, so stripping
// it needs no scope to consult.
func NewToolsCallStripToolsetIDInterceptor(logger *slog.Logger) *ToolsCallStripToolsetIDInterceptor {
	return &ToolsCallStripToolsetIDInterceptor{logger: logger}
}

// Name implements [proxy.ToolsCallRequestInterceptor].
func (i *ToolsCallStripToolsetIDInterceptor) Name() string {
	return "tools-call-strip-toolset-id"
}

// InterceptToolsCallRequest implements [proxy.ToolsCallRequestInterceptor].
// It strips the echoed [shadowmcp.XGramToolsetIDField] property from the
// arguments before the proxy forwards them upstream. Arguments that don't
// carry the property — the overwhelming majority — pass through untouched.
func (i *ToolsCallStripToolsetIDInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	if call == nil || call.Params == nil {
		return nil
	}

	// Byte-scan before parsing. StripToolsetIDProperty unmarshals the whole
	// arguments object once it sees a leading '{', and this interceptor now
	// runs on every proxied tool call rather than only policy-enabled ones.
	if !bytes.Contains(call.Params.Arguments, toolsetIDFieldBytes) {
		return nil
	}

	stripped, err := shadowmcp.StripToolsetIDProperty(call.Params.Arguments)
	if err != nil {
		// Defensive only. The proxy decodes the request body and unmarshals
		// the tools/call params before building the typed view, and
		// encoding/json validates a whole document before decoding any of
		// it, so arguments that don't parse never reach an interceptor —
		// the request is rejected upstream of this chain. Should that ever
		// change, surface a client-side parse error rather than letting the
		// bare error fall through RejectErrorFromCause to
		// RejectCodeInternalError and present a Gram 500 for a caller's
		// malformed body. Mirrors the toolset path, which maps the same
		// failure to oops.CodeBadRequest.
		i.logger.DebugContext(ctx, "tools/call arguments could not be parsed to strip the gram toolset id property",
			attr.SlogError(err))
		return &proxy.RejectError{
			Code:    proxy.RejectCodeParseError,
			Message: "tools/call arguments could not be parsed as JSON",
			Data:    nil,
		}
	}

	// The byte-scan matches the property name anywhere in the payload, so a
	// hit on a value ("how do I set x-gram-toolset-id") or on a nested key
	// reaches this point with nothing actually removed. Committing an
	// unchanged payload is not free: SetArguments flips the request's dirty
	// flag, which re-encodes the whole JSON-RPC message before forwarding
	// (dropping any params member CallToolParamsRaw doesn't model) and logs
	// "forwarding mutated request body upstream". Only commit a real change.
	if bytes.Equal(stripped, call.Params.Arguments) {
		return nil
	}

	if err := call.SetArguments(stripped); err != nil {
		return fmt.Errorf("commit scrubbed tools/call arguments: %w", err)
	}

	return nil
}
