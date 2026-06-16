// servepublic_test.go contains shared test helpers for all servepublic_*_test.go
// files, plus general-purpose ServePublic tests that aren't about auth/OAuth.
package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	variations_repo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// makeInitializeBody creates a single JSON-RPC initialize request body.
func makeInitializeBody() []byte {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	bs, _ := json.Marshal(reqBody)
	return bs
}

// servePublicHTTP creates an HTTP request and calls ServePublic, returning the recorder and error.
func servePublicHTTP(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	mcpSlug string,
	body []byte,
	authToken string,
	extraHeaders map[string]string,
) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	if err := ti.service.ServePublic(w, req); err != nil {
		return w, fmt.Errorf("serve public: %w", err)
	}
	return w, nil
}

// createPublicMCPToolset creates a public MCP toolset for testing.
func createPublicMCPToolset(
	t *testing.T,
	ctx context.Context,
	toolsetsRepo *toolsets_repo.Queries,
	authCtx *contextvalues.AuthContext,
	slug string,
) toolsets_repo.Toolset {
	t.Helper()

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test Public MCP " + slug,
		Slug:                   slug,
		Description:            conv.ToPGText("A test public MCP for auth testing"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	return toolset
}

// ---------------------------------------------------------------------------
// General-purpose ServePublic tests (non-auth)
// ---------------------------------------------------------------------------

func TestServePublic_InitializeSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test MCP Server",
		Slug:                   "test-mcp",
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("test-mcp"),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	w, err := servePublicHTTP(t, ctx, ti, mcpSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))

	var response map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "response body: %s", w.Body.String())
	require.Equal(t, "2.0", response["jsonrpc"])
	require.InDelta(t, 1, response["id"], 0)
	require.NotNil(t, response["result"])

	result, ok := response["result"].(map[string]any)
	require.True(t, ok, "result should be a map")
	require.Equal(t, "2025-03-26", result["protocolVersion"])
	require.NotNil(t, result["capabilities"])
	require.NotNil(t, result["serverInfo"])
}

// TestServePublic_InitializeWithMalformedParamsSucceeds verifies the commit's
// guarantee that capturing client info never breaks the RPC: even when the
// initialize params can't be decoded into our recorded shape, the handshake
// still returns a valid initialize result.
func TestServePublic_InitializeWithMalformedParamsSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Malformed Params MCP",
		Slug:                   "malformed-params-mcp",
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("malformed-params-mcp"),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	// params is an array rather than the expected object, so decoding into our
	// recorded shape fails — but the RPC must still succeed.
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":["unexpected"]}`)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))

	var response map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "response body: %s", w.Body.String())
	require.Nil(t, response["error"])
	result, ok := response["result"].(map[string]any)
	require.True(t, ok, "result should be a map: %v", response)
	require.Equal(t, "2025-03-26", result["protocolVersion"])
}

func TestServePublic_PrivateDisabledMCP_Returns404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private MCP Server",
		Slug:                   "private-mcp",
		Description:            conv.ToPGText("A private MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	_, err = servePublicHTTP(t, context.Background(), ti, toolset.Slug, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestServePublic_AttachedAuthErrorReturnsMCPError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private Auth Error MCP",
		Slug:                   "private-auth-error-mcp",
		Description:            conv.ToPGText("A private MCP that rejects unauthenticated requests"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("private-auth-error-mcp"),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service, nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String, bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.NotContains(t, w.Body.String(), "<!doctype")

	var response map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "response body: %s", w.Body.String())
	require.Equal(t, "2.0", response["jsonrpc"])
	require.InDelta(t, 1, response["id"], 0)

	errorBody, ok := response["error"].(map[string]any)
	require.True(t, ok, "expected JSON-RPC error: %v", response)
	require.InDelta(t, -32001, errorBody["code"], 0)
	require.Contains(t, errorBody["message"], "expired or invalid access token")
}

func TestServePublic_AttachedNotFoundReturnsMCPErrorWithNullID(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPService(t)

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service, nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp/bad-slug", bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.NotContains(t, w.Body.String(), "<!doctype")

	var response map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "response body: %s", w.Body.String())
	require.Equal(t, "2.0", response["jsonrpc"])
	require.Nil(t, response["id"])

	errorBody, ok := response["error"].(map[string]any)
	require.True(t, ok, "expected JSON-RPC error: %v", response)
	require.InDelta(t, -32002, errorBody["code"], 0)
	require.Equal(t, "mcp server not found", errorBody["message"])
}

