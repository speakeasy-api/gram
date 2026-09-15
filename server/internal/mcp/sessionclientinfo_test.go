package mcp_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/sessionclientinfo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// newClientInfoStore reads the same records the server writes, so these tests
// assert against real Redis rather than the service's own view of it.
func newClientInfoStore(t *testing.T) *sessionclientinfo.Store {
	t.Helper()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	return sessionclientinfo.NewStore(client, 0)
}

// makeInitializeBodyWithClientInfo builds an initialize request reporting a
// specific client identity.
func makeInitializeBodyWithClientInfo(t *testing.T, name, version string) []byte {
	t.Helper()

	bs, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": name, "version": version},
		},
	})
	require.NoError(t, err)

	return bs
}

func TestServePublic_InitializeCachesClientInfo(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "client-info-capture")

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "initialize response: %s", w.Body.String())

	sessionID := w.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	info, err := newClientInfoStore(t).Load(ctx, *authCtx.ProjectID, toolset.Slug, sessionID, 0)
	require.NoError(t, err, "the handshake identity must outlive initialize; tool calls have no other source for it")
	require.Equal(t, "test-client", info.Name)
	require.Equal(t, "1.0.0", info.Version)
}

// TestServePublic_InitializeBoundsCachedClientInfo covers the untrusted nature
// of clientInfo: the values are self-reported by the caller and end up in a
// function tool-call payload, so they must be stripped of control characters
// and capped before they are stored.
func TestServePublic_InitializeBoundsCachedClientInfo(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "client-info-bounds")

	body := makeInitializeBodyWithClientInfo(t, "ev\x00il\nclient"+strings.Repeat("x", 200), "1.\t0")
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "initialize response: %s", w.Body.String())

	sessionID := w.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	info, err := newClientInfoStore(t).Load(ctx, *authCtx.ProjectID, toolset.Slug, sessionID, 0)
	require.NoError(t, err)
	require.Equal(t, "evilclient"+strings.Repeat("x", 90), info.Name)
	require.Len(t, info.Name, 100)
	require.Equal(t, "1.0", info.Version)
}

// TestServePublic_InitializeWithoutClientInfoCachesProtocolVersionOnly covers a
// client that reports a protocol version but no name. The record it leaves
// carries the version and nothing else, keeping the session attributable to a
// protocol generation while still resolving to an anonymous identity — readers
// of the identity treat that the same as a cache miss.
func TestServePublic_InitializeWithoutClientInfoCachesProtocolVersionOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "client-info-absent")

	body := makeInitializeBodyWithClientInfo(t, "", "")
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "initialize response: %s", w.Body.String())

	sessionID := w.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	info, err := newClientInfoStore(t).Load(ctx, *authCtx.ProjectID, toolset.Slug, sessionID, 0)
	require.NoError(t, err)
	require.Empty(t, info.Name)
	require.Empty(t, info.Version)
	require.Equal(t, mcpversions.Version20250326, info.ProtocolVersion)
}
