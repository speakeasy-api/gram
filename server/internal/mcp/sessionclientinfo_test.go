package mcp_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

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

	clientInfoCache := cache.NewTypedObjectCache[mcp.SessionClientInfo](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	info, err := clientInfoCache.Get(ctx, mcp.SessionClientInfoCacheKey(*authCtx.ProjectID, toolset.Slug, sessionID))
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

	clientInfoCache := cache.NewTypedObjectCache[mcp.SessionClientInfo](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	info, err := clientInfoCache.Get(ctx, mcp.SessionClientInfoCacheKey(*authCtx.ProjectID, toolset.Slug, sessionID))
	require.NoError(t, err)
	require.Equal(t, "evilclient"+strings.Repeat("x", 90), info.Name)
	require.Len(t, info.Name, 100)
	require.Equal(t, "1.0", info.Version)
}

// TestServePublic_InitializeWithoutClientInfoCachesNothing keeps the cache free
// of nameless entries so readers can treat a miss and an anonymous client the
// same way.
func TestServePublic_InitializeWithoutClientInfoCachesNothing(t *testing.T) {
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

	clientInfoCache := cache.NewTypedObjectCache[mcp.SessionClientInfo](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	_, err = clientInfoCache.Get(ctx, mcp.SessionClientInfoCacheKey(*authCtx.ProjectID, toolset.Slug, sessionID))
	require.Error(t, err)
}
