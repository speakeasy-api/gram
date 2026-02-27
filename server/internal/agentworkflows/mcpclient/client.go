package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Client wraps mcp.Service for agent tool access.
// It provides zero-overhead access to MCP tools by calling mcp.Service methods directly
// without HTTP or JSON-RPC serialization overhead.
type Client struct {
	mcpService  *mcp.Service
	projectID   uuid.UUID
	toolset     string
	environment string
	sessionID   string
	chatID      string
	mode        mcp.ToolMode
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	URN         *urn.Tool // For billing/logging
}

// ToolResult represents tool execution result.
type ToolResult struct {
	Content []ContentChunk
	IsError bool
}

// ContentChunk represents one piece of tool output.
type ContentChunk struct {
	Type     string // "text", "image", "audio", "resource"
	Text     string
	Data     string // base64 for binary
	MimeType string
}

// NewClient creates a new MCPClient wrapper around mcp.Service.
func NewClient(mcpService *mcp.Service, projectID uuid.UUID, toolset, environment string) *Client {
	return &Client{
		mcpService:  mcpService,
		projectID:   projectID,
		toolset:     toolset,
		environment: environment,
		mode:        mcp.ToolModeStatic,
		sessionID:   "",
		chatID:      "",
	}
}

// ListTools lists all tools available in the toolset.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	// Build McpInputs
	inputs := &mcp.McpInputs{
		ProjectID:        c.projectID,
		Toolset:          c.toolset,
		Environment:      c.environment,
		SessionID:        c.sessionID,
		ChatID:           c.chatID,
		Mode:             c.mode,
		McpEnvVariables:  nil,
		OauthTokenInputs: nil,
		Authenticated:    false,
		UserID:           "",
		ExternalUserID:   "",
		APIKeyID:         "",
	}

	// Call mcp.Service.HandleToolsList directly
	result, err := c.mcpService.HandleToolsList(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	// Convert to []Tool
	tools := make([]Tool, len(result.Tools))
	for i, t := range result.Tools {
		// Note: URN extraction from meta is omitted for now
		// as it will be handled by the agent workflows layer
		tools[i] = Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			URN:         nil, // Will be populated by caller if needed
		}
	}

	return tools, nil
}

// CallTool executes a tool with the given name and arguments.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	inputs := &mcp.McpInputs{
		ProjectID:        c.projectID,
		Toolset:          c.toolset,
		Environment:      c.environment,
		SessionID:        c.sessionID,
		ChatID:           c.chatID,
		Mode:             c.mode,
		McpEnvVariables:  nil,
		OauthTokenInputs: nil,
		Authenticated:    false,
		UserID:           "",
		ExternalUserID:   "",
		APIKeyID:         "",
	}

	// Call mcp.Service.HandleToolsCall directly
	result, err := c.mcpService.HandleToolsCall(ctx, inputs, name, args)
	if err != nil {
		return nil, fmt.Errorf("call tool %s: %w", name, err)
	}

	// Convert to ToolResult
	content := make([]ContentChunk, len(result.Content))
	for i, c := range result.Content {
		content[i] = ContentChunk{
			Type:     c.Type,
			Text:     c.Text,
			Data:     c.Data,
			MimeType: c.MimeType,
		}
	}

	return &ToolResult{
		Content: content,
		IsError: result.IsError,
	}, nil
}
