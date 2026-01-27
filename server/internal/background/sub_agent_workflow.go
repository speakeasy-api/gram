package background

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type SubAgentWorkflowParams struct {
	OrgID        string
	ProjectID    uuid.UUID
	Goal         string
	Instructions string
	Context      string
	Toolsets     []agents.Toolset
	ToolURNs     []urn.Tool // Tool URNs to load
	ToolMetadata map[string]activities.ToolMetadata
	ParentID     string // ID of parent workflow
	Temperature  *float64
	Model        string
	Environment  string // Environment slug for loading tools
}

type SubAgentWorkflowResult struct {
	Result        string
	Output        agents.OutputItems
	Error         *string
	ToolCallCount int
}

func SubAgentWorkflow(ctx workflow.Context, params SubAgentWorkflowParams) (*SubAgentWorkflowResult, error) {
	var a *Activities

	logger := workflow.GetLogger(ctx)
	logger.Info("executing sub-agent workflow",
		"org_id", params.OrgID,
		"project_id", params.ProjectID.String(),
		"parent_id", params.ParentID)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 1 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	// System prompt for sub-agent
	systemPrompt := `You are a task-based agent with access to specific tools to achieve a specific objective. Your role is to complete the given objective using the tools provided to you.

You will receive:
- Goal: The specific objective you need to accomplish
- Context: Additional context or information relevant to completing the goal

Please use the tools available to you to complete the goal. Think step by step and use the tools as needed to achieve the objective.

PARALLEL EXECUTION:
When making tool calls, prioritize parallel execution. If multiple operations don't depend on each other's results, execute them in parallel to improve efficiency.`

	if params.Instructions != "" {
		systemPrompt += fmt.Sprintf("\n\nInstructions: %s", params.Instructions)
	}

	messages := []or.Message{
		or.CreateMessageSystem(or.SystemMessage{
			Content: or.CreateSystemMessageContentStr(systemPrompt),
			Name:    nil,
		}),
		or.CreateMessageUser(or.UserMessage{
			Content: or.CreateUserMessageContentStr(fmt.Sprintf("Goal: %s\n\nContext: %s", params.Goal, params.Context)),
			Name:    nil,
		}),
	}

	toolCallCount := 0

	toolsetRequests := make([]activities.ToolsetRequest, 0, len(params.Toolsets))
	for _, toolset := range params.Toolsets {
		toolsetRequests = append(toolsetRequests, activities.ToolsetRequest{
			ToolsetSlug:     toolset.ToolsetSlug,
			EnvironmentSlug: toolset.EnvironmentSlug,
		})
	}

	var loadOutput activities.LoadAgentToolsOutput
	err := workflow.ExecuteActivity(
		ctx,
		a.LoadAgentTools,
		activities.LoadAgentToolsInput{
			OrgID:           params.OrgID,
			ProjectID:       params.ProjectID,
			ToolURNs:        params.ToolURNs,
			Toolsets:        toolsetRequests,
			EnvironmentSlug: params.Environment,
		},
	).Get(ctx, &loadOutput)
	if err != nil {
		logger.Error("failed to load agent tools", "error", err)
		errMsg := err.Error()
		return &SubAgentWorkflowResult{
			Result:        "",
			Output:        agents.OutputItems{},
			Error:         &errMsg,
			ToolCallCount: toolCallCount,
		}, nil
	}

	toolDefs := loadOutput.ToolDefs
	toolMetadata := loadOutput.ToolMetadata

	var output agents.OutputItems

	// Agentic loop: continue until we get a final message without tool calls
	for {
		modelCallInput := activities.ExecuteModelCallInput{
			OrgID:       params.OrgID,
			ProjectID:   params.ProjectID.String(),
			Messages:    messages,
			ToolDefs:    toolDefs,
			Temperature: params.Temperature,
			Model:       params.Model,
		}

		var modelCallOutput activities.ExecuteModelCallOutput
		err := workflow.ExecuteActivity(
			ctx,
			a.ExecuteModelCall,
			modelCallInput,
		).Get(ctx, &modelCallOutput)
		if err != nil {
			logger.Error("failed to execute model call in sub-agent", "error", err)
			errMsg := err.Error()
			return &SubAgentWorkflowResult{
				Result:        "",
				Output:        agents.OutputItems{},
				Error:         &errMsg,
				ToolCallCount: toolCallCount,
			}, nil
		}

		if modelCallOutput.Error != nil {
			logger.Error("model call returned error in sub-agent", "error", modelCallOutput.Error)
			errMsg := modelCallOutput.Error.Error()
			return &SubAgentWorkflowResult{
				Result:        "",
				Output:        agents.OutputItems{},
				Error:         &errMsg,
				ToolCallCount: toolCallCount,
			}, nil
		}

		msg := *modelCallOutput.Message
		messages = append(messages, msg)

		if msg.Type != or.MessageTypeAssistant {
			// Add final message to output for history
			messageID := "msg_" + workflow.Now(ctx).Format("20060102150405")
			output = append(output, agents.OutputMessage{
				Type:   "message",
				ID:     messageID,
				Status: "completed",
				Role:   "assistant",
				Content: []agents.OutputTextContent{
					{
						Type: "output_text",
						Text: openrouter.GetText(msg),
					},
				},
			})
			// Return the final message content as the result, along with output for history
			return &SubAgentWorkflowResult{
				Result:        openrouter.GetText(msg),
				Output:        output,
				Error:         nil,
				ToolCallCount: toolCallCount,
			}, nil
		}

		// Execute tool calls in parallel
		type toolCallFuture struct {
			toolCall     or.ChatMessageToolCall
			future       workflow.Future
			toolCallName string
		}

		toolCallFutures := make([]toolCallFuture, 0, len(msg.AssistantMessage.ToolCalls))
		for _, tc := range msg.AssistantMessage.ToolCalls {
			toolMeta, ok := toolMetadata[tc.Function.Name]
			if !ok {
				toolMeta = activities.ToolMetadata{
					ToolURN:         nil,
					EnvironmentSlug: "",
					IsMCPTool:       false,
					ServerLabel:     "",
				}
				toolMetadata[tc.Function.Name] = toolMeta
			}

			toolCallInput := activities.ExecuteToolCallInput{
				OrgID:        params.OrgID,
				ProjectID:    params.ProjectID,
				ToolCall:     tc,
				ToolMetadata: toolMeta,
			}

			future := workflow.ExecuteActivity(
				ctx,
				a.ExecuteToolCall,
				toolCallInput,
			)

			toolCallFutures = append(toolCallFutures, toolCallFuture{
				toolCall:     tc,
				future:       future,
				toolCallName: tc.Function.Name,
			})
			toolCallCount++
		}

		// Wait for all tool calls to complete and process results
		for _, tcf := range toolCallFutures {
			var toolCallOutput activities.ExecuteToolCallOutput
			err := tcf.future.Get(ctx, &toolCallOutput)
			if err != nil {
				logger.Error("failed to execute tool call in sub-agent", "error", err, "tool_name", tcf.toolCallName)
				errMsg := err.Error()
				toolCallOutput = activities.ExecuteToolCallOutput{
					ToolOutput: fmt.Sprintf("Error executing tool: %v", err),
					ToolError:  &errMsg,
				}
			}

			toolMeta, ok := toolMetadata[tcf.toolCall.Function.Name]
			if ok && toolMeta.IsMCPTool {
				// MCP tool call in OpenAI Responses API format
				outputItem := agents.MCPToolCall{
					Type:        "mcp_call",
					ID:          tcf.toolCall.ID,
					ServerLabel: toolMeta.ServerLabel,
					Name:        tcf.toolCall.Function.Name,
					Arguments:   tcf.toolCall.Function.Arguments,
					Output:      toolCallOutput.ToolOutput,
					Error:       toolCallOutput.ToolError,
					Status:      "completed",
				}
				output = append(output, outputItem)
			}

			messages = append(messages, or.CreateMessageTool(or.ToolResponseMessage{
				Content:    or.CreateToolResponseMessageContentStr(toolCallOutput.ToolOutput),
				ToolCallID: tcf.toolCall.ID,
			}))
		}
	}
}
