package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	externalmcp_repo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// mockTool represents a tool that the mock MCP server will expose
type mockTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Response    mockToolResponse
}

// mockToolResponse represents the response content for a tool call
type mockToolResponse struct {
	Content []map[string]any
	IsError bool
}

// sseResponseChannel manages the channel for sending SSE responses
type sseResponseChannel struct {
	ch     chan []byte
	mu     sync.Mutex
	closed bool
}

func newSSEResponseChannel() *sseResponseChannel {
	return &sseResponseChannel{
		ch: make(chan []byte, 100),
	}
}

func (s *sseResponseChannel) send(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.ch <- data
	}
}

func (s *sseResponseChannel) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// newMockExternalMCPServer creates an httptest server that speaks the MCP protocol
func newMockExternalMCPServer(t *testing.T, transportType externalmcp_types.TransportType, tools []mockTool) *httptest.Server {
	t.Helper()

	// For SSE transport, we need a channel to send responses back to the SSE stream
	var sseResp *sseResponseChannel
	if transportType == externalmcp_types.TransportTypeSSE {
		sseResp = newSSEResponseChannel()
	}

	// Helper to process JSON-RPC requests and generate responses
	processRequest := func(rpcRequest map[string]any) map[string]any {
		method, ok := rpcRequest["method"].(string)
		if !ok {
			return nil
		}

		requestID := rpcRequest["id"]

		switch method {
		case "initialize":
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"result": map[string]any{
					"protocolVersion": "2025-03-26",
					"capabilities": map[string]any{
						"tools": map[string]any{},
					},
					"serverInfo": map[string]any{
						"name":    "test-external-mcp-server",
						"version": "1.0.0",
					},
				},
			}
		case "notifications/initialized":
			return nil
		case "tools/list":
			toolsList := make([]map[string]any, 0, len(tools))
			for _, tool := range tools {
				toolsList = append(toolsList, map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"inputSchema": tool.InputSchema,
				})
			}
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"result": map[string]any{
					"tools": toolsList,
				},
			}
		case "tools/call":
			params, ok := rpcRequest["params"].(map[string]any)
			if !ok {
				return nil
			}
			toolName, ok := params["name"].(string)
			if !ok {
				return nil
			}

			var mockResp mockToolResponse
			for _, tool := range tools {
				if tool.Name == toolName {
					mockResp = tool.Response
					break
				}
			}

			return map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"result": map[string]any{
					"content": mockResp.Content,
					"isError": mockResp.IsError,
				},
			}
		default:
			t.Logf("unexpected method: %s", method)
			return nil
		}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if transportType == externalmcp_types.TransportTypeSSE {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.WriteHeader(http.StatusOK)

				flusher, ok := w.(http.Flusher)
				if !ok {
					t.Fatal("ResponseWriter doesn't support flushing")
					return
				}

				endpoint := "/mcp"
				_, _ = fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpoint)
				flusher.Flush()

				for {
					select {
					case data, ok := <-sseResp.ch:
						if !ok {
							return
						}
						_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
						flusher.Flush()
					case <-r.Context().Done():
						// Client disconnected - just return, don't close shared channel
						return
					}
				}
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodDelete {
			// Don't close the shared channel on DELETE - just acknowledge
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			t.Fatalf("unexpected HTTP method: %s", r.Method)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("error reading body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer func() { _ = r.Body.Close() }()

		var rpcRequests []map[string]any
		if err := json.Unmarshal(body, &rpcRequests); err != nil {
			var singleRequest map[string]any
			if err := json.Unmarshal(body, &singleRequest); err != nil {
				t.Logf("error parsing JSON: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			rpcRequests = []map[string]any{singleRequest}
		}

		responses := make([]map[string]any, 0, len(rpcRequests))
		for _, rpcRequest := range rpcRequests {
			response := processRequest(rpcRequest)
			if response != nil {
				responses = append(responses, response)
			}
		}

		if transportType == externalmcp_types.TransportTypeSSE && sseResp != nil {
			for _, response := range responses {
				data, err := json.Marshal(response)
				if err != nil {
					t.Logf("error marshaling response: %v", err)
					continue
				}
				sseResp.send(data)
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if len(rpcRequests) == 1 && len(responses) == 1 {
			_ = json.NewEncoder(w).Encode(responses[0])
		} else {
			_ = json.NewEncoder(w).Encode(responses)
		}
	})

	server := httptest.NewServer(handler)

	if sseResp != nil {
		t.Cleanup(func() {
			sseResp.close()
		})
	}

	return server
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

	// Create deployment
	var deploymentID uuid.UUID
	err = ti.conn.QueryRow(ctx, `
		INSERT INTO deployments (project_id, organization_id, user_id, idempotency_key)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, projectID, orgID, "test-user", uuid.New().String()).Scan(&deploymentID)
	require.NoError(t, err)

	// Mark deployment as completed (active)
	_, err = ti.conn.Exec(ctx, `
		INSERT INTO deployment_statuses (deployment_id, status)
		VALUES ($1, 'completed')
	`, deploymentID)
	require.NoError(t, err)

	// Create MCP registry
	var registryID uuid.UUID
	err = ti.conn.QueryRow(ctx, `
		INSERT INTO mcp_registries (name, url)
		VALUES ($1, $2)
		RETURNING id
	`, "test-registry-"+slug, mockServerURL).Scan(&registryID)
	require.NoError(t, err)

	// Create external MCP attachment
	attachment, err := externalmcpRepo.CreateExternalMCPAttachment(ctx, externalmcp_repo.CreateExternalMCPAttachmentParams{
		DeploymentID:            deploymentID,
		RegistryID:              registryID,
		Name:                    "External MCP Server",
		Slug:                    slug,
		RegistryServerSpecifier: "test-server",
	})
	require.NoError(t, err)

	// Create tool definition with the external MCP tool URN
	toolURN := "tools:externalmcp:" + slug + ":proxy"
	toolDef, err := externalmcpRepo.CreateExternalMCPToolDefinition(ctx, externalmcp_repo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachment.ID,
		ToolUrn:                    toolURN,
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
	_, err = ti.conn.Exec(ctx, `
		INSERT INTO toolset_versions (toolset_id, version, tool_urns, resource_urns)
		VALUES ($1, 1, $2, '{}')
	`, toolset.ID, []string{toolURN})
	require.NoError(t, err)

	return &externalMCPConfig{
		toolset:      toolset,
		deploymentID: deploymentID,
		attachmentID: attachment.ID,
		toolDefID:    toolDef.ID,
		toolURN:      toolURN,
		slug:         slug,
	}
}

// getTestProjectAndOrg extracts project and org IDs from the test context
func getTestProjectAndOrg(t *testing.T, ctx context.Context, ti *testInstance) (uuid.UUID, string) {
	t.Helper()

	// Query for an existing project created by testenv.InitAuthContext
	var projectID uuid.UUID
	var orgID string
	err := ti.conn.QueryRow(ctx, `
		SELECT id, organization_id FROM projects LIMIT 1
	`).Scan(&projectID, &orgID)
	require.NoError(t, err)

	return projectID, orgID
}

// sendMCPRequest sends an MCP request to the service and returns the response
func sendMCPRequest(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	mcpSlug string,
	requests []map[string]any,
) *httptest.ResponseRecorder {
	t.Helper()

	bodyBytes, err := json.Marshal(requests)
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
	mockTools := []mockTool{
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
			Response: mockToolResponse{
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
	initResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
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
	})
	require.Equal(t, http.StatusOK, initResp.Code, "initialize failed: %s", initResp.Body.String())

	// Step 2: List tools - should include external tools with prefix
	listResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/list",
		},
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
	callResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "tools/call",
			"params": map[string]any{
				"name": expectedToolName,
				"arguments": map[string]any{
					"location": "San Francisco",
				},
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
	mockTools := []mockTool{
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
			Response: mockToolResponse{
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
	initResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
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
	})
	require.Equal(t, http.StatusOK, initResp.Code, "initialize failed: %s", initResp.Body.String())

	// Step 2: List tools - should include external tools with prefix
	listResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/list",
		},
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
	callResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "tools/call",
			"params": map[string]any{
				"name": expectedToolName,
				"arguments": map[string]any{
					"expression": "6 * 7",
				},
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
