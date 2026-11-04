package mcp

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
)

const (
	// toolCallShutdownMessage answers a tools/call the shutdown drain
	// interrupted. The tool may have finished its work before the gateway
	// stopped waiting, so the message does not claim it failed.
	toolCallShutdownMessage = "Speakeasy is restarting and stopped waiting for the tool's response. The tool call may still have completed."

	// resourceReadShutdownMessage answers a resources/read the shutdown drain
	// interrupted. Reads have no side effects, so the client can retry.
	resourceReadShutdownMessage = "Speakeasy is restarting and stopped waiting for the resource. Retry the read."
)

// shutdownInterruption is how a handler answers a tools/call or resources/read
// whose gateway call the shutdown drain abandoned.
type shutdownInterruption struct {
	// Response is the JSON-RPC error the client receives. It is a bare
	// *oops.MCPError so callers that degrade shareable errors, such as meta
	// server member dispatch, pass it through unchanged.
	Response *oops.MCPError

	// Failure is the failure recorded on the call's telemetry and tool call
	// record. Its 503 status attributes the failure to the deploy rather than
	// to the tool.
	Failure *oops.ShareableError
}

// interruptedAtShutdown records a tools/call or resources/read the shutdown
// drain abandoned and builds its answer. method is the MCP method that ran the
// call, toolKind the kind of tool or resource the gateway was calling, and
// cause the error the gateway returned. logAttrs, when not nil, gains the
// interrupt reason for the call's telemetry log.
func interruptedAtShutdown(
	ctx context.Context,
	logger *slog.Logger,
	metrics *mcpmetrics.Metrics,
	logAttrs tm.HTTPLogAttributes,
	method string,
	toolKind string,
	cause error,
) shutdownInterruption {
	reason := string(oops.MCPErrorDataReasonServerShutdown)

	logger.WarnContext(ctx, "interrupted tool call at shutdown",
		attr.SlogMcpMethod(method),
		attr.SlogToolCallKind(toolKind),
		attr.SlogToolCallInterruptReason(reason),
		attr.SlogError(cause),
	)
	metrics.RecordShutdownInterrupted(ctx, method, toolKind)
	if logAttrs != nil {
		logAttrs[attr.ToolCallInterruptReasonKey] = reason
	}

	message := resourceReadShutdownMessage
	if method == mcpversions.MethodToolsCall {
		message = toolCallShutdownMessage
	}

	return shutdownInterruption{
		Response: &oops.MCPError{
			ID:      mcpjsonrpc.ID{Number: 0, String: ""},
			Code:    oops.MCPCodeInternalError,
			Message: message,
			// A tools/call may already have had side effects, so only a read
			// is marked retryable.
			Data: oops.NewServerShutdownErrorData(method != mcpversions.MethodToolsCall),
		},
		Failure: oops.E(oops.CodeUnavailable, cause, "interrupted by server shutdown"),
	}
}
