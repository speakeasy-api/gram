package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type PreprocessAgentsInputInput struct {
	OrgID      string
	ProjectID  uuid.UUID
	Request    agents.ResponseRequest
	ResponseID string
}

type ToolMetadata struct {
	ToolsetSlug     string
	EnvironmentSlug string
	Headers         map[string]string
	IsMCPTool       bool
	ServerLabel     string
}

type PreprocessAgentsInputOutput struct {
	Messages     []openrouter.OpenAIChatMessage
	ToolDefs     []openrouter.Tool
	ToolMetadata map[string]ToolMetadata
}

type PreprocessAgentsInput struct {
	logger        *slog.Logger
	agentsService *agents.Service
}

func NewPreprocessAgentsInput(logger *slog.Logger, agentsService *agents.Service) *PreprocessAgentsInput {
	return &PreprocessAgentsInput{
		logger:        logger.With(attr.SlogComponent("preprocess-agents-input-activity")),
		agentsService: agentsService,
	}
}

func (a *PreprocessAgentsInput) Do(ctx context.Context, input PreprocessAgentsInputInput) (*PreprocessAgentsInputOutput, error) {
	a.logger.InfoContext(ctx, "preprocessing agents input",
		attr.SlogOrganizationID(input.OrgID),
		attr.SlogProjectID(input.ProjectID.String()),
		attr.SlogProjectSlug(input.Request.ProjectSlug))

	// Convert input to message history
	var messages []openrouter.OpenAIChatMessage
	switch requestInput := input.Request.Input.(type) {
	case string:
		// Single string input becomes a user message
		messages = append(messages, openrouter.OpenAIChatMessage{
			Role:       "user",
			Content:    requestInput,
			ToolCalls:  nil,
			ToolCallID: "",
			Name:       "",
		})
	case []interface{}:
		// Array of output items - can be agents.OutputMessage or agents.MCPToolCall
		for _, item := range requestInput {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			itemType, _ := itemMap["type"].(string)

			switch itemType {
			case "message":
				// agents.OutputMessage - convert to chat message
				role, _ := itemMap["role"].(string)
				// to chat completions mapping
				if role == "role" {
					role = "user"
				}

				var textContent string
				// Content can be either a string or an array
				switch content := itemMap["content"].(type) {
				case string:
					textContent = content
				case []interface{}:
					for _, c := range content {
						cMap, ok := c.(map[string]interface{})
						if !ok {
							continue
						}
						if cMap["type"] == "output_text" {
							textContent, _ = cMap["text"].(string)
							break
						}
					}
				}

				messages = append(messages, openrouter.OpenAIChatMessage{
					Role:       role,
					Content:    textContent,
					ToolCalls:  nil,
					ToolCallID: "",
					Name:       "",
				})

			case "mcp_call":
				// agents.MCPToolCall - convert to assistant message with tool_calls + tool response
				id, _ := itemMap["id"].(string)
				name, _ := itemMap["name"].(string)
				arguments, _ := itemMap["arguments"].(string)
				output, _ := itemMap["output"].(string)

				// Add assistant message with tool_calls
				messages = append(messages, openrouter.OpenAIChatMessage{
					Role:       "assistant",
					Content:    "",
					ToolCallID: "",
					Name:       "",
					ToolCalls: []openrouter.ToolCall{
						{
							Index: 0,
							ID:    id,
							Type:  "function",
							Function: openrouter.ToolCallFunction{
								Name:      name,
								Arguments: arguments,
							},
						},
					},
				})

				// Add tool response message
				messages = append(messages, openrouter.OpenAIChatMessage{
					Role:       "tool",
					Content:    output,
					Name:       name,
					ToolCallID: id,
					ToolCalls:  nil,
				})
			default:
				role, _ := itemMap["role"].(string)
				// to chat completions mapping
				if role == "role" {
					role = "user"
				}
				content, _ := itemMap["content"].(string)
				messages = append(messages, openrouter.OpenAIChatMessage{
					Role:       role,
					Content:    content,
					ToolCalls:  nil,
					ToolCallID: "",
					Name:       "",
				})
			}
		}
	default:
		return nil, fmt.Errorf("invalid input type: %T", input.Request.Input)
	}

	// Add system prompt if provided
	if input.Request.Instructions != nil {
		messages = append([]openrouter.OpenAIChatMessage{
			{
				Role:       "system",
				Content:    *input.Request.Instructions,
				ToolCalls:  nil,
				ToolCallID: "",
				Name:       "",
			},
		}, messages...)
	}

	// Load tools
	opts := agents.AgentChatOptions{
		SystemPrompt:    input.Request.Instructions,
		Toolsets:        input.Request.Toolsets,
		AdditionalTools: nil,
		AgentTimeout:    nil,
	}

	agentTools := opts.AdditionalTools
	for _, toolset := range opts.Toolsets {
		toolsetTools, err := a.agentsService.LoadToolsetTools(ctx, input.ProjectID, toolset.ToolsetSlug, toolset.EnvironmentSlug, toolset.Headers)
		if err != nil {
			return nil, fmt.Errorf("failed to load toolset %q: %w", toolset.ToolsetSlug, err)
		}
		agentTools = append(agentTools, toolsetTools...)
	}

	toolDefs := make([]openrouter.Tool, 0, len(agentTools))
	toolMetadata := make(map[string]ToolMetadata)

	// Build a map of toolset slug to toolset config for quick lookup
	toolsetConfigMap := make(map[string]agents.Toolset)
	for _, toolset := range opts.Toolsets {
		toolsetConfigMap[toolset.ToolsetSlug] = toolset
	}

	for _, t := range agentTools {
		if t.Definition.Function != nil {
			toolDefs = append(toolDefs, t.Definition)

			// Find which toolset this tool belongs to using ServerLabel
			var toolsetSlug, envSlug string
			var headers map[string]string
			if t.IsMCPTool && t.ServerLabel != "" {
				// ServerLabel should match the toolset slug
				if toolset, ok := toolsetConfigMap[t.ServerLabel]; ok {
					toolsetSlug = toolset.ToolsetSlug
					envSlug = toolset.EnvironmentSlug
					headers = toolset.Headers
				} else {
					// Fallback: try to find by matching server label directly
					toolsetSlug = t.ServerLabel
					// Use the first toolset's config as fallback
					if len(opts.Toolsets) > 0 {
						envSlug = opts.Toolsets[0].EnvironmentSlug
						headers = opts.Toolsets[0].Headers
					}
				}
			}

			toolMetadata[t.Definition.Function.Name] = ToolMetadata{
				ToolsetSlug:     toolsetSlug,
				EnvironmentSlug: envSlug,
				Headers:         headers,
				IsMCPTool:       t.IsMCPTool,
				ServerLabel:     t.ServerLabel,
			}
		}
	}

	return &PreprocessAgentsInputOutput{
		Messages:     messages,
		ToolDefs:     toolDefs,
		ToolMetadata: toolMetadata,
	}, nil
}
