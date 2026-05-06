package oops

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// MCPErrHandle wraps an MCP/JSON-RPC HTTP handler and serializes returned
// errors as JSON-RPC error responses instead of the generic HTTP error shape.
func MCPErrHandle(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := new(json.RawMessage)
		r = r.WithContext(contextvalues.SetMCPIDContext(r.Context(), id))

		err := handler(w, r)
		if err == nil {
			return
		}

		code := MCPCodeInternalError
		httpCode := code.HTTPStatus()
		message := code.Message()

		var se *ShareableError
		switch {
		case errors.As(err, &se):
			code = se.Code.MCPCode()
			httpCode = se.HTTPStatus()
			message = se.Error()
		default:
			stack := string(debug.Stack())
			logger.ErrorContext(r.Context(), "unexpected error", attr.SlogError(err), attr.SlogErrorStack(stack))
		}

		mcpID, ok := contextvalues.GetMCPID(r.Context())
		if !ok {
			mcpID = json.RawMessage("null")
		}
		payload := mcpErrorPayload(mcpID, code, message)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpCode)
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

func (c MCPCode) HTTPStatus() int {
	switch c {
	case MCPCodeParseError, MCPCodeInvalidRequest, MCPCodeInvalidParams:
		return http.StatusBadRequest
	case MCPCodeMethodNotFound, MCPCodeResourceNotFound:
		return http.StatusNotFound
	case MCPCodeInternalError, MCPCodeServerError:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

type mcpErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Error   mcpError        `json:"error"`
}

type mcpError struct {
	Code    MCPCode `json:"code"`
	Message string  `json:"message"`
}

func mcpErrorPayload(id json.RawMessage, code MCPCode, message string) mcpErrorResponse {
	return mcpErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: mcpError{
			Code:    code,
			Message: message,
		},
	}
}