func TestServePublic_ServerInstructionsInInitializeResponse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)
	metadataRepo := metadata_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test MCP Server with Instructions",
		Slug:                   "test-mcp-instructions",
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("test-mcp-instructions"),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	instructions := "You have tools for searching the Test Hub. Use them wisely."
	_, err = metadataRepo.UpsertMetadata(ctx, metadata_repo.UpsertMetadataParams{
		ToolsetID:                uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:                *authCtx.ProjectID,
		ExternalDocumentationUrl: pgtype.Text{String: "", Valid: false},
		LogoID:                   uuid.NullUUID{Valid: false},
		Instructions:             conv.ToPGText(instructions),
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	w, err := servePublicHTTP(t, ctx, ti, mcpSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "response body: %s", w.Body.String())

	result, ok := response["result"].(map[string]any)
	require.True(t, ok, "result should be a map")
	require.Equal(t, "2025-03-26", result["protocolVersion"])
	require.NotNil(t, result["capabilities"])
	require.NotNil(t, result["serverInfo"])
	require.Equal(t, instructions, result["instructions"])
}

func TestServePublic_BatchRequestRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "pub-batch-reject")

	batchBody := []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
		},
	}
	bodyBytes, err := json.Marshal(batchBody)
	require.NoError(t, err)

	unauthCtx := context.Background()
	_, err = servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, bodyBytes, "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "batch requests are not supported")
}

// servePublicToolsRequest issues a POST to /mcp/{slug} with an optional raw
// query string (e.g. "tags=alpha,beta") and returns the recorder. Unlike
// servePublicHTTP it threads a query string onto the URL so ?tags= filtering
// can be exercised through the full serve path.
func servePublicToolsRequest(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug, rawQuery string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	target := "/mcp/" + mcpSlug
	if rawQuery != "" {
		target += "?" + rawQuery
	}

	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(w, req))
	return w
}

func makeToolsCallBody(toolName string) []byte {
	bs, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": map[string]any{},
		},
	})
	return bs
}

// setupTagFilterToolset builds a public MCP toolset with three HTTP tools and a
// project-default variation group. tool_alpha and tool_beta carry tagged,
// renamed variations; tool_gamma has no variation at all. It returns the MCP
// slug. The toolset has no tool_variations_group_id, so the project-default
// group applies — exercising the "?tags= filters against the project default"
// behavior.
func setupTagFilterToolset(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "tag-filter-"+uuid.New().String()[:8])

	urns := addHTTPTools(t, ctx, ti, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID,
		"tool_alpha", "tool_beta", "tool_gamma")

	variationsRepo := variations_repo.New(ti.conn)
	group, err := variationsRepo.InitGlobalToolVariationsGroup(ctx, variations_repo.InitGlobalToolVariationsGroupParams{
		ProjectID:   *authCtx.ProjectID,
		Name:        "default-group",
		Description: conv.ToPGText("default group"),
	})
	require.NoError(t, err)

	_, err = variationsRepo.UpsertToolVariation(ctx, variations_repo.UpsertToolVariationParams{
		GroupID:     group,
		SrcToolUrn:  urns["tool_alpha"],
		SrcToolName: "tool_alpha",
		Name:        conv.ToPGText("alpha_renamed"),
		Tags:        []string{"alpha", "shared"},
	})
	require.NoError(t, err)

	_, err = variationsRepo.UpsertToolVariation(ctx, variations_repo.UpsertToolVariationParams{
		GroupID:     group,
		SrcToolUrn:  urns["tool_beta"],
		SrcToolName: "tool_beta",
		Name:        conv.ToPGText("beta_renamed"),
		Tags:        []string{"beta", "shared"},
	})
	require.NoError(t, err)

	return toolset.McpSlug.String
}

