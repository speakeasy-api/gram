package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// MCPClient provides MCP-based tool loading for agent workflows.
// This interface allows the chat client to load tools via MCP protocol
// without creating import cycles with the mcp package.
type MCPClient interface {
	// ListTools lists all tools in a toolset via MCP protocol
	ListTools(ctx context.Context, projectID uuid.UUID, toolset, environment string, isAuthenticated bool) ([]MCPTool, error)
	// CallTool executes a tool via MCP protocol
	CallTool(ctx context.Context, projectID uuid.UUID, toolset, environment, toolName string, args json.RawMessage, isAuthenticated bool) (*MCPToolResult, error)
}

// MCPTool represents a tool definition from MCP
type MCPTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// MCPToolResult represents the result of an MCP tool call
type MCPToolResult struct {
	Content []MCPContentChunk
	IsError bool
}

// MCPContentChunk represents one piece of MCP tool output
type MCPContentChunk struct {
	Type     string
	Text     string
	Data     string
	MimeType string
}

type Client struct {
	logger           *slog.Logger
	completionClient openrouter.CompletionClient
	db               *pgxpool.Pool
	env              *environments.EnvironmentEntries
	cache            cache.Cache
	toolsetCache     cache.TypedCacheObject[mv.ToolsetBaseContents]
	toolsetLoader    MCPClient // MCP-based tool loader
}

var _ openrouter.CompletionClient = (*Client)(nil)

func (c *Client) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	resp, err := c.completionClient.GetCompletion(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get completion: %w", err)
	}
	return resp, nil
}

func (c *Client) GetCompletionStream(ctx context.Context, request openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	stream, err := c.completionClient.GetCompletionStream(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get completion stream: %w", err)
	}
	return stream, nil
}

func (c *Client) GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	resp, err := c.completionClient.GetObjectCompletion(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get object completion: %w", err)
	}
	return resp, nil
}

func (c *Client) CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string) ([][]float32, error) {
	embeddings, err := c.completionClient.CreateEmbeddings(ctx, orgID, model, inputs)
	if err != nil {
		return nil, fmt.Errorf("create embeddings: %w", err)
	}
	return embeddings, nil
}

func NewAgenticChatClient(
	logger *slog.Logger,
	db *pgxpool.Pool,
	env *environments.EnvironmentEntries,
	cacheImpl cache.Cache,
	completionClient openrouter.CompletionClient,
	toolsetLoader MCPClient,
) *Client {
	return &Client{
		logger:           logger.With(attr.SlogComponent("agentic_chat_client")),
		completionClient: completionClient,
		db:               db,
		env:              env,
		cache:            cacheImpl,
		toolsetCache:     cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheImpl, cache.SuffixNone),
		toolsetLoader:    toolsetLoader,
	}
}

type AgentTool struct {
	Definition openrouter.Tool
	Executor   func(ctx context.Context, rawArgs string) (string, error)
}

type AgentChatOptions struct {
	SystemPrompt    *string
	ToolsetSlug     *string
	AdditionalTools []AgentTool
	AgentTimeout    *time.Duration
	Temperature     *float64
	Model           string
	UsageSource     billing.ModelUsageSource
}

