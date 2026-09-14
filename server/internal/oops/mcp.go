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
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
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
			// The status starts from the Gram error code, which is what makes
			// this path answer 401 with the OAuth discovery challenge MCP
			// clients need to start authorizing, 403 for a denied toolset, and
			// 405 for the GET compatibility probe. Those wire codes carry no
			// mandated status, so the override leaves them alone on every
			// revision.
			//
			// It does not leave the not-found alone, and must not: the wire
			// code answered here is now the revision's, and a code carries its
			// status with it. A not-found on a modern request answers -32602,
			// whose mandated status is 400. Keeping the Gram code's 404 would
			// pair it with the one status the specification gives a distinct
			// meaning — a 404 carrying a JSON-RPC body says the method is
			// unimplemented — and tell a dual-era client something untrue.
			code = shareableErr.HTTPStatus(r.Context())
			payload.Code = shareableErr.Code.MCPCodeFor(rpcCtx.ProtocolVersion)
			payload.Message = shareableErr.Error()

			if mcpversions.AtLeast(rpcCtx.ProtocolVersion, mcpversions.Version20260728) {
				if mandated, ok := payload.Code.MandatedHTTPStatus(); ok {
					code = mandated
				}
			}
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

	// Codes MCP 2026-07-28 defines for the request-level conditions a server
	// validates before it can act on a request at all. They sit outside the
	// -32000 to -32019 sub-range that revision designates legacy, and unlike
	// the codes in it they carry meaning a modern client is entitled to rely
	// on. Each one is answered with 400.
	MCPCodeHeaderMismatch                  MCPCode = -32020
	MCPCodeMissingRequiredClientCapability MCPCode = -32021
	MCPCodeUnsupportedProtocolVersion      MCPCode = -32022
)

// MandatedHTTPStatus returns the HTTP status MCP 2026-07-28 requires for c,
// and whether that revision mandates one at all. Codes it says nothing about
// report false, so callers leave their status alone rather than inventing one.
//
// Callers apply this only to requests governed by 2026-07-28. The
// handshake-based revisions carry every JSON-RPC error in the body of a
// successful response, and clients there read a non-2xx as a transport failure
// without parsing the body.
func (c MCPCode) MandatedHTTPStatus() (int, bool) {
	switch c {
	case MCPCodeMethodNotFound:
		return http.StatusNotFound, true
	case MCPCodeInvalidParams,
		MCPCodeHeaderMismatch,
		MCPCodeMissingRequiredClientCapability,
		MCPCodeUnsupportedProtocolVersion:
		return http.StatusBadRequest, true
	default:
		return 0, false
	}
}

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
	case MCPCodeHeaderMismatch:
		return "Header mismatch"
	case MCPCodeMissingRequiredClientCapability:
		return "Missing required client capability"
	case MCPCodeUnsupportedProtocolVersion:
		return "Unsupported protocol version"
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
	// Code identifies application-specific failures outside the MCP
	// specification's standard error data shapes.
	Code MCPErrorDataCode `json:"code,omitempty"`

	// Supported is the protocol revision set a client may retry with after an
	// UnsupportedProtocolVersionError.
	Supported []string `json:"supported,omitempty"`

	// Requested is the unsupported protocol revision the client declared.
	Requested string `json:"requested,omitempty"`
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
// An error that already carries its own [MCPError] passes through with the
// code its caller named, since naming a wire code directly is a deliberate
// choice. The one exception is the code 2026-07-28 forbids outright: a MUST
// NOT is not a caller's to opt out of, so it is remapped like any other.
func NewMCPErrorFromCause(id mcpjsonrpc.ID, revision string, source error) *MCPError {
	var mcpErr *MCPError
	var shareableErr *ShareableError

	switch {
	case errors.As(source, &mcpErr):
		// Adjusted on a copy. The caller owns the value it built, and the code
		// chosen here is right for one request only: rewriting it in place
		// would let a value that outlives the request answer -32602 to every
		// later legacy client, which is the compatibility break this branch
		// exists to prevent.
		adjusted := *mcpErr
		if !adjusted.ID.IsSet() {
			adjusted.ID = id
		}
		if adjusted.Code == MCPCodeResourceNotFound && mcpversions.AtLeast(revision, mcpversions.Version20260728) {
			adjusted.Code = MCPCodeInvalidParams
		}
		return &adjusted
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
