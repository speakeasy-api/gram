package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestMCPErrorHTTPStatus_ModernRevisionUsesMandatedStatuses covers the table
// MCP 2026-07-28 mandates. The 404 is the one with a rationale beyond
// tidiness: the JSON-RPC body accompanying it is what separates a modern
// server that does not implement the method from a legacy server that does not
// host the modern endpoint at all.
func TestMCPErrorHTTPStatus_ModernRevisionUsesMandatedStatuses(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		code oops.MCPCode
		want int
	}{
		{code: oops.MCPCodeMethodNotFound, want: http.StatusNotFound},
		{code: oops.MCPCodeInvalidParams, want: http.StatusBadRequest},
		{code: oops.MCPCodeHeaderMismatch, want: http.StatusBadRequest},
		{code: oops.MCPCodeMissingRequiredClientCapability, want: http.StatusBadRequest},
		{code: oops.MCPCodeUnsupportedProtocolVersion, want: http.StatusBadRequest},
	} {
		require.Equal(t, tt.want, mcpErrorHTTPStatus(tt.code, mcpversions.Version20260728), "code %d", tt.code)
	}
}

// TestMCPErrorHTTPStatus_LegacyRevisionsKeep200 pins the invariant that keeps
// today's clients working. Under the handshake-based revisions a JSON-RPC
// error is carried by a successful response, and clients that treat a non-2xx
// as a transport failure never reach the body at all.
func TestMCPErrorHTTPStatus_LegacyRevisionsKeep200(t *testing.T) {
	t.Parallel()

	for _, revision := range []string{
		mcpversions.Version20241105,
		mcpversions.Version20250326,
		mcpversions.Version20250618,
		mcpversions.Version20251125,
	} {
		require.Equal(t, http.StatusOK, mcpErrorHTTPStatus(oops.MCPCodeMethodNotFound, revision), "revision %s", revision)
		require.Equal(t, http.StatusOK, mcpErrorHTTPStatus(oops.MCPCodeInvalidParams, revision), "revision %s", revision)
	}
}

// TestMCPErrorHTTPStatus_UnresolvedRevisionKeeps200 covers the errors raised
// before a revision can be resolved. They must fall to the legacy side: a
// request whose revision is unknown is not evidence of a modern client.
func TestMCPErrorHTTPStatus_UnresolvedRevisionKeeps200(t *testing.T) {
	t.Parallel()

	require.Equal(t, http.StatusOK, mcpErrorHTTPStatus(oops.MCPCodeMethodNotFound, ""))
	require.Equal(t, http.StatusOK, mcpErrorHTTPStatus(oops.MCPCodeMethodNotFound, "not-a-revision"))
}

func TestMCPErrorHTTPStatus_UnsupportedVersionAlwaysUsesBadRequest(t *testing.T) {
	t.Parallel()

	for _, revision := range []string{
		"",
		mcpversions.Version20241105,
		mcpversions.Version20250326,
		mcpversions.Version20250618,
		mcpversions.Version20251125,
		mcpversions.Version20260728,
		"2031-01-01",
	} {
		require.Equal(t, http.StatusBadRequest, mcpErrorHTTPStatus(oops.MCPCodeUnsupportedProtocolVersion, revision), "revision %q", revision)
	}
}

// TestMCPErrorHTTPStatus_UnmandatedCodesKeep200OnModernRevision guards the
// scope of the change. The specification assigns a status to five conditions
// and says nothing about the rest, which is most of what Gram emits; giving
// those a status of our choosing would move traffic on a guess.
func TestMCPErrorHTTPStatus_UnmandatedCodesKeep200OnModernRevision(t *testing.T) {
	t.Parallel()

	for _, code := range []oops.MCPCode{
		oops.MCPCodeParseError,
		oops.MCPCodeInvalidRequest,
		oops.MCPCodeInternalError,
		oops.MCPCodeServerError,
		oops.MCPCodeUnauthorized,
		oops.MCPCodeForbidden,
	} {
		require.Equal(t, http.StatusOK, mcpErrorHTTPStatus(code, mcpversions.Version20260728), "code %d", code)
	}
}

