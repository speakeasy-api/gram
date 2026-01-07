package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background())
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type e2eTestInstance struct {
	conn   *pgxpool.Pool
	repo   *repo.Queries
	logger *slog.Logger
}

func newE2ETestInstance(t *testing.T) (context.Context, *e2eTestInstance) {
	t.Helper()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "externalmcp_e2e")
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	queries := repo.New(conn)

	return ctx, &e2eTestInstance{
		conn:   conn,
		repo:   queries,
		logger: logger,
	}
}

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

// newMockMCPServer creates an httptest server that speaks the MCP protocol
func newMockMCPServer(t *testing.T, transportType types.TransportType, tools []mockTool) *httptest.Server {
	t.Helper()

	// For SSE transport, we need a channel to send responses back to the SSE stream
	var sseResp *sseResponseChannel
	if transportType == types.TransportTypeSSE {
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
			return mcpInitializeResponse(requestID)
		case "notifications/initialized":
			// This is a notification, no response needed
			return nil
		case "tools/list":
			return mcpToolsListResponse(requestID, tools)
		case "tools/call":
			params, ok := rpcRequest["params"].(map[string]any)
			if !ok {
				return nil
			}
			toolName, ok := params["name"].(string)
			if !ok {
				return nil
			}

			// Find the tool
			var mockResp mockToolResponse
			for _, tool := range tools {
				if tool.Name == toolName {
					mockResp = tool.Response
					break
				}
			}

			return mcpToolsCallResponse(requestID, toolName, mockResp)
		default:
			t.Logf("unexpected method: %s", method)
			return nil
		}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle GET requests differently for SSE vs StreamableHTTP
		if r.Method == http.MethodGet {
			if transportType == types.TransportTypeSSE {
				// SSE requires Server-Sent Events format
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.WriteHeader(http.StatusOK)

				flusher, ok := w.(http.Flusher)
				if !ok {
					t.Fatal("ResponseWriter doesn't support flushing")
					return
				}

				// Send endpoint event - tell the client where to POST requests
				endpoint := "/mcp"
				_, _ = fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpoint)
				flusher.Flush()

				// Keep connection open and forward responses from the channel
				for {
					select {
					case data, ok := <-sseResp.ch:
						if !ok {
							// Channel closed, end stream
							return
						}
						_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
						flusher.Flush()
					case <-r.Context().Done():
						// Client disconnected
						sseResp.close()
						return
					}
				}
			}
			// For StreamableHTTP, just return OK
			w.WriteHeader(http.StatusOK)
			return
		}

		// Handle DELETE requests (used for disconnection)
		if r.Method == http.MethodDelete {
			if sseResp != nil {
				sseResp.close()
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		// Handle POST requests (JSON-RPC)
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected HTTP method: %s", r.Method)
			return
		}

		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("error reading body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer func() { _ = r.Body.Close() }()

		// Parse JSON-RPC request - it could be a single request or an array
		var rpcRequests []map[string]any

		// Try parsing as array first
		if err := json.Unmarshal(body, &rpcRequests); err != nil {
			// Try parsing as single object
			var singleRequest map[string]any
			if err := json.Unmarshal(body, &singleRequest); err != nil {
				t.Logf("error parsing JSON: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			rpcRequests = []map[string]any{singleRequest}
		}

		// Process each request
		responses := make([]map[string]any, 0, len(rpcRequests))
		for _, rpcRequest := range rpcRequests {
			response := processRequest(rpcRequest)
			if response != nil {
				responses = append(responses, response)
			}
		}

		// For SSE transport, send responses via the SSE channel
		if transportType == types.TransportTypeSSE && sseResp != nil {
			for _, response := range responses {
				data, err := json.Marshal(response)
				if err != nil {
					t.Logf("error marshaling response: %v", err)
					continue
				}
				sseResp.send(data)
			}
			// Return 202 Accepted with empty body for SSE
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// For StreamableHTTP, return response in HTTP body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if len(rpcRequests) == 1 && len(responses) == 1 {
			_ = json.NewEncoder(w).Encode(responses[0])
		} else {
			_ = json.NewEncoder(w).Encode(responses)
		}
	})

	server := httptest.NewServer(handler)

	// Cleanup SSE channel when server closes
	if sseResp != nil {
		t.Cleanup(func() {
			sseResp.close()
		})
	}

	return server
}

// mcpInitializeResponse creates an MCP initialize response
func mcpInitializeResponse(requestID any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"result": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "test-mcp-server",
				"version": "1.0.0",
			},
		},
	}
}

