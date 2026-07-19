package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

// addHTTPTools creates a deployment + HTTP tool definitions + a single
// toolset_version linking all named tools to the toolset. It returns the
// created tool URNs keyed by name so callers can attach variations to specific
// tools; callers that don't need the URNs may ignore the return.
func addHTTPTools(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID, projectID uuid.UUID, orgID string, toolNames ...string) map[string]urn.Tool {
	t.Helper()

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)

	err = deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	})
	require.NoError(t, err)

	urnsByName := make(map[string]urn.Tool, len(toolNames))
	toolURNs := make([]urn.Tool, len(toolNames))
	for i, toolName := range toolNames {
		toolURN := urn.NewTool(urn.ToolKindHTTP, toolName, uuid.New().String()[:8])
		toolURNs[i] = toolURN
		urnsByName[toolName] = toolURN
		err = tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
			ProjectID:       projectID,
			DeploymentID:    deploymentID,
			ToolUrn:         toolURN,
			Name:            toolName,
			UntruncatedName: pgtype.Text{},
			Summary:         toolName + " summary",
			Description:     toolName + " description",
			Tags:            []string{},
			HttpMethod:      "GET",
			Path:            "/test",
			SchemaVersion:   "3.0.0",
			Schema:          []byte(`{}`),
			ServerEnvVar:    "TEST_SERVER_URL",
			Security:        []byte(`[]`),
			HeaderSettings:  []byte(`{}`),
			QuerySettings:   []byte(`{}`),
			PathSettings:    []byte(`{}`),
			ReadOnlyHint:    pgtype.Bool{},
			DestructiveHint: pgtype.Bool{},
			IdempotentHint:  pgtype.Bool{},
			OpenWorldHint:   pgtype.Bool{},
		})
		require.NoError(t, err)
	}

	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return urnsByName
}

// addHTTPToolsWithSourceTags creates HTTP tools carrying source-defined tags
// (unlike addHTTPTools, which leaves source tags empty) and registers them as a
// single toolset version. It is used to exercise ?tags= filtering driven by
// source tags rather than tool variations. The map key is the tool name and the
// value is its source tags; the returned map associates each name with its URN.
func addHTTPToolsWithSourceTags(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID, projectID uuid.UUID, orgID string, toolTags map[string][]string) map[string]urn.Tool {
	t.Helper()

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)

	err = deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	})
	require.NoError(t, err)

	urnsByName := make(map[string]urn.Tool, len(toolTags))
	toolURNs := make([]urn.Tool, 0, len(toolTags))
	for toolName, tags := range toolTags {
		toolURN := urn.NewTool(urn.ToolKindHTTP, toolName, uuid.New().String()[:8])
		toolURNs = append(toolURNs, toolURN)
		urnsByName[toolName] = toolURN
		err = tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
			ProjectID:       projectID,
			DeploymentID:    deploymentID,
			ToolUrn:         toolURN,
			Name:            toolName,
			UntruncatedName: pgtype.Text{},
			Summary:         toolName + " summary",
			Description:     toolName + " description",
			Tags:            tags,
			HttpMethod:      "GET",
			Path:            "/test",
			SchemaVersion:   "3.0.0",
			Schema:          []byte(`{}`),
			ServerEnvVar:    "TEST_SERVER_URL",
			Security:        []byte(`[]`),
			HeaderSettings:  []byte(`{}`),
			QuerySettings:   []byte(`{}`),
			PathSettings:    []byte(`{}`),
			ReadOnlyHint:    pgtype.Bool{},
			DestructiveHint: pgtype.Bool{},
			IdempotentHint:  pgtype.Bool{},
			OpenWorldHint:   pgtype.Bool{},
		})
		require.NoError(t, err)
	}

	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return urnsByName
}

// ---------------------------------------------------------------------------
// Basic tools/list tests (public MCPs, full HTTP path)
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

	// The synthetic instruction tool is always injected, even for an empty
	// toolset, so an "empty" toolset still lists that one tool.
	resp := parseToolsListResponse(t, w.Body.Bytes())
	require.Equal(t, []string{"instructions"}, toolNames(resp))
}

func TestServePublic_ToolsList_ReturnsAllTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "list-all-"+uuid.NewString()[:8])

	addHTTPTools(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_alpha", "tool_beta")

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	resp := parseToolsListResponse(t, w.Body.Bytes())
	names := toolNames(resp)
	require.Len(t, names, 3)
	require.Contains(t, names, "instructions")
	require.Contains(t, names, "tool_alpha")
	require.Contains(t, names, "tool_beta")
}

func TestServePublic_RBAC_ToolsList_PublicMCPSkipsFiltering(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "list-rbac-pub-"+uuid.NewString()[:8])

	addHTTPTools(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "pub_tool_a", "pub_tool_b")

	// No grants at all — public MCP should still return everything.
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	resp := parseToolsListResponse(t, w.Body.Bytes())
	names := toolNames(resp)
	require.Len(t, names, 3)
	require.Contains(t, names, "instructions")
	require.Contains(t, names, "pub_tool_a")
	require.Contains(t, names, "pub_tool_b")
}

// ---------------------------------------------------------------------------
// RBAC tools/list filtering tests (engine-level, matching rbac_test.go pattern)
//
// Private MCP auth through ServePublic requires a real bearer token (JWT/API
// key/OAuth), and API keys bypass RBAC. Testing RBAC filtering at the engine
// level is the established pattern — see rbac_test.go.
// ---------------------------------------------------------------------------

