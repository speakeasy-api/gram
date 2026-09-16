package mcp_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// mcpErrorBody is the JSON-RPC error a refused request comes back as. The
// refusal is carried in a 200 response body, not an HTTP status: handleRequest
// errors are rendered by writeMCPError, which is the protocol's own channel
// for this.
type mcpErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func requireMCPError(t *testing.T, w *httptest.ResponseRecorder) mcpErrorBody {
	t.Helper()

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var body struct {
		Error *mcpErrorBody `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "body=%s", w.Body.String())
	require.NotNil(t, body.Error, "expected a JSON-RPC error; body=%s", w.Body.String())
	return *body.Error
}

// Cursor publishes no client ID metadata document, so the pre-authentication
// layer can never see it: there is no verified credential naming it. Most of
// the catalogue is in that position, and before the session layer existed a
// block recorded against any of them did nothing at all.
func TestServePublic_BlockedToolReportingItsName_Refused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "session-block-cursor")
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")

	body := makeInitializeBodyWithClientInfo(t, "Cursor", "1.2.3")
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, body, "", nil)
	require.NoError(t, err)

	rpcErr := requireMCPError(t, w)
	require.Equal(t, int(oops.MCPCodeForbidden), rpcErr.Code)
	// The same sentence the OAuth layer uses. One decision, one explanation,
	// whichever layer turned the caller away.
	require.Contains(t, rpcErr.Message, "Cursor")
	require.Contains(t, rpcErr.Message, "administrator must approve")
}

// Case is not a credential, and neither is punctuation the client chose. A
// tool that capitalizes its name differently is the same tool.
func TestServePublic_BlockedToolNameMatchIgnoresCase(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "session-block-case")
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBodyWithClientInfo(t, "CURSOR", "1.0.0"), "", nil)
	require.NoError(t, err)
	require.Equal(t, int(oops.MCPCodeForbidden), requireMCPError(t, w).Code)
}

// A decision about one tool must not touch another, which is as true of this
// layer as of the verified one.
func TestServePublic_UnblockedToolReportingItsName_Serves(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "session-block-other")
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBodyWithClientInfo(t, "claude-code", "1.0.0"), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "initialize response: %s", w.Body.String())
}

// A client reporting a name nobody claims is not swept into someone else's
// decision. Unrecognized stays unreviewed, which is what it was before.
func TestServePublic_UnrecognizedClientName_Serves(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "session-block-unknown")
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBodyWithClientInfo(t, "some-internal-tool", "1.0.0"), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "initialize response: %s", w.Body.String())
}

// The layer runs on every request, not only the handshake, so a session that
// started before a block was recorded is refused on its next call rather than
// running until it happens to reconnect.
func TestServePublic_BlockRecordedMidSession_RefusesTheNextRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "session-block-midsession")

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBodyWithClientInfo(t, "Cursor", "1.2.3"), "", nil)
	require.NoError(t, err, "nothing is blocked yet, so the handshake succeeds")
	require.Equal(t, http.StatusOK, w.Code)

	sessionID := w.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")

	// The client reports nothing this time: it handshaked, so its identity
	// comes from the record that handshake left behind.
	listBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	require.NoError(t, err)

	w2, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, listBody, "", map[string]string{"Mcp-Session-Id": sessionID})
	require.NoError(t, err)
	require.Equal(t, int(oops.MCPCodeForbidden), requireMCPError(t, w2).Code,
		"a block must reach a session already in flight")
}

// Under the stateless model a client repeats its identity on every request and
// may never handshake at all. Checking only initialize would leave that whole
// path unenforced.
func TestServePublic_StatelessClientNameInMeta_Refused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "session-block-stateless")
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")

	listBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params": map[string]any{
			"_meta": map[string]any{
				"io.modelcontextprotocol/clientInfo": map[string]any{"name": "Cursor", "version": "1.2.3"},
			},
		},
	})
	require.NoError(t, err)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, listBody, "", nil)
	require.NoError(t, err)
	require.Equal(t, int(oops.MCPCodeForbidden), requireMCPError(t, w).Code,
		"a client that never handshakes must still be refused")
}

// Failing to learn whether anything is blocked allows the request. Refusing
// every MCP client in an organization that has never used the feature is a
// worse outage than the one it would prevent, and it matches the verified
// layer's answer to the same question.
func TestServePublic_BlockedIDsReadFails_AllowsTheRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "session-block-readfail")
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")
	ti.service.FailAIToolBlockedIDsRead(errors.New("blocked ids unavailable"))

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBodyWithClientInfo(t, "Cursor", "1.2.3"), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "initialize response: %s", w.Body.String())
}

// Once a block is known to be in force, not knowing which target the caller is
// means the answer is unknown rather than absent, so the request is refused as
// retryable instead of quietly bypassing the control.
func TestServePublic_CatalogReadFailsWhileBlocked_RefusedAsRetryable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "session-block-catalogfail")
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")
	ti.service.FailAIToolCatalogRead(errors.New("catalog unavailable"))

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBodyWithClientInfo(t, "Cursor", "1.2.3"), "", nil)
	require.NoError(t, err)
	// Refused, which is the part that matters. The handshake-era JSON-RPC
	// codes have nothing for "retryable", so this arrives as a server error
	// rather than carrying the 503 the OAuth layer can express.
	require.Equal(t, int(oops.MCPCodeInternalError), requireMCPError(t, w).Code)
}
