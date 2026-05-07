package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcp_repo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newMockExternalMCPServer creates an httptest server that speaks the MCP protocol
// via the selected transport, delegating to the shared testmcp package so the
// same mock is used by tests across the codebase.
func newMockExternalMCPServer(t *testing.T, transportType externalmcp_types.TransportType, tools []testmcp.Tool) *httptest.Server {
	t.Helper()

	server := &testmcp.Server{
		Tools: tools,
	}

	switch transportType {
	case externalmcp_types.TransportTypeSSE:
		return testmcp.NewSSEServer(t, server)
	case externalmcp_types.TransportTypeStreamableHTTP:
		return testmcp.NewStreamableHTTPServer(t, server)
	default:
		t.Fatalf("unsupported transport type: %s", transportType)
		return nil
	}
}

// externalMCPConfig contains configuration for setting up external MCP in tests
type externalMCPConfig struct {
	toolset      toolsets_repo.Toolset
	deploymentID uuid.UUID
	attachmentID uuid.UUID
	toolDefID    uuid.UUID
	toolURN      string
	slug         string
}

// setupToolsetWithExternalMCP creates all necessary database records for testing external MCP proxy
func setupToolsetWithExternalMCP(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	mockServerURL string,
	transportType externalmcp_types.TransportType,
	slug string,
) *externalMCPConfig {
	t.Helper()

	toolsetsRepo := toolsets_repo.New(ti.conn)
	externalmcpRepo := externalmcp_repo.New(ti.conn)

	// Get auth context for project/org IDs
	projectID, orgID := getTestProjectAndOrg(t, ctx, ti)

	// Create toolset with MCP enabled
	mcpSlug := "external-mcp-" + slug
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         orgID,
		ProjectID:              projectID,
		Name:                   "External MCP Test Toolset",
		Slug:                   slug,
		Description:            conv.ToPGText("Toolset for testing external MCP proxy"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(mcpSlug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make the toolset public
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

	registryID, err := externalmcpRepo.CreateMCPRegistry(ctx, externalmcp_repo.CreateMCPRegistryParams{
		Name: "test-registry-" + slug,
		Url:  mockServerURL,
	})
	require.NoError(t, err)

	// Create external MCP attachment
	attachment, err := externalmcpRepo.CreateExternalMCPAttachment(ctx, externalmcp_repo.CreateExternalMCPAttachmentParams{
		DeploymentID:            deploymentID,
		RegistryID:              uuid.NullUUID{UUID: registryID, Valid: true},
		Name:                    "External MCP Server",
		Slug:                    slug,
		RegistryServerSpecifier: "test-server",
	})
	require.NoError(t, err)

	// Create tool definition with the external MCP tool URN
	toolURNString := "tools:externalmcp:" + slug + ":proxy"
	toolDef, err := externalmcpRepo.CreateExternalMCPToolDefinition(ctx, externalmcp_repo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachment.ID,
		ToolUrn:                    toolURNString,
		Type:                       "proxy",
		RemoteUrl:                  mockServerURL,
		TransportType:              transportType,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
	})
	require.NoError(t, err)

	// Create toolset version with the tool URN
	toolURN, err := urn.ParseTool(toolURNString)
	require.NoError(t, err)
	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{Valid: false},
	})
	require.NoError(t, err)

	return &externalMCPConfig{
		toolset:      toolset,
		deploymentID: deploymentID,
		attachmentID: attachment.ID,
		toolDefID:    toolDef.ID,
		toolURN:      toolURNString,
		slug:         slug,
	}
}

// getTestProjectAndOrg extracts project and org IDs from the auth context
// populated by testenv.InitAuthContext.
func getTestProjectAndOrg(t *testing.T, ctx context.Context, _ *testInstance) (uuid.UUID, string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context should be populated by testenv.InitAuthContext")
	require.NotNil(t, authCtx.ProjectID, "auth context should carry a project ID")

	return *authCtx.ProjectID, authCtx.ActiveOrganizationID
}

// sendMCPRequest sends an MCP request to the service and returns the response
func sendMCPRequest(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	mcpSlug string,
	request map[string]any,
) *httptest.ResponseRecorder {
	t.Helper()

	bodyBytes, err := json.Marshal(request)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	reqCtx := context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	err = ti.service.ServePublic(w, req)
	if err != nil {
		t.Logf("ServePublic error: %v", err)
	}

	return w
}

