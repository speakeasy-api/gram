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
		rpcCtx := &contextvalues.RPCContext{ID: mcpjsonrpc.NullID()}
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
		}

		var shareableErr *ShareableError
		switch {
		case errors.As(err, &shareableErr):
			code = shareableErr.HTTPStatus()
			payload.Code = shareableErr.Code.MCPCode()
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
	MCPCodeParseError       MCPCode = -32700
	MCPCodeInvalidRequest   MCPCode = -32600
	MCPCodeMethodNotFound   MCPCode = -32601
	MCPCodeInvalidParams    MCPCode = -32602
	MCPCodeInternalError    MCPCode = -32603
	MCPCodeServerError      MCPCode = -32000
	MCPCodeResourceNotFound MCPCode = -32002
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
	case MCPCodeResourceNotFound:
		return "Resource not found"
	default:
		return "Internal error"
	}
}

type MCPError struct {
	ID      mcpjsonrpc.ID
	Code    MCPCode
	Message string
}

func NewMCPErrorFromCause(id mcpjsonrpc.ID, source error) *MCPError {
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
			Code:    shareableErr.Code.MCPCode(),
			Message: shareableErr.Error(),
		}
	default:
		return &MCPError{
			ID:      id,
			Code:    MCPCodeInternalError,
			Message: MCPCodeInternalError.Message(),
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

	payload := map[string]any{
		"jsonrpc": "2.0",
		"error":   errorBody,
	}
	if e.ID.IsSet() {
		payload["id"] = e.ID
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
