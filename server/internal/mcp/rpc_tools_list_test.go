package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeToolsListBody creates a JSON-RPC tools/list request body.
func makeToolsListBody() []byte {
	bs, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	return bs
}

// toolsListResponse is the parsed JSON-RPC tools/list response.
type toolsListResponse struct {
	Result struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	} `json:"result"`
}

// parseToolsListResponse unmarshals a tools/list response and returns it.
func parseToolsListResponse(t *testing.T, body []byte) toolsListResponse {
	t.Helper()
	var resp toolsListResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

// toolNames extracts tool names from the response for easy assertion.
func toolNames(resp toolsListResponse) []string {
	names := make([]string, len(resp.Result.Tools))
	for i, t := range resp.Result.Tools {
		names[i] = t.Name
	}
	return names
}

// addHTTPTool creates a deployment + HTTP tool definition + toolset_version
// linking the named tool to the toolset. No security scheme.
func addHTTPTool(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID, projectID uuid.UUID, orgID, toolName string) {
	t.Helper()

	var deploymentID uuid.UUID
	err := ti.conn.QueryRow(ctx, `
		INSERT INTO deployments (project_id, organization_id, user_id, idempotency_key)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, projectID, orgID, "test-user", uuid.New().String()).Scan(&deploymentID)
	require.NoError(t, err)

	_, err = ti.conn.Exec(ctx, `
		INSERT INTO deployment_statuses (deployment_id, status)
		VALUES ($1, 'completed')
	`, deploymentID)
	require.NoError(t, err)

	toolURN := "tools:http:" + toolName + ":" + uuid.New().String()[:8]
	_, err = ti.conn.Exec(ctx, `
		INSERT INTO http_tool_definitions (
			project_id, deployment_id, tool_urn, name, untruncated_name,
			summary, description, tags, http_method, path,
			schema_version, schema, server_env_var, security,
			header_settings, query_settings, path_settings
		) VALUES (
			$1, $2, $3, $4, '', $5, $6,
			'{}', 'GET', '/test', '3.0.0', '{}', 'TEST_SERVER_URL',
			'[]', '{}', '{}', '{}'
		)
	`, projectID, deploymentID, toolURN, toolName, toolName+" summary", toolName+" description")
	require.NoError(t, err)

	_, err = ti.conn.Exec(ctx, `
		INSERT INTO toolset_versions (toolset_id, version, tool_urns, resource_urns)
		VALUES ($1, (SELECT COALESCE(MAX(version), 0) + 1 FROM toolset_versions WHERE toolset_id = $1), $2, '{}')
	`, toolsetID, []string{toolURN})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Basic tools/list tests
// ---------------------------------------------------------------------------

func TestServePublic_ToolsList_ReturnsEmptyForEmptyToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "list-empty-"+uuid.NewString()[:8])

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	resp := parseToolsListResponse(t, w.Body.Bytes())
	require.Empty(t, resp.Result.Tools)
}

func TestServePublic_ToolsList_ReturnsAllTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "list-all-"+uuid.NewString()[:8])

	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_alpha")
	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_beta")

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	resp := parseToolsListResponse(t, w.Body.Bytes())
	names := toolNames(resp)
	require.Len(t, names, 2)
	require.Contains(t, names, "tool_alpha")
	require.Contains(t, names, "tool_beta")
}

// ---------------------------------------------------------------------------
// RBAC tools/list filtering tests
// ---------------------------------------------------------------------------

func TestServePublic_RBAC_ToolsList_FiltersToGrantedTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "list-rbac-filter-"+uuid.NewString()[:8])

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "allowed_tool")
	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "forbidden_tool")

	// Grant mcp:connect only for "allowed_tool".
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   toolset.ID.String(),
			"tool":          "allowed_tool",
		},
	})

	sessionToken := ti.getSessionToken(ctx, t)
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), sessionToken, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	resp := parseToolsListResponse(t, w.Body.Bytes())
	names := toolNames(resp)
	require.Equal(t, []string{"allowed_tool"}, names, "only the granted tool should appear")
}

func TestServePublic_RBAC_ToolsList_ServerLevelGrantReturnsAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "list-rbac-all-"+uuid.NewString()[:8])

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_one")
	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_two")

	// Server-level grant (no tool dimension) — all tools allowed.
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, toolset.ID.String()),
	})

	sessionToken := ti.getSessionToken(ctx, t)
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), sessionToken, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	resp := parseToolsListResponse(t, w.Body.Bytes())
	names := toolNames(resp)
	require.Len(t, names, 2)
	require.Contains(t, names, "tool_one")
	require.Contains(t, names, "tool_two")
}

func TestServePublic_RBAC_ToolsList_NoGrantsDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "list-rbac-empty-"+uuid.NewString()[:8])

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_x")

	// mcp:connect grant for a DIFFERENT toolset — should not match.
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, uuid.NewString()),
	})

	sessionToken := ti.getSessionToken(ctx, t)
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), sessionToken, nil)
	// Connection-level RBAC denies before tools/list runs.
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServePublic_RBAC_ToolsList_PublicMCPSkipsFiltering(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "list-rbac-pub-"+uuid.NewString()[:8])

	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "pub_tool_a")
	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "pub_tool_b")

	// No grants at all — public MCP should still return everything.
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	resp := parseToolsListResponse(t, w.Body.Bytes())
	names := toolNames(resp)
	require.Len(t, names, 2)
	require.Contains(t, names, "pub_tool_a")
	require.Contains(t, names, "pub_tool_b")
}

func TestServePublic_RBAC_ToolsList_MultipleToolGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "list-rbac-multi-"+uuid.NewString()[:8])

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_a")
	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_b")
	addHTTPTool(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_c")

	// Grant access to tool_a and tool_c but not tool_b.
	ctx = authztest.WithExactGrants(t, ctx,
		authz.Grant{
			Scope: authz.ScopeMCPConnect,
			Selector: authz.Selector{
				"resource_kind": "mcp",
				"resource_id":   toolset.ID.String(),
				"tool":          "tool_a",
			},
		},
		authz.Grant{
			Scope: authz.ScopeMCPConnect,
			Selector: authz.Selector{
				"resource_kind": "mcp",
				"resource_id":   toolset.ID.String(),
				"tool":          "tool_c",
			},
		},
	)

	sessionToken := ti.getSessionToken(ctx, t)
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), sessionToken, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	resp := parseToolsListResponse(t, w.Body.Bytes())
	names := toolNames(resp)
	require.Len(t, names, 2)
	require.Contains(t, names, "tool_a")
	require.Contains(t, names, "tool_c")
	require.NotContains(t, names, "tool_b")
}