// TestE2E_ExternalMCP_Proxy_StreamableHTTP tests the full proxy flow with StreamableHTTP transport:
// MCP Client -> Gram -> External MCP Server (StreamableHTTP)
func TestE2E_ExternalMCP_Proxy_StreamableHTTP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	// Create mock external MCP server
	mockTools := []testmcp.Tool{
		{
			Name:        "get_weather",
			Description: "Get current weather for a location",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []any{"location"},
			},
			Response: testmcp.ToolResponse{
				Content: []map[string]any{
					{
						"type": "text",
						"text": "The weather in San Francisco is sunny and 72°F",
					},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	// Set up toolset with external MCP configuration
	config := setupToolsetWithExternalMCP(t, ctx, ti, mockServer.URL, externalmcp_types.TransportTypeStreamableHTTP, "weather-http")

	// Step 1: Initialize
	initResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, map[string]any{
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
	})
	require.Equal(t, http.StatusOK, initResp.Code, "initialize failed: %s", initResp.Body.String())

	// Step 2: List tools - should include external tools with prefix
	listResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	require.Equal(t, http.StatusOK, listResp.Code, "tools/list failed: %s", listResp.Body.String())

	var listResult map[string]any
	err := json.Unmarshal(listResp.Body.Bytes(), &listResult)
	require.NoError(t, err)

	result, ok := listResult["result"].(map[string]any)
	require.True(t, ok)
	tools, ok := result["tools"].([]any)
	require.True(t, ok)
	require.GreaterOrEqual(t, len(tools), 1, "expected at least 1 tool")

	// Find the external tool (should be prefixed with slug)
	var foundTool map[string]any
	expectedToolName := config.slug + "--get_weather"
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		require.True(t, ok, "expected tool to be a map")
		if toolMap["name"] == expectedToolName {
			foundTool = toolMap
			break
		}
	}
	require.NotNil(t, foundTool, "expected to find tool %s in tools list", expectedToolName)
	require.Equal(t, "Get current weather for a location", foundTool["description"])

	// Step 3: Call the external tool
	callResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": expectedToolName,
			"arguments": map[string]any{
				"location": "San Francisco",
			},
		},
	})
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call failed: %s", callResp.Body.String())

	var callResult map[string]any
	err = json.Unmarshal(callResp.Body.Bytes(), &callResult)
	require.NoError(t, err)

	callResultData, ok := callResult["result"].(map[string]any)
	require.True(t, ok, "expected result in response: %v", callResult)
	content, ok := callResultData["content"].([]any)
	require.True(t, ok, "expected content array in result")
	require.Len(t, content, 1)

	firstContent, ok := content[0].(map[string]any)
	require.True(t, ok, "expected content item to be a map")
	require.Equal(t, "text", firstContent["type"])
	require.Equal(t, "The weather in San Francisco is sunny and 72°F", firstContent["text"])
}

// TestE2E_ExternalMCP_Proxy_SSE tests the full proxy flow with SSE transport:
// MCP Client -> Gram -> External MCP Server (SSE)
func TestE2E_ExternalMCP_Proxy_SSE(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	// Create mock external MCP server with SSE transport
	mockTools := []testmcp.Tool{
		{
			Name:        "calculate",
			Description: "Perform a calculation",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "Math expression to evaluate",
					},
				},
				"required": []any{"expression"},
			},
			Response: testmcp.ToolResponse{
				Content: []map[string]any{
					{
						"type": "text",
						"text": "Result: 42",
					},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeSSE, mockTools)
	t.Cleanup(mockServer.Close)

	// Set up toolset with external MCP configuration using SSE transport
	config := setupToolsetWithExternalMCP(t, ctx, ti, mockServer.URL, externalmcp_types.TransportTypeSSE, "calc-sse")

	// Step 1: Initialize
	initResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, map[string]any{
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
	})
	require.Equal(t, http.StatusOK, initResp.Code, "initialize failed: %s", initResp.Body.String())

	// Step 2: List tools - should include external tools with prefix
	listResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	require.Equal(t, http.StatusOK, listResp.Code, "tools/list failed: %s", listResp.Body.String())

	var listResult map[string]any
	err := json.Unmarshal(listResp.Body.Bytes(), &listResult)
	require.NoError(t, err)

	result, ok := listResult["result"].(map[string]any)
	require.True(t, ok)
	tools, ok := result["tools"].([]any)
	require.True(t, ok)
	require.GreaterOrEqual(t, len(tools), 1, "expected at least 1 tool")

	// Find the external tool (should be prefixed with slug)
	var foundTool map[string]any
	expectedToolName := config.slug + "--calculate"
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		require.True(t, ok, "expected tool to be a map")
		if toolMap["name"] == expectedToolName {
			foundTool = toolMap
			break
		}
	}
	require.NotNil(t, foundTool, "expected to find tool %s in tools list", expectedToolName)
	require.Equal(t, "Perform a calculation", foundTool["description"])

	// Step 3: Call the external tool
	callResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": expectedToolName,
			"arguments": map[string]any{
				"expression": "6 * 7",
			},
		},
	})
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call failed: %s", callResp.Body.String())

	var callResult map[string]any
	err = json.Unmarshal(callResp.Body.Bytes(), &callResult)
	require.NoError(t, err)

	callResultData, ok := callResult["result"].(map[string]any)
	require.True(t, ok, "expected result in response: %v", callResult)
	content, ok := callResultData["content"].([]any)
	require.True(t, ok, "expected content array in result")
	require.Len(t, content, 1)

	firstContent, ok := content[0].(map[string]any)
	require.True(t, ok, "expected content item to be a map")
	require.Equal(t, "text", firstContent["type"])
	require.Equal(t, "Result: 42", firstContent["text"])
}

