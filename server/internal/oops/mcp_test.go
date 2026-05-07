package oops

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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

	require.Equal(t, http.StatusBadRequest, rec.Code)
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

	require.Equal(t, http.StatusBadRequest, rec.Code)

	var response map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Contains(t, response, "id")
	require.Nil(t, response["id"])
	require.Empty(t, logBuf.String())
}

func TestMCPCode_HTTPStatusAndMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code    MCPCode
		status  int
		message string
	}{
		{MCPCodeParseError, http.StatusBadRequest, "Parse error"},
		{MCPCodeInvalidRequest, http.StatusBadRequest, "Invalid Request"},
		{MCPCodeMethodNotFound, http.StatusNotFound, "Method not found"},
		{MCPCodeInvalidParams, http.StatusBadRequest, "Invalid params"},
		{MCPCodeInternalError, http.StatusInternalServerError, "Internal error"},
		{MCPCodeServerError, http.StatusInternalServerError, "Server error"},
		{MCPCodeResourceNotFound, http.StatusNotFound, "Resource not found"},
		{MCPCode(-1), http.StatusInternalServerError, "Internal error"},
	}

	for _, tt := range tests {
		require.Equal(t, tt.status, tt.code.HTTPStatus())
		require.Equal(t, tt.message, tt.code.Message())
	}
}

func TestMCPError_MarshalJSON(t *testing.T) {
	t.Parallel()

	err := &MCPError{
		ID:      mcpjsonrpc.NumberID(1),
		Code:    MCPCodeMethodNotFound,
		Message: "tools/unknown: Method not found",
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
}

func TestNewMCPErrorFromCause(t *testing.T) {
	t.Parallel()

	id := mcpjsonrpc.StringID("req-1")

	t.Run("returns_existing_mcp_error", func(t *testing.T) {
		t.Parallel()

		existing := &MCPError{Code: MCPCodeMethodNotFound, Message: "missing"}
		err := NewMCPErrorFromCause(id, existing)

		require.Same(t, existing, err)
		require.Equal(t, id, err.ID)
	})

	t.Run("maps_shareable_error_code", func(t *testing.T) {
		t.Parallel()

		err := NewMCPErrorFromCause(id, E(CodeNotFound, nil, "mcp server not found"))

		require.Equal(t, id, err.ID)
		require.Equal(t, MCPCodeResourceNotFound, err.Code)
		require.Equal(t, "mcp server not found", err.Message)
	})

	t.Run("defaults_unknown_error_to_internal", func(t *testing.T) {
		t.Parallel()

		err := NewMCPErrorFromCause(id, errors.New("boom"))

		require.Equal(t, id, err.ID)
		require.Equal(t, MCPCodeInternalError, err.Code)
		require.Equal(t, "Internal error", err.Message)
	})
}
