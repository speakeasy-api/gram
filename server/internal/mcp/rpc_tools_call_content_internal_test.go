package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/stretchr/testify/require"
)

func TestHTTPToolCallResultContentContract(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name        string
		status      int
		body        string
		contentType string
		want        string
	}{
		{"empty success", http.StatusOK, "", "", `{"content":[]}`},
		{"no content", http.StatusNoContent, "", "", `{"content":[]}`},
		{"text success", http.StatusOK, "healthy", "text/plain", `{"content":[{"type":"text","text":"healthy","_meta":{"getgram.ai/mime-type":"text/plain"}}]}`},
		{"structured success", http.StatusOK, `{"healthy":true}`, "application/json", `{"content":[{"type":"text","text":"{\"healthy\":true}","_meta":{"getgram.ai/mime-type":"application/json"}}],"structuredContent":{"healthy":true}}`},
		{"text error", http.StatusBadRequest, "invalid request", "text/plain", `{"content":[{"type":"text","text":"invalid request","_meta":{"getgram.ai/mime-type":"text/plain"}}],"isError":true}`},
		{"empty error", http.StatusInternalServerError, "", "", `{"content":[],"isError":true}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rw := toolCallResponseWriter{statusCode: tt.status, headers: make(http.Header), body: bytes.NewBufferString(tt.body)}
			rw.headers.Set("Content-Type", tt.contentType)
			content, structured, err := formatResult(rw, gateway.ToolKindHTTP)
			require.NoError(t, err)
			resultBody := toolCallResult{Content: content, StructuredContent: structured, IsError: rw.statusCode < 200 || rw.statusCode >= 300}
			body, err := json.Marshal(resultBody)
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(body))

			wire, err := json.Marshal(result[toolCallResult]{ID: mcpjsonrpc.NumberID(1), Result: resultBody})
			require.NoError(t, err)
			var envelope struct {
				JSONRPC string                     `json:"jsonrpc"`
				Result  map[string]json.RawMessage `json:"result"`
			}
			require.NoError(t, json.Unmarshal(wire, &envelope))
			require.Equal(t, "2.0", envelope.JSONRPC)
			// MCP CallToolResult.content is a required array of ContentBlock objects.
			// rmcp rejects [null] as CallToolResult and falls back to another result
			// variant, so a successful HTTP response still fails the client's call.
			raw, ok := envelope.Result["content"]
			require.True(t, ok)
			require.NotEqual(t, "null", string(raw))
			var blocks []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &blocks))
			require.NotNil(t, blocks)
			for _, block := range blocks {
				require.NotNil(t, block)
				require.Contains(t, block, "type")
			}
		})
	}
}
