package oops

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/stretchr/testify/require"
)

func TestMCPErrHandle_IncludesMCPID(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()

	handler := MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		rpcCtx, ok := contextvalues.GetRPCContext(r.Context())
		if !ok {
			return E(CodeUnexpected, nil, "unexpected error")
		}
		rpcCtx.ID = mcpjsonrpc.StringID("req-1")
		return E(CodeUnauthorized, nil, "unauthorized")
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var response map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "2.0", response["jsonrpc"])
	require.Equal(t, "req-1", response["id"])
	require.NotNil(t, response["error"])
	require.Empty(t, logBuf.String())
}

func TestMCPErrHandle_UsesNullMCPIDWhenMissing(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()

	handler := MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		return E(CodeUnauthorized, nil, "unauthorized")
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)

	var response map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Contains(t, response, "id")
	require.Nil(t, response["id"])
	require.Empty(t, logBuf.String())
}

func TestMCPError_MarshalJSON(t *testing.T) {
	t.Parallel()

	err := &MCPError{
		ID:      mcpjsonrpc.NumberID(1),
		Code:    MCPCodeMethodNotFound,
		Message: "tools/unknown: Method not found",
		Data:    nil,
	}

	data, marshalErr := json.Marshal(err)
	require.NoError(t, marshalErr)

	var response map[string]any
	unmarshalErr := json.Unmarshal(data, &response)
	require.NoError(t, unmarshalErr)
	require.Equal(t, "2.0", response["jsonrpc"])
	require.InDelta(t, 1, response["id"], 0)

	errorBody, ok := response["error"].(map[string]any)
	require.True(t, ok)
	require.InDelta(t, -32601, errorBody["code"], 0)
	require.Equal(t, "tools/unknown: Method not found", errorBody["message"])
	require.NotContains(t, errorBody, "data")

	err.Data = &MCPErrorData{Code: MCPErrorDataCode("typed_code")}
	data, marshalErr = json.Marshal(err)
	require.NoError(t, marshalErr)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"tools/unknown: Method not found","data":{"code":"typed_code"}}}`, string(data))
}

func TestCodeMCPCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code Code
		want MCPCode
	}{
		{"unauthorized_uses_server_defined_code", CodeUnauthorized, MCPCodeUnauthorized},
		{"forbidden_uses_server_defined_code", CodeForbidden, MCPCodeForbidden},
		{"bad_request_is_invalid_request", CodeBadRequest, MCPCodeInvalidRequest},
		{"parse_error_is_parse_error", CodeParseError, MCPCodeParseError},
		{"conflict_is_invalid_request", CodeConflict, MCPCodeInvalidRequest},
		{"unsupported_media_is_invalid_request", CodeUnsupportedMedia, MCPCodeInvalidRequest},
		{"method_not_allowed_is_server_error", CodeMethodNotAllowed, MCPCodeServerError},
		{"not_found_is_resource_not_found", CodeNotFound, MCPCodeResourceNotFound},
		{"invalid_is_invalid_params", CodeInvalid, MCPCodeInvalidParams},
		{"not_implemented_is_method_not_found", CodeNotImplemented, MCPCodeMethodNotFound},
		{"unexpected_defaults_to_internal", CodeUnexpected, MCPCodeInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.code.MCPCode())
		})
	}

	t.Run("auth_codes_are_in_server_defined_range", func(t *testing.T) {
		t.Parallel()

		for _, c := range []MCPCode{MCPCodeUnauthorized, MCPCodeForbidden} {
			require.GreaterOrEqual(t, int(c), -32099, "server-defined codes must be >= -32099")
			require.LessOrEqual(t, int(c), -32000, "server-defined codes must be <= -32000")
		}
	})
}

func TestNewMCPErrorFromCause(t *testing.T) {
	t.Parallel()

	id := mcpjsonrpc.StringID("req-1")

	t.Run("returns_existing_mcp_error", func(t *testing.T) {
		t.Parallel()

		existing := &MCPError{ID: mcpjsonrpc.ID{Number: 0, String: ""}, Code: MCPCodeMethodNotFound, Message: "missing", Data: nil}
		err := NewMCPErrorFromCause(id, mcpversions.Version20251125, existing)

		require.Same(t, existing, err)
		require.Equal(t, id, err.ID)
	})

	t.Run("maps_shareable_error_code", func(t *testing.T) {
		t.Parallel()

		err := NewMCPErrorFromCause(id, mcpversions.Version20251125, E(CodeNotFound, nil, "mcp server not found"))

		require.Equal(t, id, err.ID)
		require.Equal(t, MCPCodeResourceNotFound, err.Code)
		require.Equal(t, "mcp server not found", err.Message)
	})

	t.Run("defaults_unknown_error_to_internal", func(t *testing.T) {
		t.Parallel()

		err := NewMCPErrorFromCause(id, mcpversions.Version20251125, errors.New("boom"))

		require.Equal(t, id, err.ID)
		require.Equal(t, MCPCodeInternalError, err.Code)
		require.Equal(t, "Internal error", err.Message)
	})
}

// TestCodeMCPCodeFor_LegacyRevisionsKeepRetiredResourceNotFound pins the half
// of the 2026-07-28 error-code rule that is easy to lose in a refactor: the
// retired -32002 is not merely permitted on the handshake-based revisions, it
// is what their clients are told to expect, so the mapping is a branch rather
// than a migration.
func TestCodeMCPCodeFor_LegacyRevisionsKeepRetiredResourceNotFound(t *testing.T) {
	t.Parallel()

	for _, revision := range []string{
		mcpversions.Version20241105,
		mcpversions.Version20250326,
		mcpversions.Version20250618,
		mcpversions.Version20251125,
	} {
		require.Equal(t, MCPCodeResourceNotFound, CodeNotFound.MCPCodeFor(revision), "revision %s", revision)
	}
}

// TestCodeMCPCodeFor_ModernRevisionReplacesRetiredResourceNotFound covers the
// MUST NOT: an implementation of 2026-07-28 may not emit -32002 at all.
func TestCodeMCPCodeFor_ModernRevisionReplacesRetiredResourceNotFound(t *testing.T) {
	t.Parallel()

	require.Equal(t, MCPCodeInvalidParams, CodeNotFound.MCPCodeFor(mcpversions.Version20260728))
}

// TestCodeMCPCodeFor_UnresolvedRevisionIsLegacy covers the error paths that
// fail before a revision can be resolved — everything ahead of body decode.
// They have no declaration to read, so they must not be answered as modern.
func TestCodeMCPCodeFor_UnresolvedRevisionIsLegacy(t *testing.T) {
	t.Parallel()

	require.Equal(t, MCPCodeResourceNotFound, CodeNotFound.MCPCodeFor(""))
	require.Equal(t, MCPCodeResourceNotFound, CodeNotFound.MCPCodeFor("not-a-revision"))
}

// TestCodeMCPCodeFor_OnlyResourceNotFoundIsRevisionConditional guards the
// scope of the branch. Gram's other server-defined codes sit in the range
// 2026-07-28 designates legacy, but only -32002 is a MUST NOT; the rest stay
// legal to emit and must not be quietly remapped alongside it.
func TestCodeMCPCodeFor_OnlyResourceNotFoundIsRevisionConditional(t *testing.T) {
	t.Parallel()

	for _, code := range []Code{
		CodeUnauthorized,
		CodeForbidden,
		CodeBadRequest,
		CodeConflict,
		CodeFailedPrecondition,
		CodeUnsupportedMedia,
		CodeMethodNotAllowed,
		CodeInvalid,
		CodeNotImplemented,
		CodeUnexpected,
	} {
		require.Equal(t, code.MCPCode(), code.MCPCodeFor(mcpversions.Version20260728), "code %s", code)
	}
}

// TestNewMCPErrorFromCause_ModernRevisionReplacesRetiredResourceNotFound
// covers the in-handler write path, which is where Gram's general not-found —
// an unknown tool, toolset, or endpoint — reaches the wire.
func TestNewMCPErrorFromCause_ModernRevisionReplacesRetiredResourceNotFound(t *testing.T) {
	t.Parallel()

	err := NewMCPErrorFromCause(mcpjsonrpc.StringID("req-1"), mcpversions.Version20260728, E(CodeNotFound, nil, "unknown tool"))

	require.Equal(t, MCPCodeInvalidParams, err.Code)
	require.Equal(t, "unknown tool", err.Message)
}

// TestNewMCPErrorFromCause_PreservesExplicitCodeOnModernRevision covers the
// passthrough arm: a caller that names a wire code itself has chosen it, and
// the revision mapping must not second-guess that.
func TestNewMCPErrorFromCause_PreservesExplicitCodeOnModernRevision(t *testing.T) {
	t.Parallel()

	existing := &MCPError{ID: mcpjsonrpc.NullID(), Code: MCPCodeResourceNotFound, Message: "explicit", Data: nil}
	err := NewMCPErrorFromCause(mcpjsonrpc.StringID("req-1"), mcpversions.Version20260728, existing)

	require.Same(t, existing, err)
	require.Equal(t, MCPCodeResourceNotFound, err.Code)
}

// TestMCPErrHandle_ModernRevisionReplacesRetiredResourceNotFound covers the
// outer path. A not-found can escape a handler after the body is decoded — a
// private MCP server reached anonymously is answered that way — so this path
// carries the MUST NOT too, and reaches the revision through the mutable
// holder it installs rather than through the request context it captured.
func TestMCPErrHandle_ModernRevisionReplacesRetiredResourceNotFound(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	handler := MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		rpcCtx, ok := contextvalues.GetRPCContext(r.Context())
		require.True(t, ok)
		rpcCtx.ProtocolVersion = mcpversions.Version20260728

		return C(CodeNotFound)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

	var response struct {
		Error struct {
			Code MCPCode `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, MCPCodeInvalidParams, response.Error.Code)
}

// TestMCPErrHandle_StatusStaysKeyedOnGramCodeForModernRevision pins the
// deliberate asymmetry between the two error paths. The statuses this wrapper
// answers are transport-level refusals that are correct on every revision, and
// the 401 in particular is what an MCP client needs to begin its OAuth
// discovery flow. Deriving the status from the revision here would answer that
// challenge with a 200 and strand every client that has to authorize.
func TestMCPErrHandle_StatusStaysKeyedOnGramCodeForModernRevision(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	for code, want := range map[Code]int{
		CodeUnauthorized:     http.StatusUnauthorized,
		CodeForbidden:        http.StatusForbidden,
		CodeNotFound:         http.StatusNotFound,
		CodeMethodNotAllowed: http.StatusMethodNotAllowed,
	} {
		handler := MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
			rpcCtx, ok := contextvalues.GetRPCContext(r.Context())
			require.True(t, ok)
			rpcCtx.ProtocolVersion = mcpversions.Version20260728

			return C(code)
		})

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

		require.Equal(t, want, rec.Code, "code %s", code)
	}
}