// mcpToolsListResponse creates an MCP tools/list response
func mcpToolsListResponse(requestID any, tools []mockTool) map[string]any {
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
}

// mcpToolsCallResponse creates an MCP tools/call response
func mcpToolsCallResponse(requestID any, toolName string, mockResp mockToolResponse) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"result": map[string]any{
			"content": mockResp.Content,
			"isError": mockResp.IsError,
		},
	}
}

// createTestProject creates a test project in the database
func createTestProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool) uuid.UUID {
	t.Helper()

	// Create organization first
	orgID := "test-org-" + uuid.New().String()
	_, err := conn.Exec(ctx, `
		INSERT INTO organization_metadata (id, name, slug)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO NOTHING
	`, orgID, "Test Org", "test-org")
	require.NoError(t, err)

	// Create project
	var projectID uuid.UUID
	err = conn.QueryRow(ctx, `
		INSERT INTO projects (name, slug, organization_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "test-project", "test-project-"+uuid.New().String()[:8], orgID).Scan(&projectID)
	require.NoError(t, err)

	return projectID
}

// createTestDeployment creates a test deployment in the database
func createTestDeployment(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	// Get organization_id from project
	var orgID string
	err := conn.QueryRow(ctx, `SELECT organization_id FROM projects WHERE id = $1`, projectID).Scan(&orgID)
	require.NoError(t, err)

	var deploymentID uuid.UUID
	err = conn.QueryRow(ctx, `
		INSERT INTO deployments (project_id, organization_id, user_id, idempotency_key)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, projectID, orgID, "test-user", uuid.New().String()).Scan(&deploymentID)
	require.NoError(t, err)

	return deploymentID
}

// createTestRegistry creates a test MCP registry in the database
func createTestRegistry(t *testing.T, ctx context.Context, conn *pgxpool.Pool, url string) uuid.UUID {
	t.Helper()

	var registryID uuid.UUID
	err := conn.QueryRow(ctx, `
		INSERT INTO mcp_registries (name, url)
		VALUES ($1, $2)
		RETURNING id
	`, "test-registry", url).Scan(&registryID)
	require.NoError(t, err)

	return registryID
}

// createExternalMCPAttachment creates an external MCP attachment in the database
func createExternalMCPAttachment(
	t *testing.T,
	ctx context.Context,
	queries *repo.Queries,
	deploymentID uuid.UUID,
	registryID uuid.UUID,
	slug string,
) uuid.UUID {
	t.Helper()

	attachment, err := queries.CreateExternalMCPAttachment(ctx, repo.CreateExternalMCPAttachmentParams{
		DeploymentID:            deploymentID,
		RegistryID:              registryID,
		Name:                    "Test MCP Server",
		Slug:                    slug,
		RegistryServerSpecifier: "test-server",
	})
	require.NoError(t, err)

	return attachment.ID
}

// assertToolDefinitionExists verifies a tool definition was created and returns its ID
func assertToolDefinitionExists(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	attachmentID uuid.UUID,
	expectedTransport types.TransportType,
) uuid.UUID {
	t.Helper()

	var toolDefID uuid.UUID
	var transportType types.TransportType
	err := conn.QueryRow(ctx, `
		SELECT id, transport_type
		FROM external_mcp_tool_definitions
		WHERE external_mcp_attachment_id = $1 AND deleted IS FALSE
	`, attachmentID).Scan(&toolDefID, &transportType)
	require.NoError(t, err)
	require.Equal(t, expectedTransport, transportType)

	return toolDefID
}

// createTestMCPClient creates an MCP client with cleanup
func createTestMCPClient(
	t *testing.T,
	ctx context.Context,
	logger *slog.Logger,
	remoteURL string,
	transportType types.TransportType,
) *Client {
	t.Helper()

	client, err := NewClient(ctx, logger, remoteURL, transportType, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Logf("failed to close MCP client: %v", err)
		}
	})

	return client
}

