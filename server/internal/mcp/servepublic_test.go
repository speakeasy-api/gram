package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestService_ServePublic(t *testing.T) {
	t.Parallel()

	t.Run("handles initialize request successfully", func(t *testing.T) {
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

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"clientInfo": map[string]any{
						"name":    "test-client",
						"version": "1.0.0",
					},
				},
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		mcpSlug := toolset.McpSlug.String
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", mcpSlug)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()

		err = ti.service.ServePublic(w, req)
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
		require.Equal(t, "2024-11-05", result["protocolVersion"])
		require.NotNil(t, result["capabilities"])
		require.NotNil(t, result["serverInfo"])
	})

	t.Run("returns unauthorized for private mcp without authentication", func(t *testing.T) {
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

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.Slug, bytes.NewReader(bodyBytes))

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.Slug)
		unauthCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(unauthCtx)

		w := httptest.NewRecorder()

		err = ti.service.ServePublic(w, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})
}
