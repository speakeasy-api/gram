package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/mcp"
)

// InternalMCPClient adapts the MCP service to implement chat.MCPClient interface.
// This allows the chat client to use MCP protocol without creating import cycles.
type InternalMCPClient struct {
	mcpService *mcp.Service
}

// NewInternalMCPClient creates a new adapter for chat.MCPClient.
func NewInternalMCPClient(mcpService *mcp.Service) *InternalMCPClient {
	return &InternalMCPClient{
		mcpService: mcpService,
	}
}

// ListTools implements chat.MCPClient.ListTools
func (l *InternalMCPClient) ListTools(
	ctx context.Context,
	projectID uuid.UUID,
	toolset string,
	environment string,
	isAuthenticated bool,
) ([]chat.MCPTool, error) {
	inputs := &mcp.McpInputs{
		ProjectID:        projectID,
		Toolset:          toolset,
		Environment:      environment,
		SessionID:        "",
		ChatID:           "",
		Mode:             mcp.ToolModeStatic,
		McpEnvVariables:  nil,
		OauthTokenInputs: nil,
		Authenticated:    isAuthenticated,
		UserID:           "",
		ExternalUserID:   "",
		APIKeyID:         "",
	}

	result, err := l.mcpService.HandleToolsList(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("handle tools list: %w", err)
	}

	// Convert to chat.MCPTool
	tools := make([]chat.MCPTool, len(result.Tools))
	for i, t := range result.Tools {
		tools[i] = chat.MCPTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}

	return tools, nil
}

// CallTool implements chat.ToolsetLoader.CallTool
func (l *InternalMCPClient) CallTool(
	ctx context.Context,
	projectID uuid.UUID,
	toolset string,
	environment string,
	toolName string,
	args json.RawMessage,
	isAuthenticated bool,
) (*chat.MCPToolResult, error) {
	inputs := &mcp.McpInputs{
		ProjectID:        projectID,
		Toolset:          toolset,
		Environment:      environment,
		SessionID:        "",
		ChatID:           "",
		Mode:             mcp.ToolModeStatic,
		McpEnvVariables:  nil,
		OauthTokenInputs: nil,
		Authenticated:    isAuthenticated,
		UserID:           "",
		ExternalUserID:   "",
		APIKeyID:         "",
	}

	result, err := l.mcpService.HandleToolsCall(ctx, inputs, toolName, args)
	if err != nil {
		return nil, fmt.Errorf("handle tools call: %w", err)
	}

	// Convert to chat.MCPToolResult
	content := make([]chat.MCPContentChunk, len(result.Content))
	for i, c := range result.Content {
		content[i] = chat.MCPContentChunk{
			Type:     c.Type,
			Text:     c.Text,
			Data:     c.Data,
			MimeType: c.MimeType,
		}
	}

	return &chat.MCPToolResult{
		Content: content,
		IsError: result.IsError,
	}, nil
}

// Ensure InternalMCPClient implements chat.MCPClient
var _ chat.MCPClient = (*InternalMCPClient)(nil)
