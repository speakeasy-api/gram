package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestToolsCall_ShutdownInterruptAnswersTheClientAndRecordsTheCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fixture := seedHostedTool(t, ctx, ti, "list_repos")

	// The tool's server takes the call and never answers, so only the drain's
	// interrupt can end it.
	reached := make(chan struct{})
	release := make(chan struct{})
	held := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(reached)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(held.Close)
	// Runs before held.Close (LIFO) so a handler still holding its response
	// does not block shutdown.
	t.Cleanup(func() { close(release) })

	type served struct {
		response *httptest.ResponseRecorder
		err      error
	}
	done := make(chan served, 1)
	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "list_repos", "arguments": map[string]any{}})
	go func() {
		response, err := servePublicHTTP(t, ctx, ti, fixture.slug, body, "", map[string]string{
			"Mcp-Test-Server-Url": held.URL,
			"Mcp-Session-Id":      "session-1",
		})
		done <- served{response: response, err: err}
	}()

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the tool's server to receive the call")
	}

	ti.service.StartDrain(0)

	var result served
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the interrupted call to be answered")
	}
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.response.Code, "the JSON-RPC error travels in an HTTP 200")

	envelope := decodeRPCResponse(t, result.response)
	require.Contains(t, envelope, "error", "body=%s", result.response.Body.String())
	var rpcErr struct {
		Code oops.MCPCode `json:"code"`
		Data struct {
			Reason    string `json:"reason"`
			Retryable bool   `json:"retryable"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(envelope["error"], &rpcErr))
	require.Equal(t, oops.MCPCodeInternalError, rpcErr.Code)
	require.Equal(t, string(oops.MCPErrorDataReasonServerShutdown), rpcErr.Data.Reason)
	require.False(t, rpcErr.Data.Retryable, "a tool call may already have had side effects")

	record := awaitToolCallRecord(t, ti)
	require.Equal(t, int64(http.StatusServiceUnavailable), toolCallRecordAttribute(record, "http.response.status_code").GetIntValue())
	require.Equal(t, string(oops.CodeUnavailable), toolCallRecordAttribute(record, "error.type").GetStringValue())
	require.Equal(t, string(oops.MCPErrorDataReasonServerShutdown), toolCallRecordAttribute(record, "gram.tool_call.interrupt_reason").GetStringValue())
}
