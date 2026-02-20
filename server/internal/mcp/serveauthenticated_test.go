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

func TestService_ServeAuthenticated(t *testing.T) {
	t.Parallel()

	t.Run("returns error when project slug is missing", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodPost, "/mcp/project/toolset/environment", nil)
		rctx := chi.NewRouteContext()
		// Not setting project param
		rctx.URLParams.Add("toolset", "test-toolset")
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err := ti.service.ServeAuthenticated(w, req)

		require.Error(t, err)
		require.Contains(t, err.Error(), "project slug must be provided")
	})

	t.Run("returns error when toolset slug is missing", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodPost, "/mcp/project/toolset/environment", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", "test-project")
		// Not setting toolset param
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err := ti.service.ServeAuthenticated(w, req)

		require.Error(t, err)
		require.Contains(t, err.Error(), "toolset slug must be provided")
	})

	t.Run("returns error when environment slug is missing", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodPost, "/mcp/project/toolset/environment", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", "test-project")
		rctx.URLParams.Add("toolset", "test-toolset")
		// Not setting environment param
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err := ti.service.ServeAuthenticated(w, req)

		require.Error(t, err)
		require.Contains(t, err.Error(), "environment slug must be provided")
	})

	t.Run("returns unauthorized without valid API key", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodPost, "/mcp/project/toolset/environment", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", "test-project")
		rctx.URLParams.Add("toolset", "test-toolset")
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
		// No Authorization header

		w := httptest.NewRecorder()
		err := ti.service.ServeAuthenticated(w, req)

		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("handles initialize request with valid API key", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Auth Test MCP",
			Slug:                   "auth-test-mcp",
			Description:            conv.ToPGText("A test MCP for authenticated access"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("auth-test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
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
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+*authCtx.ProjectSlug+"/"+toolset.Slug+"/production", bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", *authCtx.ProjectSlug)
		rctx.URLParams.Add("toolset", toolset.Slug)
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.ServeAuthenticated(w, req)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, w.Code)
		require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))

		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err, "response body: %s", w.Body.String())
		require.Equal(t, "2.0", response["jsonrpc"])
	})

	t.Run("handles ping request", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Ping Test MCP",
			Slug:                   "ping-test-mcp",
			Description:            conv.ToPGText("A test MCP for ping"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("ping-test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "ping",
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+*authCtx.ProjectSlug+"/"+toolset.Slug+"/production", bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", *authCtx.ProjectSlug)
		rctx.URLParams.Add("toolset", toolset.Slug)
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.ServeAuthenticated(w, req)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err, "response body: %s", w.Body.String())
		require.Equal(t, "2.0", response["jsonrpc"])
		// Ping returns a valid response - result field may be omitted for empty results (omitzero tag)
		require.NotNil(t, response["id"])
	})

	t.Run("handles tools/list request", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Tools List Test MCP",
			Slug:                   "tools-list-test-mcp",
			Description:            conv.ToPGText("A test MCP for tools/list"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("tools-list-test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/list",
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+*authCtx.ProjectSlug+"/"+toolset.Slug+"/production", bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", *authCtx.ProjectSlug)
		rctx.URLParams.Add("toolset", toolset.Slug)
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.ServeAuthenticated(w, req)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err, "response body: %s", w.Body.String())
		require.Equal(t, "2.0", response["jsonrpc"])
		// tools/list returns a valid JSON-RPC response
		require.NotNil(t, response["id"], "response should have id field")
	})

	t.Run("handles prompts/list request", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Prompts List Test MCP",
			Slug:                   "prompts-list-test-mcp",
			Description:            conv.ToPGText("A test MCP for prompts/list"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("prompts-list-test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "prompts/list",
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+*authCtx.ProjectSlug+"/"+toolset.Slug+"/production", bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", *authCtx.ProjectSlug)
		rctx.URLParams.Add("toolset", toolset.Slug)
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.ServeAuthenticated(w, req)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err, "response body: %s", w.Body.String())
		require.Equal(t, "2.0", response["jsonrpc"])
		require.NotNil(t, response["result"])

		result, ok := response["result"].(map[string]any)
		require.True(t, ok)
		require.NotNil(t, result["prompts"])
	})

	t.Run("handles resources/list request", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Resources List Test MCP",
			Slug:                   "resources-list-test-mcp",
			Description:            conv.ToPGText("A test MCP for resources/list"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("resources-list-test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "resources/list",
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+*authCtx.ProjectSlug+"/"+toolset.Slug+"/production", bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", *authCtx.ProjectSlug)
		rctx.URLParams.Add("toolset", toolset.Slug)
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.ServeAuthenticated(w, req)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err, "response body: %s", w.Body.String())
		require.Equal(t, "2.0", response["jsonrpc"])
		require.NotNil(t, response["result"])

		result, ok := response["result"].(map[string]any)
		require.True(t, ok)
		require.NotNil(t, result["resources"])
	})

	t.Run("returns method not found for unknown method", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Unknown Method Test MCP",
			Slug:                   "unknown-method-test-mcp",
			Description:            conv.ToPGText("A test MCP for unknown methods"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("unknown-method-test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "unknown/method",
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+*authCtx.ProjectSlug+"/"+toolset.Slug+"/production", bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", *authCtx.ProjectSlug)
		rctx.URLParams.Add("toolset", toolset.Slug)
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.ServeAuthenticated(w, req)

		// Unknown method returns JSON-RPC error in response body, not a Go error
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code)

		// Parse the response (single object for single request in batch)
		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Check that the response contains an error
		require.NotNil(t, response["error"], "expected JSON-RPC error for unknown method")
		errorObj, ok := response["error"].(map[string]any)
		require.True(t, ok)
		require.Contains(t, errorObj["message"], "method does not exist")
	})

	t.Run("handles empty batch gracefully", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Empty Batch Test MCP",
			Slug:                   "empty-batch-test-mcp",
			Description:            conv.ToPGText("A test MCP for empty batch"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("empty-batch-test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		// Empty batch
		reqBody := []map[string]any{}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+*authCtx.ProjectSlug+"/"+toolset.Slug+"/production", bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", *authCtx.ProjectSlug)
		rctx.URLParams.Add("toolset", toolset.Slug)
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.ServeAuthenticated(w, req)
		require.NoError(t, err)

		// Should return 202 Accepted for empty batch
		require.Equal(t, http.StatusAccepted, w.Code)
	})

	t.Run("returns error for non-existent toolset", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+*authCtx.ProjectSlug+"/non-existent-toolset/production", bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", *authCtx.ProjectSlug)
		rctx.URLParams.Add("toolset", "non-existent-toolset")
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.ServeAuthenticated(w, req)

		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("handles notifications/initialized silently", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Notification Test MCP",
			Slug:                   "notification-test-mcp",
			Description:            conv.ToPGText("A test MCP for notifications"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("notification-test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"method":  "notifications/initialized",
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+*authCtx.ProjectSlug+"/"+toolset.Slug+"/production", bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("project", *authCtx.ProjectSlug)
		rctx.URLParams.Add("toolset", toolset.Slug)
		rctx.URLParams.Add("environment", "production")
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.ServeAuthenticated(w, req)
		require.NoError(t, err)

		// Notifications should return 202 Accepted
		require.Equal(t, http.StatusAccepted, w.Code)
	})
}
