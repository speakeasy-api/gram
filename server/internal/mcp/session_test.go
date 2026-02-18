package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	chat_repo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
)

// TestE2E_MCPSession_ExternalMCPSchemaInjection verifies that external MCP proxy tools
// DO have x-gram-session and x-gram-messages fields injected into their schemas.
// Even though external MCP servers manage their own sessions, we inject Gram session
// fields so LLMs can maintain continuity across the Gram MCP proxy layer.
func TestE2E_MCPSession_ExternalMCPSchemaInjection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	// Create mock external MCP server with a simple tool
	mockTools := []mockTool{
		{
			Name:        "echo",
			Description: "Echo the input back",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{
						"type":        "string",
						"description": "Message to echo",
					},
				},
				"required": []any{"message"},
			},
			Response: mockToolResponse{
				Content: []map[string]any{
					{
						"type": "text",
						"text": "Echo: hello",
					},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	config := setupToolsetWithExternalMCP(t, ctx, ti, mockServer.URL, externalmcp_types.TransportTypeStreamableHTTP, "session-schema-test")

	// Initialize
	initResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			},
		},
	})
	require.Equal(t, http.StatusOK, initResp.Code, "initialize failed: %s", initResp.Body.String())

	// List tools
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

	// Find our echo tool and verify session fields ARE present
	expectedToolName := config.slug + "--echo"
	var foundTool map[string]any
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		require.True(t, ok)
		if toolMap["name"] == expectedToolName {
			foundTool = toolMap
			break
		}
	}
	require.NotNil(t, foundTool, "expected to find tool %s", expectedToolName)

	// Check that inputSchema DOES contain x-gram-session
	inputSchema, ok := foundTool["inputSchema"].(map[string]any)
	require.True(t, ok, "expected inputSchema to be a map")

	properties, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok, "expected properties in inputSchema")

	// Verify x-gram-session IS present
	_, hasSession := properties["x-gram-session"]
	require.True(t, hasSession, "external MCP tools should have x-gram-session injected")

	// Verify x-gram-messages IS present
	_, hasMessages := properties["x-gram-messages"]
	require.True(t, hasMessages, "external MCP tools should have x-gram-messages injected")

	// Verify description contains session tracking instruction
	description, ok := foundTool["description"].(string)
	require.True(t, ok, "expected description to be a string")
	require.Contains(t, description, "Session Tracking", "description should contain session tracking instruction")
}

// TestE2E_MCPSession_ResponseContainsSessionID verifies that tools/call responses
// include the session ID in the _meta field.
func TestE2E_MCPSession_ResponseContainsSessionID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	mockTools := []mockTool{
		{
			Name:        "greet",
			Description: "Greet someone",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Name to greet",
					},
				},
			},
			Response: mockToolResponse{
				Content: []map[string]any{
					{
						"type": "text",
						"text": "Hello, World!",
					},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	config := setupToolsetWithExternalMCP(t, ctx, ti, mockServer.URL, externalmcp_types.TransportTypeStreamableHTTP, "session-response-test")

	// Initialize
	initResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			},
		},
	})
	require.Equal(t, http.StatusOK, initResp.Code)

	// Call the tool (without providing a session ID - should generate one)
	expectedToolName := config.slug + "--greet"
	callResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name": expectedToolName,
				"arguments": map[string]any{
					"name": "World",
				},
			},
		},
	})
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call failed: %s", callResp.Body.String())

	var callResult map[string]any
	err := json.Unmarshal(callResp.Body.Bytes(), &callResult)
	require.NoError(t, err)

	result, ok := callResult["result"].(map[string]any)
	require.True(t, ok, "expected result in response")

	content, ok := result["content"].([]any)
	require.True(t, ok, "expected content array in result")
	require.Len(t, content, 1)

	firstContent, ok := content[0].(map[string]any)
	require.True(t, ok)

	// Check for session ID in _meta
	meta, ok := firstContent["_meta"].(map[string]any)
	require.True(t, ok, "expected _meta in content, got: %v", firstContent)

	sessionID, ok := meta["x-gram-session"].(string)
	require.True(t, ok, "expected x-gram-session in _meta")
	require.NotEmpty(t, sessionID, "session ID should not be empty")

	// Verify it's a valid UUID
	_, err = uuid.Parse(sessionID)
	require.NoError(t, err, "session ID should be a valid UUID")
}