// setupSourceTagFilterToolset builds a public MCP toolset whose tools carry
// source-defined tags, exercising the rule that a variation is not required for
// ?tags= filtering. Three tools cover the three tag states:
//
//   - source_only: source tags ["billing"], no variation — filterable by its
//     source tag with no variation row at all.
//   - reporting_renamed: source tags ["reporting"], a variation that renames it
//     but does not modify tags (nil) — source tags stay authoritative.
//   - removed: source tags ["billing"], a variation with an explicit empty tag
//     set — removed from every tag filter even though its source tag matches.
//
// It returns the MCP slug and the renamed/removed source names for assertions.
func setupSourceTagFilterToolset(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "src-tag-filter-"+uuid.New().String()[:8])

	urns := addHTTPToolsWithSourceTags(t, ctx, ti, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID,
		map[string][]string{
			"source_only": {"billing"},
			"reporting":   {"reporting"},
			"removed":     {"billing"},
		})

	variationsRepo := variations_repo.New(ti.conn)
	group, err := variationsRepo.InitGlobalToolVariationsGroup(ctx, variations_repo.InitGlobalToolVariationsGroupParams{
		ProjectID:   *authCtx.ProjectID,
		Name:        "default-group",
		Description: conv.ToPGText("default group"),
	})
	require.NoError(t, err)

	// A variation that renames but leaves tags unset (nil) — source tags remain
	// authoritative for filtering.
	_, err = variationsRepo.UpsertToolVariation(ctx, variations_repo.UpsertToolVariationParams{
		GroupID:     group,
		SrcToolUrn:  urns["reporting"],
		SrcToolName: "reporting",
		Name:        conv.ToPGText("reporting_renamed"),
		Tags:        nil,
	})
	require.NoError(t, err)

	// A variation with an explicit empty tag set — removes the tool from every
	// tag filter despite its source "billing" tag.
	_, err = variationsRepo.UpsertToolVariation(ctx, variations_repo.UpsertToolVariationParams{
		GroupID:     group,
		SrcToolUrn:  urns["removed"],
		SrcToolName: "removed",
		Tags:        []string{},
	})
	require.NoError(t, err)

	return toolset.McpSlug.String
}

func TestServePublic_ToolsList_SourceTags_NoVariationRequired(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupSourceTagFilterToolset(t, ctx, ti)

	// source_only has no variation at all, yet its source tag drives filtering.
	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "tags=billing", makeToolsListBody())
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	// removed also has source tag "billing" but its empty variation tag set takes
	// it out of every filter, so only source_only remains.
	require.Equal(t, []string{"source_only"}, names)
}

func TestServePublic_ToolsList_SourceTags_NilVariationFallsBackToSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupSourceTagFilterToolset(t, ctx, ti)

	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "tags=reporting", makeToolsListBody())
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	// The renaming variation leaves tags unset, so the source "reporting" tag
	// still matches; the wire name reflects the variation rename.
	require.Equal(t, []string{"reporting_renamed"}, names)
}

func TestServePublic_ToolsList_EmptyVariationTags_RemovesFromAllFilters(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupSourceTagFilterToolset(t, ctx, ti)

	// With no ?tags= filter, every tool is returned — removed is present.
	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "", makeToolsListBody())
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	all := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	require.ElementsMatch(t, []string{"source_only", "reporting_renamed", "removed"}, all)

	// Under a filter matching its source tag, the empty variation tag set keeps
	// removed out of the results entirely.
	w = servePublicToolsRequest(t, ctx, ti, mcpSlug, "tags=billing", makeToolsListBody())
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	filtered := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	require.NotContains(t, filtered, "removed")
}

func TestServePublic_ToolsCall_EmptyVariationTags_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupSourceTagFilterToolset(t, ctx, ti)

	// removed is excluded by its empty variation tag set, so calling it under a
	// filter matching its source tag must resolve to method-not-found.
	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "tags=billing", makeToolsCallBody("removed"))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body: %s", w.Body.String())
	require.NotNil(t, resp.Error, "expected a JSON-RPC error, body: %s", w.Body.String())
	require.Contains(t, resp.Error.Message, "not found")
}

func TestServePublic_ToolsCall_FilteredOutTool_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := setupTagFilterToolset(t, ctx, ti)

	// alpha_renamed exists but is excluded by the beta-only filter, so the call
	// must resolve to method-not-found rather than executing.
	w := servePublicToolsRequest(t, ctx, ti, mcpSlug, "tags=beta", makeToolsCallBody("alpha_renamed"))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body: %s", w.Body.String())
	require.NotNil(t, resp.Error, "expected a JSON-RPC error, body: %s", w.Body.String())
	require.Contains(t, resp.Error.Message, "not found")
}
