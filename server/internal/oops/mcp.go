package oops

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
)

// MCPErrHandle wraps an MCP/JSON-RPC HTTP handler and serializes returned
// errors as JSON-RPC error responses instead of the generic HTTP error shape.
func MCPErrHandle(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rpcCtx := &contextvalues.RPCContext{ID: mcpjsonrpc.NullID(), ProtocolVersion: ""}
		r = r.WithContext(contextvalues.SetRPCContext(r.Context(), rpcCtx))

		err := handler(w, r)
		if err == nil {
			return
		}

		mcpID := mcpjsonrpc.NullID()
		if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok && rpcCtx.ID.IsSet() {
			mcpID = rpcCtx.ID
		}

		code := http.StatusInternalServerError

		payload := &MCPError{
			ID:      mcpID,
			Code:    MCPCodeInternalError,
			Message: MCPCodeInternalError.Message(),
			Data:    nil,
		}

		var shareableErr *ShareableError
		switch {
		case errors.As(err, &shareableErr):
			// The status stays keyed on the Gram error code, not on the wire
			// code and not on the revision. Errors that escape a handler are
			// transport-level refusals rather than protocol results, and the
			// statuses this path already answers are correct on every
			// revision: 401 carries the OAuth discovery challenge MCP clients
			// need to start their authorization flow, 404 names an unknown
			// server, 405 answers the GET compatibility probe. The
			// specification's status table governs the JSON-RPC errors
			// written from inside the handler, which this is not.
			code = shareableErr.HTTPStatus(r.Context())
			payload.Code = shareableErr.Code.MCPCodeFor(rpcCtx.ProtocolVersion)
			payload.Message = shareableErr.Error()
		default:
			stack := string(debug.Stack())
			logger.ErrorContext(r.Context(), "unexpected error", attr.SlogError(err), attr.SlogErrorStack(stack))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		err = json.NewEncoder(w).Encode(payload)
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to encode MCP error response", attr.SlogError(err))
		}
	})
}

type MCPCode int

const (
	MCPCodeParseError     MCPCode = -32700
	MCPCodeInvalidRequest MCPCode = -32600
	MCPCodeMethodNotFound MCPCode = -32601
	MCPCodeInvalidParams  MCPCode = -32602
	MCPCodeInternalError  MCPCode = -32603

	// Server-defined codes occupy the JSON-RPC 2.0 reserved range -32000 to
	// -32099 and are used for application-level errors.
	MCPCodeServerError      MCPCode = -32000
	MCPCodeUnauthorized     MCPCode = -32001
	MCPCodeResourceNotFound MCPCode = -32002
	MCPCodeForbidden        MCPCode = -32003
)

func (c MCPCode) Message() string {
	switch c {
	case MCPCodeParseError:
		return "Parse error"
	case MCPCodeInvalidRequest:
		return "Invalid Request"
	case MCPCodeMethodNotFound:
		return "Method not found"
	case MCPCodeInvalidParams:
		return "Invalid params"
	case MCPCodeServerError:
		return "Server error"
	case MCPCodeUnauthorized:
		return "Unauthorized"
	case MCPCodeResourceNotFound:
		return "Resource not found"
	case MCPCodeForbidden:
		return "Forbidden"
	default:
		return "Internal error"
	}
}

// MCPErrorDataCode is a stable machine-readable JSON-RPC error code.
type MCPErrorDataCode string

const (
	// MCPErrorDataCodeToolCallsPaused identifies a matched MCP tool-execution pause.
	MCPErrorDataCodeToolCallsPaused MCPErrorDataCode = "mcp_tool_calls_paused"
)

// MCPErrorData is the shared typed data envelope for MCP JSON-RPC errors.
type MCPErrorData struct {
	Code MCPErrorDataCode `json:"code"`
}

type MCPError struct {
	ID      mcpjsonrpc.ID
	Code    MCPCode
	Message string
	Data    *MCPErrorData
}

// NewMCPErrorFromCause converts an error returned by an MCP request handler
// into the JSON-RPC error to write in response to request id. revision is the
// protocol revision in effect for that request, which selects the wire error
// code; an empty or unrecognized value is served the legacy mappings.
//
// An error that already carries its own [MCPError] passes through with its
// code intact, on the grounds that a caller naming a wire code directly has
// chosen it deliberately.
func NewMCPErrorFromCause(id mcpjsonrpc.ID, revision string, source error) *MCPError {
	var mcpErr *MCPError
	var shareableErr *ShareableError

	switch {
	case errors.As(source, &mcpErr):
		if !mcpErr.ID.IsSet() {
			mcpErr.ID = id
		}
		return mcpErr
	case errors.As(source, &shareableErr):
		return &MCPError{
			ID:      id,
			Code:    shareableErr.Code.MCPCodeFor(revision),
			Message: shareableErr.Error(),
			Data:    nil,
		}
	default:
		return &MCPError{
			ID:      id,
			Code:    MCPCodeInternalError,
			Message: MCPCodeInternalError.Message(),
			Data:    nil,
		}
	}
}

func (e *MCPError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%d: %s", e.Code, e.message())
}

func (e *MCPError) MarshalJSON() ([]byte, error) {
	if e == nil {
		return nil, nil
	}

	errorBody := map[string]any{
		"code":    e.Code,
		"message": e.message(),
	}
	if e.Data != nil {
		errorBody["data"] = e.Data
	}

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      e.ID,
		"error":   errorBody,
	}

	bs, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal mcp error: %w", err)
	}

	return bs, nil
}

func (e *MCPError) message() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code.Message()
}
