package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServePublic_ResourcesTemplatesList_ReturnsEmptyList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "res-tmpl-list-"+uuid.NewString()[:8])

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "resources/templates/list",
	})
	require.NoError(t, err)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), "response body: %s", w.Body.String())
	require.Nil(t, response["error"], "resources/templates/list must not return method-not-found")
	require.Equal(t, "2.0", response["jsonrpc"])
	require.InDelta(t, 4, response["id"], 0)

	result, ok := response["result"].(map[string]any)
	require.True(t, ok, "result should be a map: %v", response)

	templates, ok := result["resourceTemplates"].([]any)
	require.True(t, ok, "resourceTemplates should be an array: %v", result)
	require.Empty(t, templates)

	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	requireCacheHints(t, envelope["result"], "public")
}

// The hosted list operations that are not filtered per caller resolve their
// cache scope from server visibility and whether the caller authenticated.
// These two exercise the public branch end to end, which needs a request
// carrying no auth context at all: a caller inside the owning organization is
// treated as authenticated whether or not it presents a token, so passing the
// fixture context here would exercise the private branch instead. It is also
// the one combination whose two operands differ, so a transposed derivation
// surfaces here as "private" rather than passing silently.
func TestServePublic_PromptsList_PublicUnauthenticatedIsCacheablePublicly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "prompts-scope-"+uuid.NewString()[:8])

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "prompts/list",
	})
	require.NoError(t, err)

	w, err := servePublicHTTP(t, t.Context(), ti, toolset.McpSlug.String, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	requireCacheHints(t, envelope["result"], "public")
}

func TestServePublic_ResourcesList_PublicUnauthenticatedIsCacheablePublicly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "resources-scope-"+uuid.NewString()[:8])

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "resources/list",
	})
	require.NoError(t, err)

	w, err := servePublicHTTP(t, t.Context(), ti, toolset.McpSlug.String, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	requireCacheHints(t, envelope["result"], "public")
}
