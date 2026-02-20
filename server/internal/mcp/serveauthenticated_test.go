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
	templates_repo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

	t.Run("handles prompts/get with valid prompt", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)
		tplRepo := templates_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)
		require.NotNil(t, authCtx.ProjectSlug)

		// Create a toolset
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Prompts Get Test MCP",
			Slug:                   "prompts-get-test-mcp",
			Description:            conv.ToPGText("A test MCP for prompts/get"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("prompts-get-test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create a template (prompt)
		_, err = tplRepo.CreateTemplate(ctx, templates_repo.CreateTemplateParams{
			ProjectID:   *authCtx.ProjectID,
			ToolUrn:     urn.NewTool(urn.ToolKindPrompt, "test-source", "test-prompt"),
			Name:        "test-prompt",
			Prompt:      "Hello, {{name}}!",
			Description: conv.ToPGText("A test prompt"),
			Arguments:   []byte(`{"type":"object","properties":{"name":{"type":"string"}}}`),
			Engine:      pgtype.Text{String: "mustache", Valid: true},
			Kind:        pgtype.Text{String: "prompt", Valid: true},
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "prompts/get",
				"params": map[string]any{
					"name":      "test-prompt",
					"arguments": map[string]any{"name": "World"},
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

		// Parse and validate response
		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotNil(t, response["result"])
	})

	t.Run("returns error for prompts/get with missing name", func(t *testing.T) {
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
			Name:                   "Prompts Get Missing Name MCP",
			Slug:                   "prompts-get-missing-name-mcp",
			Description:            conv.ToPGText("A test MCP"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("prompts-get-missing-name-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "prompts/get",
				"params":  map[string]any{},
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

		// Should return JSON-RPC error
		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotNil(t, response["error"])
	})

	t.Run("returns error for prompts/get with nonexistent prompt", func(t *testing.T) {
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
			Name:                   "Prompts Get Nonexistent MCP",
			Slug:                   "prompts-get-nonexistent-mcp",
			Description:            conv.ToPGText("A test MCP"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("prompts-get-nonexistent-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "prompts/get",
				"params": map[string]any{
					"name": "nonexistent-prompt",
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

		// Should return JSON-RPC error for not found
		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotNil(t, response["error"])
	})

	t.Run("returns error for resources/read with missing URI", func(t *testing.T) {
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
			Name:                   "Resources Read Missing URI MCP",
			Slug:                   "resources-read-missing-uri-mcp",
			Description:            conv.ToPGText("A test MCP"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("resources-read-missing-uri-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "resources/read",
				"params":  map[string]any{},
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

		// Should return JSON-RPC error
		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotNil(t, response["error"])
	})

	t.Run("returns error for resources/read with nonexistent resource", func(t *testing.T) {
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
			Name:                   "Resources Read Nonexistent MCP",
			Slug:                   "resources-read-nonexistent-mcp",
			Description:            conv.ToPGText("A test MCP"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("resources-read-nonexistent-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "resources/read",
				"params": map[string]any{
					"uri": "nonexistent://resource",
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

		// Should return JSON-RPC error for not found
		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotNil(t, response["error"])
	})

	t.Run("returns error for tools/call with missing name", func(t *testing.T) {
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
			Name:                   "Tools Call Missing Name MCP",
			Slug:                   "tools-call-missing-name-mcp",
			Description:            conv.ToPGText("A test MCP"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("tools-call-missing-name-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params":  map[string]any{},
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

		// Should return JSON-RPC error
		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotNil(t, response["error"])
	})

	t.Run("returns error for tools/call with nonexistent tool", func(t *testing.T) {
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
			Name:                   "Tools Call Nonexistent MCP",
			Slug:                   "tools-call-nonexistent-mcp",
			Description:            conv.ToPGText("A test MCP"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("tools-call-nonexistent-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params": map[string]any{
					"name":      "nonexistent-tool",
					"arguments": map[string]any{},
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

		// Should return JSON-RPC error for not found
		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotNil(t, response["error"])
	})

	t.Run("handles multiple requests in batch", func(t *testing.T) {
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
			Name:                   "Batch Request MCP",
			Slug:                   "batch-request-mcp",
			Description:            conv.ToPGText("A test MCP for batch requests"),
			DefaultEnvironmentSlug: pgtype.Text{String: "production", Valid: true},
			McpSlug:                conv.ToPGText("batch-request-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		// Send multiple requests in a batch
		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "ping",
			},
			{
				"jsonrpc": "2.0",
				"id":      2,
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

		// Parse the batch response (array of responses)
		var responses []map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &responses)
		require.NoError(t, err)
		require.Len(t, responses, 2)

		// Both responses should have id fields
		require.NotNil(t, responses[0]["id"])
		require.NotNil(t, responses[1]["id"])
	})
}
