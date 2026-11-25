package oops_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestMCPErrHandle_IncludesMCPID(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()

	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		rpcCtx, ok := contextvalues.GetRPCContext(r.Context())
		if !ok {
			return oops.E(oops.CodeUnexpected, nil, "unexpected error")
		}
		rpcCtx.ID = mcpjsonrpc.StringID("req-1")
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
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

	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
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

	err := &oops.MCPError{
		ID:      mcpjsonrpc.NumberID(1),
		Code:    oops.MCPCodeMethodNotFound,
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

	err.Data = &oops.MCPErrorData{
		Code:      oops.MCPErrorDataCode("typed_code"),
		Supported: nil,
		Requested: "",
	}
	data, marshalErr = json.Marshal(err)
	require.NoError(t, marshalErr)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"tools/unknown: Method not found","data":{"code":"typed_code"}}}`, string(data))
}

func TestMCPError_MarshalJSONFor_ID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		revision string
		id       mcpjsonrpc.ID
		want     string
	}{
		{name: "handshake null", revision: mcpversions.Version20251125, id: mcpjsonrpc.NullID(), want: "null"},
		{name: "unresolved null", id: mcpjsonrpc.NullID(), want: "null"},
		{name: "per-request null", revision: mcpversions.Version20260728, id: mcpjsonrpc.NullID()},
		{name: "per-request unset", revision: mcpversions.Version20260728},
		{name: "per-request zero", revision: mcpversions.Version20260728, id: mcpjsonrpc.NumberID(0), want: "0"},
		{name: "per-request empty string", revision: mcpversions.Version20260728, id: mcpjsonrpc.StringID(""), want: `""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := &oops.MCPError{ID: tc.id, Code: oops.MCPCodeInvalidParams}
			bs, marshalErr := err.MarshalJSONFor(tc.revision)
			require.NoError(t, marshalErr)
			var response struct {
				ID json.RawMessage `json:"id"`
			}
			require.NoError(t, json.Unmarshal(bs, &response))
			require.Equal(t, tc.want, string(response.ID))
		})
	}
}

func TestCodeMCPCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code oops.Code
		want oops.MCPCode
	}{
		{"unauthorized_uses_server_defined_code", oops.CodeUnauthorized, oops.MCPCodeUnauthorized},
		{"forbidden_uses_server_defined_code", oops.CodeForbidden, oops.MCPCodeForbidden},
		{"bad_request_is_invalid_request", oops.CodeBadRequest, oops.MCPCodeInvalidRequest},
		{"parse_error_is_parse_error", oops.CodeParseError, oops.MCPCodeParseError},
		{"conflict_is_invalid_request", oops.CodeConflict, oops.MCPCodeInvalidRequest},
		{"unsupported_media_is_invalid_request", oops.CodeUnsupportedMedia, oops.MCPCodeInvalidRequest},
		{"method_not_allowed_is_server_error", oops.CodeMethodNotAllowed, oops.MCPCodeServerError},
		{"not_found_is_resource_not_found", oops.CodeNotFound, oops.MCPCodeResourceNotFound},
		{"invalid_is_invalid_params", oops.CodeInvalid, oops.MCPCodeInvalidParams},
		{"not_implemented_is_method_not_found", oops.CodeNotImplemented, oops.MCPCodeMethodNotFound},
		{"unexpected_defaults_to_internal", oops.CodeUnexpected, oops.MCPCodeInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.code.MCPCode())
		})
	}

	t.Run("auth_codes_are_in_server_defined_range", func(t *testing.T) {
		t.Parallel()

		for _, c := range []oops.MCPCode{oops.MCPCodeUnauthorized, oops.MCPCodeForbidden} {
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

		existing := &oops.MCPError{ID: mcpjsonrpc.ID{Number: 0, String: ""}, Code: oops.MCPCodeMethodNotFound, Message: "missing", Data: nil}
		err := oops.NewMCPErrorFromCause(id, mcpversions.Version20251125, existing)

		require.NotSame(t, existing, err, "the caller's value is adjusted on a copy")
		require.Equal(t, oops.MCPCodeMethodNotFound, err.Code)
		require.Equal(t, "missing", err.Message)
		require.Equal(t, id, err.ID)
		require.False(t, existing.ID.IsSet(), "the caller's value keeps the id it was built with")
	})

	t.Run("maps_shareable_error_code", func(t *testing.T) {
		t.Parallel()

		err := oops.NewMCPErrorFromCause(id, mcpversions.Version20251125, oops.E(oops.CodeNotFound, nil, "mcp server not found"))

		require.Equal(t, id, err.ID)
		require.Equal(t, oops.MCPCodeResourceNotFound, err.Code)
		require.Equal(t, "mcp server not found", err.Message)
	})

	t.Run("defaults_unknown_error_to_internal", func(t *testing.T) {
		t.Parallel()

		err := oops.NewMCPErrorFromCause(id, mcpversions.Version20251125, errors.New("boom"))

		require.Equal(t, id, err.ID)
		require.Equal(t, oops.MCPCodeInternalError, err.Code)
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
		require.Equal(t, oops.MCPCodeResourceNotFound, oops.CodeNotFound.MCPCodeFor(revision), "revision %s", revision)
	}
}

// TestCodeMCPCodeFor_ModernRevisionReplacesRetiredResourceNotFound covers the
// MUST NOT: an implementation of 2026-07-28 may not emit -32002 at all.
func TestCodeMCPCodeFor_ModernRevisionReplacesRetiredResourceNotFound(t *testing.T) {
	t.Parallel()

	require.Equal(t, oops.MCPCodeInvalidParams, oops.CodeNotFound.MCPCodeFor(mcpversions.Version20260728))
}

// TestCodeMCPCodeFor_UnresolvedRevisionIsLegacy covers the error paths that
// fail before a revision can be resolved — everything ahead of body decode.
// They have no declaration to read, so they must not be answered as modern.
func TestCodeMCPCodeFor_UnresolvedRevisionIsLegacy(t *testing.T) {
	t.Parallel()

	require.Equal(t, oops.MCPCodeResourceNotFound, oops.CodeNotFound.MCPCodeFor(""))
	require.Equal(t, oops.MCPCodeResourceNotFound, oops.CodeNotFound.MCPCodeFor("not-a-revision"))
}

// TestCodeMCPCodeFor_OnlyResourceNotFoundIsRevisionConditional guards the
// scope of the branch. Speakeasy's other server-defined codes sit in the range
// 2026-07-28 designates legacy, but only -32002 is a MUST NOT; the rest stay
// legal to emit and must not be quietly remapped alongside it.
func TestCodeMCPCodeFor_OnlyResourceNotFoundIsRevisionConditional(t *testing.T) {
	t.Parallel()

	for _, code := range []oops.Code{
		oops.CodeUnauthorized,
		oops.CodeForbidden,
		oops.CodeBadRequest,
		oops.CodeConflict,
		oops.CodeFailedPrecondition,
		oops.CodeUnsupportedMedia,
		oops.CodeMethodNotAllowed,
		oops.CodeInvalid,
		oops.CodeNotImplemented,
		oops.CodeUnexpected,
	} {
		require.Equal(t, code.MCPCode(), code.MCPCodeFor(mcpversions.Version20260728), "code %s", code)
	}
}

// TestNewMCPErrorFromCause_ModernRevisionReplacesRetiredResourceNotFound
// covers the in-handler write path, which is where Speakeasy's general not-found —
// an unknown tool, toolset, or endpoint — reaches the wire.
func TestNewMCPErrorFromCause_ModernRevisionReplacesRetiredResourceNotFound(t *testing.T) {
	t.Parallel()

	err := oops.NewMCPErrorFromCause(mcpjsonrpc.StringID("req-1"), mcpversions.Version20260728, oops.E(oops.CodeNotFound, nil, "unknown tool"))

	require.Equal(t, oops.MCPCodeInvalidParams, err.Code)
	require.Equal(t, "unknown tool", err.Message)
}

// TestNewMCPErrorFromCause_PreservesExplicitCodeOnModernRevision covers the
// passthrough arm: a caller that names a wire code itself has chosen it, and
// the revision mapping must not second-guess that.
func TestNewMCPErrorFromCause_PreservesExplicitCodeOnModernRevision(t *testing.T) {
	t.Parallel()

	existing := &oops.MCPError{ID: mcpjsonrpc.NullID(), Code: oops.MCPCodeForbidden, Message: "explicit", Data: nil}
	err := oops.NewMCPErrorFromCause(mcpjsonrpc.StringID("req-1"), mcpversions.Version20260728, existing)

	require.Equal(t, oops.MCPCodeForbidden, err.Code)
}

// TestNewMCPErrorFromCause_DoesNotMutateTheCallersError covers the lifetime
// hazard in remapping a caller-supplied error. The code chosen here is right
// for one request, so writing it back into a value the caller may hold would
// let a single modern request leave every later legacy client answered with
// -32602: the compatibility break the revision branch exists to prevent.
func TestNewMCPErrorFromCause_DoesNotMutateTheCallersError(t *testing.T) {
	t.Parallel()

	existing := &oops.MCPError{ID: mcpjsonrpc.NullID(), Code: oops.MCPCodeResourceNotFound, Message: "explicit", Data: nil}

	modern := oops.NewMCPErrorFromCause(mcpjsonrpc.StringID("req-1"), mcpversions.Version20260728, existing)
	require.Equal(t, oops.MCPCodeInvalidParams, modern.Code)
	require.Equal(t, oops.MCPCodeResourceNotFound, existing.Code, "the caller's value is untouched")

	legacy := oops.NewMCPErrorFromCause(mcpjsonrpc.StringID("req-2"), mcpversions.Version20251125, existing)
	require.Equal(t, oops.MCPCodeResourceNotFound, legacy.Code, "a legacy request after a modern one still gets the retired code")
}

// TestNewMCPErrorFromCause_RemapsExplicitRetiredCodeOnModernRevision covers the
// limit of that deference. Naming a wire code is a caller's choice, but the one
// code 2026-07-28 forbids outright is not a choice that stays available, so the
// passthrough arm remaps it like any other.
func TestNewMCPErrorFromCause_RemapsExplicitRetiredCodeOnModernRevision(t *testing.T) {
	t.Parallel()

	existing := &oops.MCPError{ID: mcpjsonrpc.NullID(), Code: oops.MCPCodeResourceNotFound, Message: "explicit", Data: nil}
	err := oops.NewMCPErrorFromCause(mcpjsonrpc.StringID("req-1"), mcpversions.Version20260728, existing)

	require.Equal(t, oops.MCPCodeInvalidParams, err.Code)
}

// TestNewMCPErrorFromCause_KeepsExplicitRetiredCodeOnLegacyRevision is the
// other half: the retired code is still what legacy clients are told to expect.
func TestNewMCPErrorFromCause_KeepsExplicitRetiredCodeOnLegacyRevision(t *testing.T) {
	t.Parallel()

	existing := &oops.MCPError{ID: mcpjsonrpc.NullID(), Code: oops.MCPCodeResourceNotFound, Message: "explicit", Data: nil}
	err := oops.NewMCPErrorFromCause(mcpjsonrpc.StringID("req-1"), mcpversions.Version20251125, existing)

	require.Equal(t, oops.MCPCodeResourceNotFound, err.Code)
}

// TestMCPErrHandle_ModernRevisionUsesMandatedStatusForRemappedNotFound covers
// the coupling between the two halves of this path. Once the wire code is
// revision-aware, the status has to follow it: a not-found answers -32602 on a
// modern request, and answering that with the Speakeasy code's 404 would tell a
// dual-era client the method is unimplemented, since a 404 carrying a JSON-RPC
// body is precisely that signal.
func TestMCPErrHandle_ModernRevisionUsesMandatedStatusForRemappedNotFound(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok {
			rpcCtx.ID = mcpjsonrpc.StringID("req-1")
			rpcCtx.ProtocolVersion = mcpversions.Version20260728
		}

		return oops.C(oops.CodeNotFound)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestMCPErrHandle_LegacyRevisionKeepsNotFoundStatus pins that the override is
// confined to the modern branch, so the statuses this path answers today are
// untouched for the traffic that has always received them.
func TestMCPErrHandle_LegacyRevisionKeepsNotFoundStatus(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok {
			rpcCtx.ID = mcpjsonrpc.StringID("req-1")
			rpcCtx.ProtocolVersion = mcpversions.Version20251125
		}

		return oops.C(oops.CodeNotFound)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestMCPErrHandle_ModernRevisionReplacesRetiredResourceNotFound covers the
// outer path. A not-found can escape a handler after the body is decoded — a
// private MCP server reached anonymously is answered that way — so this path
// carries the MUST NOT too, and reaches the revision through the mutable
// holder it installs rather than through the request context it captured.
func TestMCPErrHandle_ModernRevisionReplacesRetiredResourceNotFound(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	var holderInstalled bool
	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		rpcCtx, ok := contextvalues.GetRPCContext(r.Context())
		holderInstalled = ok
		if ok {
			rpcCtx.ProtocolVersion = mcpversions.Version20260728
		}

		return oops.C(oops.CodeNotFound)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

	require.True(t, holderInstalled, "the wrapper must install the holder before dispatch, or the handler has nowhere to publish the revision")

	var response struct {
		Error struct {
			Code oops.MCPCode `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, oops.MCPCodeInvalidParams, response.Error.Code)
}

// TestMCPErrHandle_UnmandatedCodesKeepGramStatusOnModernRevision pins the
// statuses that must survive the modern branch. None of these Speakeasy codes maps
// to a wire code 2026-07-28 assigns a status to, so each keeps the status this
// path has always answered. The 401 is the one that would hurt most to lose:
// it carries the challenge an MCP client needs to begin OAuth discovery, and
// answering it with anything else strands every client that has to authorize.
func TestMCPErrHandle_UnmandatedCodesKeepGramStatusOnModernRevision(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	for _, tt := range []struct {
		code oops.Code
		want int
	}{
		{code: oops.CodeUnauthorized, want: http.StatusUnauthorized},
		{code: oops.CodeForbidden, want: http.StatusForbidden},
		{code: oops.CodeMethodNotAllowed, want: http.StatusMethodNotAllowed},
	} {
		handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
			if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok {
				rpcCtx.ID = mcpjsonrpc.StringID("req-1")
				rpcCtx.ProtocolVersion = mcpversions.Version20260728
			}

			return oops.C(tt.code)
		})

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

		require.Equal(t, tt.want, rec.Code, "code %s", tt.code)
	}
}

// TestMCPErrHandle_ModernRevisionUsesMandatedStatusForNullID covers the case
// the two error paths previously disagreed on. A message naming no request
// still gets the status its condition mandates: this path has no 200 to fall
// back to, so skipping the override here would leave the Speakeasy code's 404 on a
// -32602 body and tell a dual-era client the method is unimplemented.
func TestMCPErrHandle_ModernRevisionUsesMandatedStatusForNullID(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok {
			rpcCtx.ProtocolVersion = mcpversions.Version20260728
		}

		return oops.C(oops.CodeNotFound)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

	require.Equal(t, http.StatusBadRequest, rec.Code)

	var response map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.NotContains(t, response, "id")
}

// TestMCPErrHandle_ModernRevisionMapsNotImplementedToNotFound covers the one
// override that moves a status the specification names for a reason. Speakeasy
// answers an unimplemented feature with 501, but the wire code is -32601, and
// 404 carrying that code is what lets a dual-era client tell a modern server
// missing a method from a legacy server missing the endpoint.
func TestMCPErrHandle_ModernRevisionMapsNotImplementedToNotFound(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	require.Equal(t, http.StatusNotImplemented, oops.StatusCodes[oops.CodeNotImplemented],
		"this test's premise: the Speakeasy status differs from the mandated one")

	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok {
			rpcCtx.ID = mcpjsonrpc.StringID("req-1")
			rpcCtx.ProtocolVersion = mcpversions.Version20260728
		}

		return oops.C(oops.CodeNotImplemented)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestMCPErrHandle_ModernRevisionMapsInvalidToBadRequest covers the other
// status the override moves: Speakeasy answers an invalid payload with 422, and
// -32602 mandates 400.
func TestMCPErrHandle_ModernRevisionMapsInvalidToBadRequest(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	require.Equal(t, http.StatusUnprocessableEntity, oops.StatusCodes[oops.CodeInvalid],
		"this test's premise: the Speakeasy status differs from the mandated one")

	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok {
			rpcCtx.ID = mcpjsonrpc.StringID("req-1")
			rpcCtx.ProtocolVersion = mcpversions.Version20260728
		}

		return oops.C(oops.CodeInvalid)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestMCPErrHandle_LegacyRevisionKeepsGramStatusesForMandatedCodes is the
// guard on all of the above: none of those statuses may move for the traffic
// in service today.
func TestMCPErrHandle_LegacyRevisionKeepsGramStatusesForMandatedCodes(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	for _, tt := range []struct {
		code oops.Code
		want int
	}{
		{code: oops.CodeNotFound, want: http.StatusNotFound},
		{code: oops.CodeNotImplemented, want: http.StatusNotImplemented},
		{code: oops.CodeInvalid, want: http.StatusUnprocessableEntity},
	} {
		handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
			if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok {
				rpcCtx.ID = mcpjsonrpc.StringID("req-1")
				rpcCtx.ProtocolVersion = mcpversions.Version20251125
			}

			return oops.C(tt.code)
		})

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/test", nil))

		require.Equal(t, tt.want, rec.Code, "code %s", tt.code)
	}
}

func TestNewMissingRequiredClientCapabilityError_MarshalsSpecShape(t *testing.T) {
	t.Parallel()

	mcpErr := oops.NewMissingRequiredClientCapabilityError(mcpjsonrpc.NumberID(7), map[string]json.RawMessage{
		"elicitation": json.RawMessage(`{}`),
	})

	bs, err := json.Marshal(mcpErr)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"jsonrpc": "2.0",
		"id": 7,
		"error": {
			"code": -32021,
			"message": "Missing required client capability",
			"data": {"requiredCapabilities": {"elicitation": {}}}
		}
	}`, string(bs))

	status, ok := mcpErr.Code.MandatedHTTPStatus()
	require.True(t, ok)
	require.Equal(t, http.StatusBadRequest, status)
}

func TestMCPErrHandle_ConnectionReferenceOnlyAddsALine(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="x"`)
		contextvalues.SetConnectionReference(r.Context(), "ref-1")
		return oops.E(oops.CodeUnauthorized, nil, "Reconnect it")
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, `Bearer resource_metadata="x"`, rec.Header().Get("WWW-Authenticate"))
	var body struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, int(oops.MCPCodeUnauthorized), body.Error.Code)
	require.Equal(t, "Reconnect it\nReference: ref-1", body.Error.Message)
}

func TestMCPErrHandle_NoReferenceLeavesTheMessageAlone(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()

	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		return oops.E(oops.CodeUnauthorized, nil, "Reconnect it")
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))

	require.Contains(t, rec.Body.String(), `"message":"Reconnect it"`)
	require.NotContains(t, rec.Body.String(), "Reference")
}