func TestServePublic_RBAC_ToolsList_FiltersToGrantedTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "list-rbac-filter-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	// Grant mcp:connect only for "allowed_tool".
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   toolset.ID.String(),
			"tool":          "allowed_tool",
		},
	})

	// allowed_tool should pass.
	err = authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
		Tool:        "allowed_tool",
		Disposition: "",
	}))
	require.NoError(t, err)

	// forbidden_tool should be denied.
	err = authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
		Tool:        "forbidden_tool",
		Disposition: "",
	}))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestServePublic_RBAC_ToolsList_ServerLevelGrantReturnsAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "list-rbac-all-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	// Server-level grant (no tool dimension) — all tools allowed.
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, toolset.ID.String()),
	})

	// Any tool should pass with a server-level grant.
	err = authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
		Tool:        "tool_one",
		Disposition: "",
	}))
	require.NoError(t, err)

	err = authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
		Tool:        "tool_two",
		Disposition: "",
	}))
	require.NoError(t, err)
}

func TestServePublic_RBAC_ToolsList_NoGrantsDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "list-rbac-empty-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	// mcp:connect grant for a DIFFERENT toolset — should not match.
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, uuid.NewString()),
	})

	err = authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
		Tool:        "tool_x",
		Disposition: "",
	}))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestServePublic_RBAC_ToolsList_MultipleToolGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "list-rbac-multi-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

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

	// tool_a and tool_c pass.
	require.NoError(t, authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{Tool: "tool_a", Disposition: ""})))
	require.NoError(t, authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{Tool: "tool_c", Disposition: ""})))

	// tool_b denied.
	err = authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{Tool: "tool_b", Disposition: ""}))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// ---------------------------------------------------------------------------
// Disposition-based RBAC filtering (engine-level)
// ---------------------------------------------------------------------------

func TestServePublic_RBAC_ToolsList_DispositionGrant_AllowsMatchingDisposition(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-disp-allow-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	// Grant mcp:connect scoped to read_only disposition only.
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   toolset.ID.String(),
			"disposition":   "read_only",
		},
	})

	// read_only tool should pass.
	err = authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
		Tool:        "safe_tool",
		Disposition: "read_only",
	}))
	require.NoError(t, err)

	// destructive tool should be denied.
	err = authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
		Tool:        "dangerous_tool",
		Disposition: "destructive",
	}))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestServePublic_RBAC_ToolsList_DisabledRBACAllowsAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "list-rbac-off-"+uuid.NewString()[:8])

	// Engine with RBAC disabled — simulates org without RBAC feature flag.
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysDisabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	// No grants in context at all. With RBAC disabled, every tool should pass.
	for _, tool := range []string{"tool_one", "tool_two", "tool_three"} {
		err := authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
			Tool:        tool,
			Disposition: "",
		}))
		require.NoError(t, err, "tool %q should be allowed when RBAC is disabled", tool)
	}
}

func TestServePublic_RBAC_ToolsList_DispositionGrant_ServerLevelAllowsAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-disp-server-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	// Server-level grant (no disposition key) — all dispositions allowed.
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, toolset.ID.String()),
	})

	err = authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
		Tool:        "any_tool",
		Disposition: "destructive",
	}))
	require.NoError(t, err)
}

func TestServePublic_ToolsList_NoTagsFilter_ReturnsAllWithVariationNames(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupTagFilterToolset(t, ctx, ti)

	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "", makeToolsListBody())
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	// Variation-renamed names are exposed on the wire; the untagged tool keeps
	// its original name. The synthetic instruction tool is always present.
	require.ElementsMatch(t, []string{"instructions", "alpha_renamed", "beta_renamed", "tool_gamma"}, names)
}

func TestServePublic_ToolsList_TagsFilter_SingleTag(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupTagFilterToolset(t, ctx, ti)

	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "tags=alpha", makeToolsListBody())
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	// Only the alpha-tagged tool; beta and the variation-less gamma are
	// excluded. The synthetic instruction tool bypasses tag filtering and is
	// always prepended.
	require.Equal(t, []string{"instructions", "alpha_renamed"}, names)
}

func TestServePublic_ToolsList_TagsFilter_Union(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupTagFilterToolset(t, ctx, ti)

	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "tags=alpha,beta", makeToolsListBody())
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	require.ElementsMatch(t, []string{"instructions", "alpha_renamed", "beta_renamed"}, names)
}

func TestServePublic_ToolsList_TagsFilter_SharedTagMatchesBoth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupTagFilterToolset(t, ctx, ti)

	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "tags=shared", makeToolsListBody())
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	require.ElementsMatch(t, []string{"instructions", "alpha_renamed", "beta_renamed"}, names)
}

func TestServePublic_ToolsList_TagsFilter_NonexistentReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupTagFilterToolset(t, ctx, ti)

	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "tags=does-not-exist", makeToolsListBody())
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// An all-nonexistent filter must be a successful empty list, not a
	// JSON-RPC error (which would also be HTTP 200 with no tools).
	var rpcResp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rpcResp))
	require.Nil(t, rpcResp.Error, "expected empty tools list, got JSON-RPC error: %s", w.Body.String())

	// The synthetic instruction tool bypasses tag filtering, so it is the only
	// tool left when a filter matches nothing.
	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	require.Equal(t, []string{"instructions"}, names)
}