// TestE2E_MCPSession_Propagation verifies that session IDs are propagated across
// multiple tool calls when the client provides the session ID from a previous response.
func TestE2E_MCPSession_Propagation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	mockTools := []mockTool{
		{
			Name:        "counter",
			Description: "Increment a counter",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Response: mockToolResponse{
				Content: []map[string]any{
					{
						"type": "text",
						"text": "Counter: 1",
					},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	config := setupToolsetWithExternalMCP(t, ctx, ti, mockServer.URL, externalmcp_types.TransportTypeStreamableHTTP, "session-prop-test")

	// Initialize
	initResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			},
		},
	})
	require.Equal(t, http.StatusOK, initResp.Code)

	expectedToolName := config.slug + "--counter"

	// First tool call - no session ID provided
	call1Resp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      expectedToolName,
				"arguments": map[string]any{},
			},
		},
	})
	require.Equal(t, http.StatusOK, call1Resp.Code)

	var call1Result map[string]any
	err := json.Unmarshal(call1Resp.Body.Bytes(), &call1Result)
	require.NoError(t, err)

	// Extract session ID from first response
	result1, ok := call1Result["result"].(map[string]any)
	require.True(t, ok)
	content1, ok := result1["content"].([]any)
	require.True(t, ok)
	require.Len(t, content1, 1)
	firstContent1, ok := content1[0].(map[string]any)
	require.True(t, ok)
	meta1, ok := firstContent1["_meta"].(map[string]any)
	require.True(t, ok)
	sessionID1, ok := meta1["x-gram-session"].(string)
	require.True(t, ok)
	require.NotEmpty(t, sessionID1)

	// Second tool call - provide the session ID from the first call
	call2Resp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "tools/call",
			"params": map[string]any{
				"name": expectedToolName,
				"arguments": map[string]any{
					"x-gram-session": sessionID1,
				},
			},
		},
	})
	require.Equal(t, http.StatusOK, call2Resp.Code)

	var call2Result map[string]any
	err = json.Unmarshal(call2Resp.Body.Bytes(), &call2Result)
	require.NoError(t, err)

	// Extract session ID from second response
	result2, ok := call2Result["result"].(map[string]any)
	require.True(t, ok)
	content2, ok := result2["content"].([]any)
	require.True(t, ok)
	require.Len(t, content2, 1)
	firstContent2, ok := content2[0].(map[string]any)
	require.True(t, ok)
	meta2, ok := firstContent2["_meta"].(map[string]any)
	require.True(t, ok)
	sessionID2, ok := meta2["x-gram-session"].(string)
	require.True(t, ok)

	// Session IDs should match - same session was used
	require.Equal(t, sessionID1, sessionID2, "session ID should be propagated across calls")
}