func TestE2E_ExternalMCP_StreamableHTTP_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, testInst := newE2ETestInstance(t)

	// Create mock MCP server with StreamableHTTP transport
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
						"text": "The weather is sunny and 72°F",
					},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockMCPServer(t, types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	// Create test project and deployment
	projectID := createTestProject(t, ctx, testInst.conn)
	deploymentID := createTestDeployment(t, ctx, testInst.conn, projectID)
	registryID := createTestRegistry(t, ctx, testInst.conn, mockServer.URL)
	attachmentID := createExternalMCPAttachment(t, ctx, testInst.repo, deploymentID, registryID, "test-mcp")

	// Create tool definition (simulating deployment flow)
	_, err := testInst.repo.CreateExternalMCPToolDefinition(ctx, repo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachmentID,
		ToolUrn:                    "tools:externalmcp:test-mcp:proxy",
		RemoteUrl:                  mockServer.URL,
		TransportType:              types.TransportTypeStreamableHTTP,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
	})
	require.NoError(t, err)

	// Verify tool definition was created with correct transport type
	_ = assertToolDefinitionExists(t, ctx, testInst.conn, attachmentID, types.TransportTypeStreamableHTTP)

	// Create MCP client and list tools
	client := createTestMCPClient(t, ctx, testInst.logger, mockServer.URL, types.TransportTypeStreamableHTTP)
	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, "get_weather", tools[0].Name)
	require.Equal(t, "Get current weather for a location", tools[0].Description)

	// Call tool
	args := json.RawMessage(`{"location": "San Francisco"}`)
	result, err := client.CallTool(ctx, "get_weather", args)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 1)

	// Verify response content
	var content map[string]any
	err = json.Unmarshal(result.Content[0], &content)
	require.NoError(t, err)
	require.Equal(t, "text", content["type"])
	require.Equal(t, "The weather is sunny and 72°F", content["text"])
}

func TestE2E_ExternalMCP_SSE_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, testInst := newE2ETestInstance(t)

	// Create mock MCP server with SSE transport
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

	mockServer := newMockMCPServer(t, types.TransportTypeSSE, mockTools)
	t.Cleanup(mockServer.Close)

	// Create test project and deployment
	projectID := createTestProject(t, ctx, testInst.conn)
	deploymentID := createTestDeployment(t, ctx, testInst.conn, projectID)
	registryID := createTestRegistry(t, ctx, testInst.conn, mockServer.URL)
	attachmentID := createExternalMCPAttachment(t, ctx, testInst.repo, deploymentID, registryID, "calc-mcp")

	// Create tool definition with SSE transport
	_, err := testInst.repo.CreateExternalMCPToolDefinition(ctx, repo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachmentID,
		ToolUrn:                    "tools:externalmcp:calc-mcp:proxy",
		RemoteUrl:                  mockServer.URL,
		TransportType:              types.TransportTypeSSE,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
	})
	require.NoError(t, err)

	// Verify tool definition was created with correct transport type
	_ = assertToolDefinitionExists(t, ctx, testInst.conn, attachmentID, types.TransportTypeSSE)

	// Create MCP client and list tools
	client := createTestMCPClient(t, ctx, testInst.logger, mockServer.URL, types.TransportTypeSSE)
	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, "calculate", tools[0].Name)
	require.Equal(t, "Perform a calculation", tools[0].Description)

	// Call tool
	args := json.RawMessage(`{"expression": "6 * 7"}`)
	result, err := client.CallTool(ctx, "calculate", args)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 1)

	// Verify response content
	var content map[string]any
	err = json.Unmarshal(result.Content[0], &content)
	require.NoError(t, err)
	require.Equal(t, "text", content["type"])
	require.Equal(t, "Result: 42", content["text"])
}

