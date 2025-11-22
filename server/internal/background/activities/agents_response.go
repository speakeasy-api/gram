package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type AgentsResponseInput struct {
	OrgID      string
	ProjectID  uuid.UUID
	Request    agents.ResponseRequest
	ResponseID string
}

type AgentsResponse struct {
	logger        *slog.Logger
	agentsService *agents.Service
}

func NewAgentsResponse(logger *slog.Logger, agentsService *agents.Service) *AgentsResponse {
	return &AgentsResponse{
		logger:        logger.With(attr.SlogComponent("agents-response-activity")),
		agentsService: agentsService,
	}
}

func (a *AgentsResponse) Execute(ctx context.Context, input AgentsResponseInput) (*agents.ResponseOutput, error) {
	a.logger.InfoContext(ctx, "executing agents response",
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
		errMsg := fmt.Sprintf("invalid input type: %T", input.Request.Input)
		return &agents.ResponseOutput{
			ID:                 input.ResponseID,
			Object:             "response",
			CreatedAt:          time.Now().Unix(),
			Status:             "failed",
			Error:              &errMsg,
			Instructions:       input.Request.Instructions,
			Model:              input.Request.Model,
			Output:             []agents.OutputItem{},
			PreviousResponseID: input.Request.PreviousResponseID,
			Temperature:        getTemperature(input.Request.Temperature),
			Text: agents.ResponseText{
				Format: agents.TextFormat{Type: "text"},
			},
			Usage: agents.ResponseUsage{
				InputTokens:  0,
				OutputTokens: 0,
				TotalTokens:  0,
			},
		}, nil
	}

	// Build agent options
	opts := agents.AgentChatOptions{
		SystemPrompt:    input.Request.Instructions,
		Toolsets:        input.Request.Toolsets,
		AdditionalTools: nil,
		AgentTimeout:    nil,
	}

	// Call ResponseAgent
	output, usage, err := a.agentsService.ResponseAgent(ctx, input.OrgID, input.ProjectID, messages, opts)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to execute agent", attr.SlogError(err))
		errMsg := err.Error()
		return &agents.ResponseOutput{
			ID:                 input.ResponseID,
			Object:             "response",
			CreatedAt:          time.Now().Unix(),
			Status:             "failed",
			Error:              &errMsg,
			Instructions:       input.Request.Instructions,
			Model:              input.Request.Model,
			Output:             []agents.OutputItem{},
			PreviousResponseID: input.Request.PreviousResponseID,
			Temperature:        getTemperature(input.Request.Temperature),
			Text: agents.ResponseText{
				Format: agents.TextFormat{Type: "text"},
			},
			Usage: agents.ResponseUsage{
				InputTokens:  0,
				OutputTokens: 0,
				TotalTokens:  0,
			},
		}, nil
	}

	// Build successful response
	return &agents.ResponseOutput{
		ID:                 input.ResponseID,
		Object:             "response",
		CreatedAt:          time.Now().Unix(),
		Status:             "completed",
		Error:              nil,
		Instructions:       input.Request.Instructions,
		Model:              input.Request.Model,
		Output:             output,
		PreviousResponseID: input.Request.PreviousResponseID,
		Temperature:        getTemperature(input.Request.Temperature),
		Text: agents.ResponseText{
			Format: agents.TextFormat{Type: "text"},
		},
		Usage: *usage,
	}, nil
}

func getTemperature(temp *float64) float64 {
	if temp != nil {
		return *temp
	}
	return 1.0
}