// TestE2E_MCPSession_DatabaseStorage verifies that MCP sessions and messages
// are stored in the database (chats and chat_messages tables).
func TestE2E_MCPSession_DatabaseStorage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	mockTools := []mockTool{
		{
			Name:        "store_test",
			Description: "Test tool for storage",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Response: mockToolResponse{
				Content: []map[string]any{
					{
						"type": "text",
						"text": "Stored!",
					},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	config := setupToolsetWithExternalMCP(t, ctx, ti, mockServer.URL, externalmcp_types.TransportTypeStreamableHTTP, "session-storage-test")

	// Initialize
	initResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			},
		},
	})
	require.Equal(t, http.StatusOK, initResp.Code)

	expectedToolName := config.slug + "--store_test"

	// Call the tool with conversation messages
	callResp := sendMCPRequest(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name": expectedToolName,
				"arguments": map[string]any{
					"x-gram-messages": []map[string]any{
						{"role": "user", "content": "Hello, can you store something?"},
						{"role": "assistant", "content": "Sure, I'll call the store_test tool for you."},
					},
				},
			},
		},
	})
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call failed: %s", callResp.Body.String())

	var callResult map[string]any
	err := json.Unmarshal(callResp.Body.Bytes(), &callResult)
	require.NoError(t, err)

	// Extract session ID
	result, ok := callResult["result"].(map[string]any)
	require.True(t, ok)
	content, ok := result["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	firstContent, ok := content[0].(map[string]any)
	require.True(t, ok)
	meta, ok := firstContent["_meta"].(map[string]any)
	require.True(t, ok)
	sessionIDStr, ok := meta["x-gram-session"].(string)
	require.True(t, ok)
	require.NotEmpty(t, sessionIDStr)

	sessionID, err := uuid.Parse(sessionIDStr)
	require.NoError(t, err)

	// Give async storage a moment to complete
	time.Sleep(100 * time.Millisecond)

	// Verify the session was stored in the chats table
	chatRepo := chat_repo.New(ti.conn)
	chat, err := chatRepo.GetChat(ctx, sessionID)
	require.NoError(t, err, "session should be stored in chats table")
	require.Equal(t, "MCP", chat.Source.String, "chat source should be MCP")
	require.Equal(t, config.toolset.ProjectID, chat.ProjectID)

	// Verify messages were stored in chat_messages table
	messages, err := chatRepo.ListChatMessages(ctx, chat_repo.ListChatMessagesParams{
		ChatID:    sessionID,
		ProjectID: config.toolset.ProjectID,
	})
	require.NoError(t, err)

	// Should have at least 3 messages: 2 from x-gram-messages + 1 tool response
	require.GreaterOrEqual(t, len(messages), 3, "expected at least 3 messages stored")

	// Find user message
	var foundUserMsg, foundAssistantMsg, foundToolMsg bool
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if msg.Content == "Hello, can you store something?" {
				foundUserMsg = true
			}
		case "assistant":
			if msg.Content == "Sure, I'll call the store_test tool for you." {
				foundAssistantMsg = true
			}
		case "tool":
			foundToolMsg = true
		}
	}

	require.True(t, foundUserMsg, "user message should be stored")
	require.True(t, foundAssistantMsg, "assistant message should be stored")
	require.True(t, foundToolMsg, "tool response should be stored")
}

// TestE2E_MCPSession_HeaderFallback verifies that when no x-gram-session is in
// the arguments, the session ID falls back to the Mcp-Session-Id header.
func TestE2E_MCPSession_HeaderFallback(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	mockTools := []mockTool{
		{
			Name:        "fallback_test",
			Description: "Test tool for header fallback",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Response: mockToolResponse{
				Content: []map[string]any{
					{
						"type": "text",
						"text": "OK",
					},
				},
				IsError: false,
			},
		},
	}

	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	config := setupToolsetWithExternalMCP(t, ctx, ti, mockServer.URL, externalmcp_types.TransportTypeStreamableHTTP, "header-fallback-test")

	// Initialize and get the Mcp-Session-Id from the response header
	initResp := sendMCPRequestWithHeaders(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			},
		},
	}, nil)
	require.Equal(t, http.StatusOK, initResp.Code)

	// Get the session ID from the header
	mcpSessionID := initResp.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, mcpSessionID, "Mcp-Session-Id header should be present")

	expectedToolName := config.slug + "--fallback_test"

	// Call the tool with Mcp-Session-Id header but no x-gram-session in args
	headers := map[string]string{
		"Mcp-Session-Id": mcpSessionID,
	}
	callResp := sendMCPRequestWithHeaders(t, ctx, ti, config.toolset.McpSlug.String, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      expectedToolName,
				"arguments": map[string]any{}, // No x-gram-session
			},
		},
	}, headers)
	require.Equal(t, http.StatusOK, callResp.Code)

	var callResult map[string]any
	err := json.Unmarshal(callResp.Body.Bytes(), &callResult)
	require.NoError(t, err)

	// Extract session ID from response
	result, ok := callResult["result"].(map[string]any)
	require.True(t, ok)
	content, ok := result["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	firstContent, ok := content[0].(map[string]any)
	require.True(t, ok)
	meta, ok := firstContent["_meta"].(map[string]any)
	require.True(t, ok)
	responseSessionID, ok := meta["x-gram-session"].(string)
	require.True(t, ok)

	// The session ID in the response should match the header
	require.Equal(t, mcpSessionID, responseSessionID, "session ID should fall back to Mcp-Session-Id header")
}

// sendMCPRequestWithHeaders is like sendMCPRequest but allows custom headers
func sendMCPRequestWithHeaders(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	mcpSlug string,
	requests []map[string]any,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()

	bodyBytes, err := json.Marshal(requests)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

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