// TestE2E_ExternalMCP_Proxy_Annotations verifies that tool annotations from an
// external MCP server are parsed and forwarded in the tools/list response.
// This covers the ptrBool fix: explicit false values must be preserved as false,
// not dropped as nil/absent.
func TestE2E_ExternalMCP_Proxy_Annotations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	mockTools := []testmcp.Tool{
		{
			Name:        "read_data",
			Description: "Read data from the database",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Annotations: &mcp.ToolAnnotations{
				Title:           "Read Data",
				ReadOnlyHint:    true,
				DestructiveHint: new(false), // explicit false — must not be dropped
				IdempotentHint:  true,
				OpenWorldHint:   new(false), // explicit false — must not be dropped
			},
			Response: testmcp.ToolResponse{
				Content: []map[string]any{
					{"type": "text", "text": "data"},
				},
			},
		},
		{
			Name:        "delete_record",
			Description: "Delete a record permanently",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    false,
				DestructiveHint: new(true),
				IdempotentHint:  true,
				OpenWorldHint:   new(true),
			},
			Response: testmcp.ToolResponse{
				Content: []map[string]any{
					{"type": "text", "text": "deleted"},
				},
			},
		},
	}

	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	config := setupToolsetWithExternalMCP(t, ctx, ti, mockServer.URL, externalmcp_types.TransportTypeStreamableHTTP, "annot-test")

	// Initialize
	initResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
		},
	})
	require.Equal(t, http.StatusOK, initResp.Code, "initialize failed: %s", initResp.Body.String())

	// List tools
	listResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	require.Equal(t, http.StatusOK, listResp.Code, "tools/list failed: %s", listResp.Body.String())

	var listResult map[string]any
	err := json.Unmarshal(listResp.Body.Bytes(), &listResult)
	require.NoError(t, err)

	result, ok := listResult["result"].(map[string]any)
	require.True(t, ok)
	tools, ok := result["tools"].([]any)
	require.True(t, ok)
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools")

	// Build a tool lookup by name
	toolsByName := make(map[string]map[string]any)
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		require.True(t, ok)
		name, _ := toolMap["name"].(string)
		toolsByName[name] = toolMap
	}

	// Verify read_data annotations
	readTool := toolsByName[config.slug+"--read_data"]
	require.NotNil(t, readTool, "expected to find read_data tool")

	readAnnotations, ok := readTool["annotations"].(map[string]any)
	require.True(t, ok, "expected annotations on read_data tool, got: %v", readTool)

	require.Equal(t, "Read Data", readAnnotations["title"], "title should be preserved")
	require.Equal(t, true, readAnnotations["readOnlyHint"], "readOnlyHint should be true")
	require.Equal(t, false, readAnnotations["destructiveHint"], "explicit false must be preserved, not dropped")
	require.Equal(t, true, readAnnotations["idempotentHint"], "idempotentHint should be true")
	require.Equal(t, false, readAnnotations["openWorldHint"], "explicit false must be preserved, not dropped")

	// Verify delete_record annotations
	deleteTool := toolsByName[config.slug+"--delete_record"]
	require.NotNil(t, deleteTool, "expected to find delete_record tool")

	deleteAnnotations, ok := deleteTool["annotations"].(map[string]any)
	require.True(t, ok, "expected annotations on delete_record tool, got: %v", deleteTool)

	require.Equal(t, true, deleteAnnotations["destructiveHint"], "destructiveHint should be true")
	require.Equal(t, true, deleteAnnotations["idempotentHint"], "deleting same record twice has no additional effect")
	require.Equal(t, true, deleteAnnotations["openWorldHint"], "openWorldHint should be true")
}