// AgentChat loops over tool calls until completion and returns the final message.
func (c *Client) AgentChat(
	ctx context.Context,
	orgID string,
	projectID uuid.UUID,
	prompt string,
	opts AgentChatOptions,
) (string, error) {
	if opts.AgentTimeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *opts.AgentTimeout)
		defer cancel()
	}

	var messages []or.ChatMessages

	// Optional system prompt
	if opts.SystemPrompt != nil {
		messages = append(messages, or.CreateChatMessagesSystem(or.ChatSystemMessage{
			Role:    or.ChatSystemMessageRoleSystem,
			Content: or.CreateChatSystemMessageContentStr(*opts.SystemPrompt),
			Name:    nil,
		}))
	}

	// User message
	messages = append(messages, or.CreateChatMessagesUser(or.ChatUserMessage{
		Role:    or.ChatUserMessageRoleUser,
		Content: or.CreateChatUserMessageContentStr(prompt),
		Name:    nil,
	}))

	// Register tool definitions and their executors
	agentTools := opts.AdditionalTools
	if opts.ToolsetSlug != nil {
		toolsetTools, err := c.LoadToolsetTools(ctx, projectID, *opts.ToolsetSlug)
		if err != nil {
			return "", fmt.Errorf("failed to load toolset tools: %w", err)
		}
		agentTools = append(agentTools, toolsetTools...)
	}

	toolDefs := make([]openrouter.Tool, 0, len(agentTools))
	toolMap := make(map[string]AgentTool)
	for _, t := range agentTools {
		if t.Definition.Function != nil {
			toolDefs = append(toolDefs, t.Definition)
			toolMap[t.Definition.Function.Name] = t
		}
	}

	// Generate a chat ID so that the messages get stored
	// TODO: slack -- support "resuming" a conversation
	chatID := uuid.New()

	// Agent loop
	// Keep making completions calls until we get a final message without tool calls
	for {
		// Build completion request
		completionReq := openrouter.CompletionRequest{
			OrgID:                     orgID,
			ProjectID:                 projectID.String(),
			Messages:                  messages,
			Tools:                     toolDefs,
			Temperature:               opts.Temperature,
			Model:                     opts.Model,
			Stream:                    false,
			UsageSource:               billing.ModelUsageSourceAgents,
			ChatID:                    chatID,
			UserID:                    "",
			ExternalUserID:            "", // TODO
			UserEmail:                 "",
			HTTPMetadata:              nil,
			APIKeyID:                  "",
			JSONSchema:                nil,
			NormalizeOutboundMessages: false,
		}

		if opts.UsageSource != "" {
			completionReq.UsageSource = opts.UsageSource
		}

		response, err := c.completionClient.GetCompletion(ctx, completionReq)
		if err != nil {
			return "", fmt.Errorf("failed to get completion: %w", err)
		}

		messages = append(messages, *response.Message)

		// No tool calls = final assistant message
		if len(response.ToolCalls) == 0 {
			return openrouter.GetText(*response.Message), nil
		}

		// Tool call loop
		for _, tc := range response.ToolCalls {
			c.logger.InfoContext(ctx, "Tool called", attr.SlogToolName(tc.Function.Name))

			var output string
			tool, ok := toolMap[tc.Function.Name]
			if !ok || tool.Executor == nil {
				output = fmt.Sprintf("No tool found for %q", tc.Function.Name)
				c.logger.ErrorContext(ctx, "Missing tool", attr.SlogToolName(tc.Function.Name))
			} else {
				result, err := tool.Executor(ctx, tc.Function.Arguments)
				if err != nil {
					output = fmt.Sprintf("Error calling tool %q: %v", tc.Function.Name, err)
					c.logger.ErrorContext(ctx, "Tool error", attr.SlogToolName(tc.Function.Name), attr.SlogError(err))
				} else {
					output = result
				}
			}

			messages = append(messages, or.CreateChatMessagesTool(or.ChatToolMessage{
				Role:       or.ChatToolMessageRoleTool,
				Content:    or.CreateChatToolMessageContentStr(output),
				ToolCallID: tc.ID,
			}))
		}
	}
}

func (c *Client) LoadToolsetTools(
	ctx context.Context,
	projectID uuid.UUID,
	toolsetSlug string,
) ([]AgentTool, error) {
	// Get default environment for the toolset
	toolset, err := mv.DescribeToolset(ctx, c.logger, c.db, mv.ProjectID(projectID), mv.ToolsetSlug(toolsetSlug), &c.toolsetCache)
	if err != nil {
		return nil, err
	}

	if toolset.DefaultEnvironmentSlug == nil {
		return nil, fmt.Errorf("toolset has no default environment slug")
	}

	envSlug := string(*toolset.DefaultEnvironmentSlug)

	// Use MCP protocol to list tools
	mcpTools, err := c.toolsetLoader.ListTools(ctx, projectID, toolsetSlug, envSlug, true)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools via MCP: %w", err)
	}

	// Convert MCP tools to AgentTools with executors
	agentTools := make([]AgentTool, 0, len(mcpTools))
	for _, mcpTool := range mcpTools {
		// Capture variables for closure
		toolName := mcpTool.Name
		projID := projectID
		tslug := toolsetSlug
		eslug := envSlug

		// Create executor that calls tool via MCP
		executor := func(ctx context.Context, rawArgs string) (string, error) {
			// Call tool via MCP protocol
			result, err := c.toolsetLoader.CallTool(ctx, projID, tslug, eslug, toolName, json.RawMessage(rawArgs), true)
			if err != nil {
				return "", fmt.Errorf("MCP tool call error: %w", err)
			}

			// Format result from MCP content chunks
			return formatMCPResult(result), nil
		}

		agentTools = append(agentTools, AgentTool{
			Definition: openrouter.Tool{
				Type: "function",
				Function: &openrouter.FunctionDefinition{
					Name:        mcpTool.Name,
					Description: mcpTool.Description,
					Parameters:  mcpTool.InputSchema,
				},
			},
			Executor: executor,
		})
	}

	return agentTools, nil
}

// formatMCPResult converts MCP tool result to a string for the agent
func formatMCPResult(result *MCPToolResult) string {
	if result.IsError {
		// Return first text chunk as error message
		for _, chunk := range result.Content {
			if chunk.Type == "text" && chunk.Text != "" {
				return fmt.Sprintf("Error: %s", chunk.Text)
			}
		}
		return "Error: unknown error occurred"
	}

	// Combine all content chunks
	var parts []string
	for _, chunk := range result.Content {
		switch chunk.Type {
		case "text":
			if chunk.Text != "" {
				parts = append(parts, chunk.Text)
			}
		case "image", "audio":
			if chunk.Data != "" {
				parts = append(parts, fmt.Sprintf("[%s data: %s bytes, mime=%s]", chunk.Type, chunk.Data[:min(20, len(chunk.Data))], chunk.MimeType))
			}
		}
	}

	if len(parts) == 0 {
		return "" // Empty successful response
	}
	return strings.Join(parts, "\n")
}
