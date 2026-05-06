package oops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestMCPErrHandle_IncludesMCPID(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()

	handler := MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		contextvalues.SetMCPID(r.Context(), json.RawMessage(`"req-1"`))
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
