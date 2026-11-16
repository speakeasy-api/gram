package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type failingRequestBody struct {
	err error
}

func (b failingRequestBody) Read([]byte) (int, error) {
	return 0, b.err
}

func (failingRequestBody) Close() error {
	return nil
}

func TestRejectMissingMCPTarget_ValidatesRequest(t *testing.T) {
	t.Parallel()

	const body = `{"jsonrpc":"2.0","id":0,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	for _, tc := range []struct {
		name    string
		body    string
		version string
		method  string
		status  int
		code    oops.MCPCode
		id      string
	}{
		{name: "zero ID", body: body, version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeInvalidParams, id: "0"},
		{name: "string ID", body: strings.Replace(body, `"id":0`, `"id":"request-1"`, 1), version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeInvalidParams, id: `"request-1"`},
		{name: "malformed JSON", body: "{", version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeParseError},
		{name: "empty body", version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeParseError},
		{name: "invalid envelope", body: strings.Replace(body, `"jsonrpc":"2.0"`, `"jsonrpc":"1.0"`, 1), version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeInvalidRequest, id: "0"},
		{name: "batch", body: "[" + body + "]", version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeInvalidRequest},
		{name: "missing method header", body: body, version: mcpversions.Version20260728, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch, id: "0"},
		{name: "missing version header with modern _meta", body: body, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch, id: "0"},
		{name: "notification with missing version header and modern _meta", body: strings.Replace(body, `"id":0,`, "", 1), method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch},
		{name: "absent declarations", body: `{"jsonrpc":"2.0","id":0,"method":"tools/list"}`, method: mcpversions.MethodToolsList, status: http.StatusNotFound, code: oops.MCPCodeResourceNotFound, id: "null"},
		{name: "absent declarations and malformed body", body: "{", method: mcpversions.MethodToolsList, status: http.StatusNotFound, code: oops.MCPCodeResourceNotFound, id: "null"},
		{name: "absent declarations and oversized body", body: strings.Repeat(" ", missingMCPTargetMaxBodyBytes+1), method: mcpversions.MethodToolsList, status: http.StatusNotFound, code: oops.MCPCodeResourceNotFound, id: "null"},
		{name: "mismatched method", body: body, version: mcpversions.Version20260728, method: mcpversions.MethodToolsCall, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch, id: "0"},
		{name: "mismatched revision", body: strings.Replace(body, mcpversions.Version20260728, mcpversions.Version20251125, 1), version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch, id: "0"},
		{name: "malformed version header", body: body, version: "2026-\t07-28", method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch, id: "0"},
		{name: "non-ASCII version header", body: body, version: "2026-☃-28", method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch, id: "0"},
		{name: "blank version header", body: body, version: " \t ", method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch, id: "0"},
		{name: "notification with mismatched method", body: strings.Replace(body, `"id":0,`, "", 1), version: mcpversions.Version20260728, method: mcpversions.MethodToolsCall, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch},
		{name: "malformed version header without _meta declaration", body: `{"jsonrpc":"2.0","id":0,"method":"tools/list"}`, version: "2026-\t07-28", method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch, id: "0"},
		{name: "notification with malformed version header without _meta declaration", body: `{"jsonrpc":"2.0","method":"notifications/initialized"}`, version: "2026-\t07-28", method: mcpversions.MethodNotificationsInitialized, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch},
		{name: "unsupported initialize", body: strings.ReplaceAll(strings.Replace(body, "tools/list", "initialize", 1), mcpversions.Version20260728, "2031-01-01"), version: "2031-01-01", method: mcpversions.MethodInitialize, status: http.StatusBadRequest, code: oops.MCPCodeUnsupportedProtocolVersion, id: "0"},
		{name: "unsupported header conflicting with _meta", body: body, version: "2031-01-01", method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeHeaderMismatch, id: "0"},
		{name: "oversized body", body: strings.Repeat(" ", missingMCPTargetMaxBodyBytes+1), version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusRequestEntityTooLarge, code: oops.MCPCodeInternalError},
		{name: "null ID", body: strings.Replace(body, `"id":0`, `"id":null`, 1), version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeInvalidRequest},
		{name: "missing method", body: strings.Replace(body, `"method":"tools/list",`, "", 1), version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeInvalidRequest, id: "0"},
		{name: "readable ID with invalid method type", body: `{"jsonrpc":"2.0","id":"r1","method":42}`, version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeInvalidRequest, id: `"r1"`},
		{name: "unreadable ID", body: `{"jsonrpc":"2.0","id":{},"method":"tools/list"}`, version: mcpversions.Version20260728, method: mcpversions.MethodToolsList, status: http.StatusBadRequest, code: oops.MCPCodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logger := testenv.NewLogger(t)
			handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
				return rejectMissingMCPTarget(w, r, logger, mcpversions.SupportedHostedToolset(), oops.C(oops.CodeNotFound))
			})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/missing-target", strings.NewReader(tc.body))
			if tc.version != "" {
				req.Header.Set(mcpversions.HTTPHeader, tc.version)
			}
			req.Header.Set(httpheaders.MethodHeader, tc.method)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			require.Equal(t, tc.status, w.Code, "body=%s", w.Body.String())
			var response struct {
				ID    json.RawMessage `json:"id"`
				Error struct {
					Code oops.MCPCode `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(t, tc.id, string(response.ID))
			require.Equal(t, tc.code, response.Error.Code)
		})
	}
}

