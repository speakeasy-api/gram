package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/speakeasy-api/gram/server/internal/agentworkflows/agents"
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
	IsMCPTool       bool
	ServerLabel     string
}

type PreprocessAgentsInputOutput struct {
	Messages     []or.Message
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
	// If PreviousResponseID is provided, fetch its output and convert to messages
	request := input.Request
	var messages []or.Message
	if request.PreviousResponseID != nil && *request.PreviousResponseID != "" {

		workflowRun := a.temporalClient.GetWorkflow(ctx, *request.PreviousResponseID, "")

		var prevWorkflowResult agents.AgentsResponseWorkflowResult
		if err := workflowRun.Get(ctx, &prevWorkflowResult); err != nil {
			return nil, fmt.Errorf("failed to get previous response workflow result: %w", err)
		}

		prevResponse := prevWorkflowResult.ResponseOutput

		if prevWorkflowResult.OrgID != input.OrgID {
			return nil, fmt.Errorf("previous response %s is not from the same org as the current request", *request.PreviousResponseID)
		}

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
		messages = append([]or.Message{
			or.CreateMessageSystem(or.SystemMessage{
				Content: or.CreateSystemMessageContentStr(*request.Instructions),
				Name:    nil,
			}),
		}, messages...)
	}

	var agentTools []agents.AgentTool
	for _, toolset := range request.Toolsets {
		toolsetTools, err := a.agentsService.LoadToolsetTools(ctx, input.ProjectID, toolset.ToolsetSlug, toolset.EnvironmentSlug)
		if err != nil {
			return nil, fmt.Errorf("failed to load toolset %q: %w", toolset.ToolsetSlug, err)
		}
		agentTools = append(agentTools, toolsetTools...)
	}

	toolDefs := make([]openrouter.Tool, 0, len(agentTools))
	toolMetadata := make(map[string]ToolMetadata)

	// Build a map of toolset slug to toolset config for quick lookup
	toolsetConfigMap := make(map[string]agents.Toolset)
	for _, toolset := range request.Toolsets {
		toolsetConfigMap[toolset.ToolsetSlug] = toolset
	}

	for _, t := range agentTools {
		if t.Definition.Function != nil {
			toolDefs = append(toolDefs, t.Definition)

			var envSlug string
			if t.IsMCPTool && t.ServerLabel != "" {
				if toolset, ok := toolsetConfigMap[t.ServerLabel]; ok {
					envSlug = toolset.EnvironmentSlug
				}
			}

			toolMetadata[t.Definition.Function.Name] = ToolMetadata{
				ToolURN:         t.ToolURN,
				EnvironmentSlug: envSlug,
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
func convertInputToMessages(input agents.ResponseInput) ([]or.Message, error) {
	if input == nil {
		return []or.Message{}, nil
	}

	var messages []or.Message

	switch requestInput := input.(type) {
	case string:
		messages = append(messages, or.CreateMessageUser(or.UserMessage{
			Content: or.CreateUserMessageContentStr(requestInput),
			Name:    nil,
		}))
	case []any:
		// Array of output items - can be agents.OutputMessage or agents.MCPToolCall

		for _, item := range requestInput {
			itemMap, ok := item.(map[string]any)
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
				case []any:
					for _, c := range content {
						cMap, ok := c.(map[string]any)
						if !ok {
							continue
						}
						if cMap["type"] == "output_text" {
							textContent, _ = cMap["text"].(string)
							break
						}
					}
				}

				var msg or.Message
				switch or.MessageType(role) {
				case or.MessageTypeSystem:
					msg = or.CreateMessageSystem(or.SystemMessage{
						Content: or.CreateSystemMessageContentStr(textContent),
						Name:    nil,
					})
				case or.MessageTypeUser:
					msg = or.CreateMessageUser(or.UserMessage{
						Content: or.CreateUserMessageContentStr(textContent),
						Name:    nil,
					})
				case or.MessageTypeDeveloper:
					msg = or.CreateMessageDeveloper(or.MessageDeveloper{
						Content: or.CreateMessageContentStr(textContent),
						Name:    nil,
					})
				case or.MessageTypeAssistant:
					msg = or.CreateMessageAssistant(or.AssistantMessage{
						Content: optionalnullable.From(
							new(or.CreateAssistantMessageContentStr(textContent)),
						),
						Name:             nil,
						ToolCalls:        nil,
						Refusal:          nil,
						Reasoning:        nil,
						ReasoningDetails: nil,
						Images:           nil,
					})
				case or.MessageTypeTool:
					msg = or.CreateMessageTool(or.ToolResponseMessage{
						Content:    or.CreateToolResponseMessageContentStr(textContent),
						ToolCallID: "",
					})
				default:
					return nil, fmt.Errorf("unknown message role: %s", role)
				}
				messages = append(messages, msg)
			case "mcp_call":
				// agents.MCPToolCall - convert to assistant message with tool_calls + tool response
				id, _ := itemMap["id"].(string)
				name, _ := itemMap["name"].(string)
				arguments, _ := itemMap["arguments"].(string)
				output, _ := itemMap["output"].(string)

				// Add assistant message with tool_calls
				messages = append(messages, or.CreateMessageAssistant(or.AssistantMessage{
					Content: nil,
					Name:    nil,
					ToolCalls: []or.ChatMessageToolCall{
						{
							ID: id,
							Function: or.ChatMessageToolCallFunction{
								Name:      name,
								Arguments: arguments,
							},
						},
					},
					Refusal:          nil,
					Reasoning:        nil,
					ReasoningDetails: nil,
					Images:           nil,
				}))

				// Add tool response message
				messages = append(messages, or.CreateMessageTool(or.ToolResponseMessage{
					Content:    or.CreateToolResponseMessageContentStr(output),
					ToolCallID: id,
				}))
			case "function_call":
				// function_call - convert to assistant message with tool_calls
				toolCallID, _ := itemMap["call_id"].(string)
				name, _ := itemMap["name"].(string)
				arguments, _ := itemMap["arguments"].(string)

				messages = append(messages, or.CreateMessageAssistant(or.AssistantMessage{
					Content: nil,
					Name:    nil,
					ToolCalls: []or.ChatMessageToolCall{
						{
							ID: toolCallID,
							Function: or.ChatMessageToolCallFunction{
								Name:      name,
								Arguments: arguments,
							},
						},
					},
					Refusal:          nil,
					Reasoning:        nil,
					ReasoningDetails: nil,
					Images:           nil,
				}))

			case "function_call_output":
				// function_call_output - convert to tool response message
				callID, _ := itemMap["call_id"].(string)
				output, _ := itemMap["output"].(string)

				// Add tool response message
				messages = append(messages, or.CreateMessageTool(or.ToolResponseMessage{
					Content:    or.CreateToolResponseMessageContentStr(output),
					ToolCallID: callID,
				}))

			default:
				// No type field - treat as simple message object (role/content)
				rawRole, _ := itemMap["role"].(string)
				role := rawRole
				// to chat completions mapping
				if role == "role" {
					role = "user"
				}
				content, _ := itemMap["content"].(string)

				var msg or.Message
				switch or.MessageType(role) {
				case or.MessageTypeAssistant:
					msg = or.CreateMessageAssistant(or.AssistantMessage{
						Content: optionalnullable.From(
							new(or.CreateAssistantMessageContentStr(content)),
						),
						Name:             nil,
						ToolCalls:        nil,
						Refusal:          nil,
						Reasoning:        nil,
						ReasoningDetails: nil,
						Images:           nil,
					})
				case or.MessageTypeDeveloper:
					msg = or.CreateMessageDeveloper(or.MessageDeveloper{
						Content: or.CreateMessageContentStr(content),
						Name:    nil,
					})
				case or.MessageTypeSystem:
					msg = or.CreateMessageSystem(or.SystemMessage{
						Content: or.CreateSystemMessageContentStr(content),
						Name:    nil,
					})
				case or.MessageTypeTool:
					msg = or.CreateMessageTool(or.ToolResponseMessage{
						Content:    or.CreateToolResponseMessageContentStr(content),
						ToolCallID: "",
					})
				case or.MessageTypeUser:
					msg = or.CreateMessageUser(or.UserMessage{
						Content: or.CreateUserMessageContentStr(content),
						Name:    nil,
					})
				default:
					return nil, fmt.Errorf("unknown message role: %s", rawRole)
				}

				messages = append(messages, msg)
			}
		}

	default:
		return nil, fmt.Errorf("invalid input type: %T", input)
	}

	return messages, nil
}

// convertOutputItemsToMessages converts output items (OutputMessage, MCPToolCall) to chat messages
func convertOutputItemsToMessages(output []agents.OutputItem) []or.Message {
	var messages []or.Message

	for _, item := range output {
		switch item := item.(type) {
		case agents.OutputMessage:
			// Extract text content from the first content item
			var textContent string
			if len(item.Content) > 0 {
				textContent = item.Content[0].Text
			}

			var msg or.Message
			switch or.MessageType(item.Role) {
			case or.MessageTypeAssistant:
				msg = or.CreateMessageAssistant(or.AssistantMessage{
					Content: optionalnullable.From(
						new(or.CreateAssistantMessageContentStr(textContent)),
					),
					Name:             nil,
					ToolCalls:        nil,
					Refusal:          nil,
					Reasoning:        nil,
					ReasoningDetails: nil,
					Images:           nil,
				})
			case or.MessageTypeDeveloper:
				msg = or.CreateMessageDeveloper(or.MessageDeveloper{
					Content: or.CreateMessageContentStr(textContent),
					Name:    nil,
				})
			case or.MessageTypeSystem:
				msg = or.CreateMessageSystem(or.SystemMessage{
					Content: or.CreateSystemMessageContentStr(textContent),
					Name:    nil,
				})
			case or.MessageTypeTool:
				msg = or.CreateMessageTool(or.ToolResponseMessage{
					Content:    or.CreateToolResponseMessageContentStr(textContent),
					ToolCallID: "",
				})
			case or.MessageTypeUser:
				msg = or.CreateMessageUser(or.UserMessage{
					Content: or.CreateUserMessageContentStr(textContent),
					Name:    nil,
				})
			default:
				panic("unexpected or.MessageType")
			}

			messages = append(messages, msg)

		case agents.MCPToolCall:
			// Add assistant message with tool_calls
			messages = append(messages, or.CreateMessageAssistant(or.AssistantMessage{
				ToolCalls: []or.ChatMessageToolCall{
					{
						ID: item.ID,
						Function: or.ChatMessageToolCallFunction{
							Name:      item.Name,
							Arguments: item.Arguments,
						},
					},
				},
				Content:          nil,
				Name:             nil,
				Refusal:          nil,
				Reasoning:        nil,
				ReasoningDetails: nil,
				Images:           nil,
			}))

			// Add tool response message
			messages = append(messages, or.CreateMessageTool(or.ToolResponseMessage{
				Content:    or.CreateToolResponseMessageContentStr(item.Output),
				ToolCallID: item.ID,
			}))
		}
	}

	return messages
}
