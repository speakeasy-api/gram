package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

const toolSearchUnavailableMessage = "tool search is temporarily unavailable; try again later"

func TestMCPServesStaticRequestsAndFailsFastOnStaleDynamicIndex(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "stale-index-"+uuid.NewString()[:8])
	addHTTPTools(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "static_tool")

	initialize, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, initialize.Code)

	staticList, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"static_tool"}, toolNames(parseToolsListResponse(t, staticList.Body.Bytes())))

	dynamicList, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", map[string]string{"Gram-Mode": "dynamic"})
	require.NoError(t, err)
	requireMCPToolSearchUnavailable(t, dynamicList)

	dynamicSearch, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsCallBody("search_tools"), "", map[string]string{"Gram-Mode": "dynamic"})
	require.NoError(t, err)
	requireMCPToolSearchUnavailable(t, dynamicSearch)
}

func requireMCPToolSearchUnavailable(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Error struct {
			Code    oops.MCPCode `json:"code"`
			Message string       `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, oops.MCPCodeInternalError, response.Error.Code)
	require.Equal(t, toolSearchUnavailableMessage, response.Error.Message)
}