// TestMCPErrorHTTPStatus_RetiredNotFoundBecomesBadRequestOnModernRevision
// covers the two revision-conditional rules meeting on one error. Gram's
// general not-found — an unknown tool, toolset, or endpoint — maps to -32602
// under 2026-07-28, and that code is answered with 400 rather than the 404 the
// internal error code alone would suggest.
func TestMCPErrorHTTPStatus_RetiredNotFoundBecomesBadRequestOnModernRevision(t *testing.T) {
	t.Parallel()

	code := oops.CodeNotFound.MCPCodeFor(mcpversions.Version20260728)

	require.Equal(t, oops.MCPCodeInvalidParams, code)
	require.Equal(t, http.StatusBadRequest, mcpErrorHTTPStatus(code, mcpversions.Version20260728))
}

// TestWriteMCPError_WritesStatusCodeAndBodyTogether covers the wiring rather
// than the table: that the status actually reaches the response, and that it
// was chosen for the same error the body carries. A helper that computed the
// right status and wrote a different one would satisfy every other test here.
func TestWriteMCPError_WritesStatusCodeAndBodyTogether(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := writeMCPError(t.Context(), testenv.NewLogger(t), rec, mcpjsonrpc.StringID("req-1"),
		mcpversions.Version20260728, oops.E(oops.CodeNotImplemented, nil, "tools/nope: Method not found"))
	require.NoError(t, err)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var response struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Error   struct {
			Code oops.MCPCode `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "2.0", response.JSONRPC)
	require.Equal(t, "req-1", response.ID)
	require.Equal(t, oops.MCPCodeMethodNotFound, response.Error.Code)
}

func TestWriteMCPError_UnsupportedVersionMatchesSpecificationShape(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	supported := []string{mcpversions.Version20250326, mcpversions.Version20251125}
	err := writeMCPError(
		t.Context(),
		testenv.NewLogger(t),
		rec,
		mcpjsonrpc.StringID("req-unsupported"),
		mcpversions.DefaultInEffect,
		unsupportedProtocolVersionError(mcpjsonrpc.StringID("req-unsupported"), mcpversions.Version20260728, supported),
	)
	require.NoError(t, err)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `{
		"jsonrpc":"2.0",
		"id":"req-unsupported",
		"error":{
			"code":-32022,
			"message":"Unsupported protocol version",
			"data":{
				"supported":["2025-03-26","2025-11-25"],
				"requested":"2026-07-28"
			}
		}
	}`, rec.Body.String())
}

// TestWriteMCPError_LegacyRevisionWritesOKWithTheSameBody is the invariant that
// protects today's traffic: identical JSON-RPC error, HTTP 200.
func TestWriteMCPError_LegacyRevisionWritesOKWithTheSameBody(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := writeMCPError(t.Context(), testenv.NewLogger(t), rec, mcpjsonrpc.StringID("req-1"),
		mcpversions.Version20251125, oops.E(oops.CodeNotImplemented, nil, "tools/nope: Method not found"))
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Error struct {
			Code oops.MCPCode `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, oops.MCPCodeMethodNotFound, response.Error.Code)
}

// TestWriteMCPError_NotificationsGetTheMandatedStatusToo records a deliberate
// choice. A message carrying no id gets the same status as one that does: the
// mandated status describes the condition, not the message that provoked it,
// and 2026-07-28 directs a server that cannot accept input to answer with an
// HTTP error status rather than a 200 carrying an error body. Answering
// notifications differently here would also diverge from the wrapper that
// handles errors escaping a handler, which has no 200 to fall back to.
func TestWriteMCPError_NotificationsGetTheMandatedStatusToo(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := writeMCPError(t.Context(), testenv.NewLogger(t), rec, mcpjsonrpc.ID{},
		mcpversions.Version20260728, oops.E(oops.CodeNotImplemented, nil, "notifications/nope: Method not found"))
	require.NoError(t, err)

	require.Equal(t, http.StatusNotFound, rec.Code)

	var response map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Nil(t, response["id"], "a notification is still answered with a null id")
}

// TestWriteMCPError_NotificationsKeep200OnLegacyRevisions is the other half:
// nothing about notification handling changes for the revisions in service
// today.
func TestWriteMCPError_NotificationsKeep200OnLegacyRevisions(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := writeMCPError(t.Context(), testenv.NewLogger(t), rec, mcpjsonrpc.ID{},
		mcpversions.Version20251125, oops.E(oops.CodeNotImplemented, nil, "notifications/nope: Method not found"))
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)
}