func TestE2E_ExternalMCP_MultipleTools(t *testing.T) {
	t.Parallel()

	ctx, testInst := newE2ETestInstance(t)

	// Create mock MCP server with multiple tools
	mockTools := []mockTool{
		{
			Name:        "list_files",
			Description: "List files in directory",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
			Response: mockToolResponse{
				Content: []map[string]any{
					{"type": "text", "text": "file1.txt\nfile2.txt\nfile3.txt"},
				},
				IsError: false,
			},
		},
		{
			Name:        "read_file",
			Description: "Read file contents",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
			Response: mockToolResponse{
				Content: []map[string]any{
					{"type": "text", "text": "File contents here"},
				},
				IsError: false,
			},
		},
		{
			Name:        "write_file",
			Description: "Write to a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
			},
			Response: mockToolResponse{
				Content: []map[string]any{
					{"type": "text", "text": "File written successfully"},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockMCPServer(t, types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	// Create test data
	projectID := createTestProject(t, ctx, testInst.conn)
	deploymentID := createTestDeployment(t, ctx, testInst.conn, projectID)
	registryID := createTestRegistry(t, ctx, testInst.conn, mockServer.URL)
	attachmentID := createExternalMCPAttachment(t, ctx, testInst.repo, deploymentID, registryID, "filesystem-mcp")

	// Create tool definition
	_, err := testInst.repo.CreateExternalMCPToolDefinition(ctx, repo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachmentID,
		ToolUrn:                    "tools:externalmcp:filesystem-mcp:proxy",
		RemoteUrl:                  mockServer.URL,
		TransportType:              types.TransportTypeStreamableHTTP,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
	})
	require.NoError(t, err)

	// Create MCP client and list tools
	client := createTestMCPClient(t, ctx, testInst.logger, mockServer.URL, types.TransportTypeStreamableHTTP)
	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 3)

	// Verify all tools are present
	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Name
	}
	require.Contains(t, toolNames, "list_files")
	require.Contains(t, toolNames, "read_file")
	require.Contains(t, toolNames, "write_file")

	// Call each tool to verify they all work
	result1, err := client.CallTool(ctx, "list_files", json.RawMessage(`{"path": "/test"}`))
	require.NoError(t, err)
	require.False(t, result1.IsError)

	result2, err := client.CallTool(ctx, "read_file", json.RawMessage(`{"path": "/test/file1.txt"}`))
	require.NoError(t, err)
	require.False(t, result2.IsError)

	result3, err := client.CallTool(ctx, "write_file", json.RawMessage(`{"path": "/test/new.txt", "content": "hello"}`))
	require.NoError(t, err)
	require.False(t, result3.IsError)
}

func TestE2E_ExternalMCP_ToolCallWithArguments(t *testing.T) {
	t.Parallel()

	ctx, testInst := newE2ETestInstance(t)

	// Create mock MCP server with a tool that expects specific arguments
	mockTools := []mockTool{
		{
			Name:        "search",
			Description: "Search for information",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum results",
					},
					"filters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"category": map[string]any{"type": "string"},
							"language": map[string]any{"type": "string"},
						},
					},
				},
				"required": []any{"query"},
			},
			Response: mockToolResponse{
				Content: []map[string]any{
					{
						"type": "text",
						"text": "Found 3 results for 'golang tutorial'",
					},
					{
						"type": "text",
						"text": "Result 1: Go Tutorial 1 - https://example.com/result1",
					},
					{
						"type": "text",
						"text": "Result 2: Go Tutorial 2 - https://example.com/result2",
					},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockMCPServer(t, types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	// Create test data
	projectID := createTestProject(t, ctx, testInst.conn)
	deploymentID := createTestDeployment(t, ctx, testInst.conn, projectID)
	registryID := createTestRegistry(t, ctx, testInst.conn, mockServer.URL)
	attachmentID := createExternalMCPAttachment(t, ctx, testInst.repo, deploymentID, registryID, "search-mcp")

	// Create tool definition
	_, err := testInst.repo.CreateExternalMCPToolDefinition(ctx, repo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachmentID,
		ToolUrn:                    "tools:externalmcp:search-mcp:proxy",
		RemoteUrl:                  mockServer.URL,
		TransportType:              types.TransportTypeStreamableHTTP,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
	})
	require.NoError(t, err)

	// Create MCP client
	client := createTestMCPClient(t, ctx, testInst.logger, mockServer.URL, types.TransportTypeStreamableHTTP)

	// Call tool with complex arguments
	args := json.RawMessage(`{
		"query": "golang tutorial",
		"limit": 10,
		"filters": {
			"category": "programming",
			"language": "en"
		}
	}`)

	result, err := client.CallTool(ctx, "search", args)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 3)

	// Verify first content item (text)
	var content0 map[string]any
	err = json.Unmarshal(result.Content[0], &content0)
	require.NoError(t, err)
	require.Equal(t, "text", content0["type"])
	require.Equal(t, "Found 3 results for 'golang tutorial'", content0["text"])

	// Verify second content item (text)
	var content1 map[string]any
	err = json.Unmarshal(result.Content[1], &content1)
	require.NoError(t, err)
	require.Equal(t, "text", content1["type"])
	require.Equal(t, "Result 1: Go Tutorial 1 - https://example.com/result1", content1["text"])
}