func TestPreparedMCPRequest_RecoversIDFromInvalidEnvelope(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		id   mcpjsonrpc.ID
	}{
		{name: "ID before invalid method", body: `{"jsonrpc":"2.0","id":"r1","method":42}`, id: mcpjsonrpc.StringID("r1")},
		{name: "ID after invalid method", body: `{"jsonrpc":"2.0","method":42,"id":"r1"}`, id: mcpjsonrpc.StringID("r1")},
		{name: "zero ID with invalid version type", body: `{"jsonrpc":2,"id":0,"method":"tools/list"}`, id: mcpjsonrpc.NumberID(0)},
		{name: "empty string ID", body: `{"jsonrpc":"2.0","id":"","method":42}`, id: mcpjsonrpc.StringID("")},
		{name: "absent ID", body: `{"jsonrpc":"2.0","method":42}`},
		{name: "invalid ID", body: `{"jsonrpc":"2.0","id":{},"method":"tools/list"}`},
		{name: "duplicate invalid ID", body: `{"jsonrpc":"2.0","id":"r1","id":{},"method":"tools/list"}`},
		{name: "malformed JSON after ID", body: `{"jsonrpc":"2.0","id":"r1","method":`},
		{name: "batch", body: `[{"jsonrpc":"2.0","id":"r1","method":"tools/list"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rpcCtx := &contextvalues.RPCContext{ID: mcpjsonrpc.NullID()}
			ctx := contextvalues.SetRPCContext(t.Context(), rpcCtx)
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/test", strings.NewReader(tc.body))
			req.Header.Set(mcpversions.HTTPHeader, mcpversions.Version20260728)
			prepared := prepareMCPRequest(httptest.NewRecorder(), req, missingMCPTargetMaxBodyBytes, mcpversions.SupportedHostedToolset())

			require.NoError(t, prepared.bodyReadErr)
			require.Error(t, prepared.bodyDecodeErr)
			require.False(t, prepared.readyForProtocolVersionValidation(), "recovering the ID must not accept an invalid envelope")
			require.Equal(t, tc.id, prepared.request.ID)
			if tc.id.IsSet() {
				require.Equal(t, tc.id, rpcCtx.ID)
			} else {
				require.True(t, rpcCtx.ID.IsNull(), "an unreadable ID must not be copied into the response context")
			}
		})
	}
}

func TestRejectMissingMCPTarget_BodyReadFailure(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	handler := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		return rejectMissingMCPTarget(w, r, logger, mcpversions.SupportedHostedToolset(), oops.C(oops.CodeNotFound))
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/missing-target", nil)
	req.Header.Set(mcpversions.HTTPHeader, mcpversions.Version20260728)
	req.Body = failingRequestBody{err: errors.New("read failed")}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var response map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotContains(t, response, "id")
}

func TestRejectMissingMCPTarget_HandshakeDoesNotReadBody(t *testing.T) {
	t.Parallel()

	for _, version := range []string{mcpversions.Version20241105, mcpversions.Version20250326, mcpversions.Version20250618, mcpversions.Version20251125} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/missing-target", nil)
			req.Header.Set(mcpversions.HTTPHeader, version)
			body := strings.NewReader("unread request")
			req.Body = io.NopCloser(body)
			cause := oops.C(oops.CodeNotFound)
			err := rejectMissingMCPTarget(httptest.NewRecorder(), req, testenv.NewLogger(t), mcpversions.SupportedHostedToolset(), cause)
			require.Same(t, cause, err)
			require.Equal(t, len("unread request"), body.Len())
		})
	}
}

func TestPreparedMCPRequest_ZeroByteReadErrorIsNotEmpty(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")
	req := httptest.NewRequest(http.MethodPost, "/mcp/test", nil)
	req.Body = failingRequestBody{err: readErr}
	prepared := prepareMCPRequest(httptest.NewRecorder(), req, 1<<20, mcpversions.SupportedHostedToolset())

	require.False(t, prepared.empty())
	err := validateMCPRequestEnvelope(t.Context(), testenv.NewLogger(t), prepared, oops.CodeBadRequest, "body too large")
	require.Error(t, err)
	require.ErrorIs(t, err, readErr)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
}

func TestValidateMCPRequestEnvelope_MalformedArrayIsParseError(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/mcp/test", strings.NewReader("["))
	prepared := prepareMCPRequest(httptest.NewRecorder(), req, 1<<20, mcpversions.SupportedHostedToolset())

	err := validateMCPRequestEnvelope(t.Context(), testenv.NewLogger(t), prepared, oops.CodeBadRequest, "body too large")
	require.Error(t, err)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeParseError, shareable.Code)
}
