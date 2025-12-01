package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type PreprocessAgentsInputInput struct {
	OrgID      string
	ProjectID  uuid.UUID
	Request    agents.ResponseRequest
	ResponseID string
}

type ToolMetadata struct {
	ToolURN         *urn.Tool
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
	logger         *slog.Logger
	agentsService  *agents.Service
	temporalClient client.Client
}

func NewPreprocessAgentsInput(logger *slog.Logger, agentsService *agents.Service, temporalClient client.Client) *PreprocessAgentsInput {
	return &PreprocessAgentsInput{
		logger:         logger.With(attr.SlogComponent("preprocess-agents-input-activity")),
		agentsService:  agentsService,
		temporalClient: temporalClient,
	}
}

func (a *PreprocessAgentsInput) Do(ctx context.Context, input PreprocessAgentsInputInput) (*PreprocessAgentsInputOutput, error) {
	a.logger.InfoContext(ctx, "preprocessing agents input",
		attr.SlogOrganizationID(input.OrgID),
		attr.SlogProjectID(input.ProjectID.String()),
	)
	// If PreviousResponseID is provided, fetch its output and convert to messages
	request := input.Request
	var messages []openrouter.OpenAIChatMessage
	if request.PreviousResponseID != nil && *request.PreviousResponseID != "" {

		// Get the workflow run for the previous response
		workflowRun := a.temporalClient.GetWorkflow(ctx, *request.PreviousResponseID, "")

		var prevWorkflowResult agents.AgentsResponseWorkflowResult
		if err := workflowRun.Get(ctx, &prevWorkflowResult); err != nil {
			return nil, fmt.Errorf("failed to get previous response workflow result: %w", err)
		}

		prevResponse := prevWorkflowResult.ResponseOutput

		if prevWorkflowResult.OrgID != input.OrgID {
			return nil, fmt.Errorf("previous response %s is not from the same org as the current request", *request.PreviousResponseID)
		}

		// Check if the previous response completed successfully
		if prevResponse.Status != "completed" {
			return nil, fmt.Errorf("previous response %s has status %q, expected 'completed'", *request.PreviousResponseID, prevResponse.Status)
		}

		// Convert previous output to messages
		prevMessages := convertOutputItemsToMessages([]agents.OutputItem(prevResponse.Output))
		messages = append(messages, prevMessages...)
	}

	// Convert current input to messages and append
	currentMessages, err := convertInputToMessages(request.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}
	messages = append(messages, currentMessages...)

	// Add system prompt if provided
	if request.Instructions != nil {
		messages = append([]openrouter.OpenAIChatMessage{
			{
				Role:       "system",
				Content:    *request.Instructions,
				ToolCalls:  nil,
				ToolCallID: "",
				Name:       "",
			},
		}, messages...)
	}

	// Load tools
	opts := agents.AgentChatOptions{
		Toolsets:        request.Toolsets,
		AdditionalTools: nil,
		AgentTimeout:    nil,
	}

	agentTools := opts.AdditionalTools
	for _, toolset := range opts.Toolsets {
		toolsetTools, err := a.agentsService.LoadToolsetTools(ctx, input.ProjectID, toolset.ToolsetSlug, toolset.EnvironmentSlug, nil)
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
			var envSlug string
			if t.IsMCPTool && t.ServerLabel != "" {
				// ServerLabel should match the toolset slug
				if toolset, ok := toolsetConfigMap[t.ServerLabel]; ok {
					envSlug = toolset.EnvironmentSlug
				} else if len(opts.Toolsets) > 0 {
					// Use the first toolset's config as fallback
					envSlug = opts.Toolsets[0].EnvironmentSlug
				}
			}

			toolMetadata[t.Definition.Function.Name] = ToolMetadata{
				ToolURN:         t.ToolURN,
				EnvironmentSlug: envSlug,
				Headers:         nil,
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

// convertInputToMessages converts ResponseInput (any JSON value) directly to chat messages
// TODO: Figure out some union like approach to do this in a cleaner way
func convertInputToMessages(input agents.ResponseInput) ([]openrouter.OpenAIChatMessage, error) {
	if input == nil {
		return []openrouter.OpenAIChatMessage{}, nil
	}

	var messages []openrouter.OpenAIChatMessage

	switch requestInput := input.(type) {
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

			case "function_call":
				// function_call - convert to assistant message with tool_calls
				toolCallID, _ := itemMap["call_id"].(string)
				name, _ := itemMap["name"].(string)
				arguments, _ := itemMap["arguments"].(string)
				messages = append(messages, openrouter.OpenAIChatMessage{
					Role:       "assistant",
					Content:    "",
					ToolCallID: "",
					Name:       "",
					ToolCalls: []openrouter.ToolCall{
						{
							Index: 0,
							ID:    toolCallID,
							Type:  "function",
							Function: openrouter.ToolCallFunction{
								Name:      name,
								Arguments: arguments,
							},
						},
					},
				})

			case "function_call_output":
				// function_call_output - convert to tool response message
				callID, _ := itemMap["call_id"].(string)
				output, _ := itemMap["output"].(string)

				// Add tool response message
				messages = append(messages, openrouter.OpenAIChatMessage{
					Role:       "tool",
					Content:    output,
					Name:       "",
					ToolCallID: callID,
					ToolCalls:  nil,
				})

			default:
				// No type field - treat as simple message object (role/content)
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
		return nil, fmt.Errorf("invalid input type: %T", input)
	}

	return messages, nil
}

// convertOutputItemsToMessages converts output items (OutputMessage, MCPToolCall) to chat messages
func convertOutputItemsToMessages(output []agents.OutputItem) []openrouter.OpenAIChatMessage {
	var messages []openrouter.OpenAIChatMessage

	for _, item := range output {
		switch item := item.(type) {
		case agents.OutputMessage:
			// Extract text content from the first content item
			var textContent string
			if len(item.Content) > 0 {
				textContent = item.Content[0].Text
			}

			messages = append(messages, openrouter.OpenAIChatMessage{
				Role:       item.Role,
				Content:    textContent,
				ToolCalls:  nil,
				ToolCallID: "",
				Name:       "",
			})

		case agents.MCPToolCall:
			// Add assistant message with tool_calls
			messages = append(messages, openrouter.OpenAIChatMessage{
				Role:       "assistant",
				Content:    "",
				ToolCallID: "",
				Name:       "",
				ToolCalls: []openrouter.ToolCall{
					{
						Index: 0,
						ID:    item.ID,
						Type:  "function",
						Function: openrouter.ToolCallFunction{
							Name:      item.Name,
							Arguments: item.Arguments,
						},
					},
				},
			})

			// Add tool response message
			messages = append(messages, openrouter.OpenAIChatMessage{
				Role:       "tool",
				Content:    item.Output,
				Name:       item.Name,
				ToolCallID: item.ID,
				ToolCalls:  nil,
			})
		}
	}

	return messages
}
