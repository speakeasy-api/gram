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

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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
		ToolsetID:                toolset.ID,
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
