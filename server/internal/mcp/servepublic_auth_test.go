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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

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

// makeToolsListBody creates a single JSON-RPC tools/list request body.
func makeToolsListBody() []byte {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
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
) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
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

func TestServePublicAuth_PublicNoOAuth_NoToken_InitializeSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "pub-no-oauth-init")

	unauthCtx := context.Background()
	w, err := servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "2.0", response["jsonrpc"])
}

func TestServePublicAuth_PublicNoOAuth_NoToken_ToolsListSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "pub-no-oauth-list")

	unauthCtx := context.Background()

	// Initialize first
	w, err := servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	// tools/list should succeed — no security variables means no auth required
	w, err = servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeToolsListBody(), "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServePublicAuth_PrivateServer_NoToken_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private Auth Test MCP",
		Slug:                   "priv-auth-test",
		Description:            conv.ToPGText("A private MCP for auth testing"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("priv-auth-test"),
		McpEnabled:             true,
		// McpIsPublic defaults to false
	})
	require.NoError(t, err)

	unauthCtx := context.Background()
	_, err = servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired or invalid access token")
}

func TestServePublicAuth_BatchRequest_Rejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "pub-batch-reject")

	// Send a batch (array) request — should be rejected
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
	_, err = servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, bodyBytes, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "batch requests are not supported")
}
