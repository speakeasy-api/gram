package background

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type SubAgentWorkflowParams struct {
	OrgID        string
	ProjectID    uuid.UUID
	Goal         string
	Context      string
	Tools        []openrouter.Tool
	ToolMetadata map[string]activities.ToolMetadata
	ParentID     string // ID of parent workflow
}

type SubAgentWorkflowResult struct {
	Result string
	Output []agents.OutputItem
	Error  *string
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
- Tools: A set of tools you can use to complete the task

Please use the tools available to you to complete the goal. Think step by step and use the tools as needed to achieve the objective.

PARALLEL EXECUTION:
When making tool calls, prioritize parallel execution. If multiple operations don't depend on each other's results, execute them in parallel to improve efficiency.`

	// Initialize messages with system prompt, goal, and context
	messages := []openrouter.OpenAIChatMessage{
		{
			Role:       "system",
			Content:    systemPrompt,
			ToolCalls:  nil,
			ToolCallID: "",
			Name:       "",
		},
		{
			Role:       "user",
			Content:    fmt.Sprintf("Goal: %s\n\nContext: %s", params.Goal, params.Context),
			ToolCalls:  nil,
			ToolCallID: "",
			Name:       "",
		},
	}

	toolDefs := params.Tools
	// Use tool metadata passed from parent workflow
	toolMetadata := params.ToolMetadata
	if toolMetadata == nil {
		toolMetadata = make(map[string]activities.ToolMetadata)
	}
	var output []agents.OutputItem

	// Agentic loop: continue until we get a final message without tool calls
	for {
		// Execute model call
		modelCallInput := activities.ExecuteModelCallInput{
			OrgID:    params.OrgID,
			Messages: messages,
			ToolDefs: toolDefs,
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
				Result: "",
				Output: []agents.OutputItem{},
				Error:  &errMsg,
			}, nil
		}

		if modelCallOutput.Error != nil {
			logger.Error("model call returned error in sub-agent", "error", modelCallOutput.Error)
			errMsg := modelCallOutput.Error.Error()
			return &SubAgentWorkflowResult{
				Result: "",
				Output: []agents.OutputItem{},
				Error:  &errMsg,
			}, nil
		}

		msg := modelCallOutput.Message
		messages = append(messages, *msg)

		// No tool calls = final assistant message
		if len(msg.ToolCalls) == 0 {
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
						Text: msg.Content,
					},
				},
			})
			// Return the final message content as the result, along with output for history
			return &SubAgentWorkflowResult{
				Result: msg.Content,
				Output: output,
				Error:  nil,
			}, nil
		}

		// Execute tool calls in parallel
		type toolCallFuture struct {
			toolCall     openrouter.ToolCall
			future       workflow.Future
			toolCallName string
		}

		toolCallFutures := make([]toolCallFuture, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			// Get or create tool metadata for this tool
			toolMeta, ok := toolMetadata[tc.Function.Name]
			if !ok {
				toolMeta = activities.ToolMetadata{
					ToolsetSlug:     "",
					EnvironmentSlug: "",
					Headers:         nil,
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

			// Start activity without waiting - allows parallel execution
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
		}

		// Wait for all tool calls to complete and process results
		for _, tcf := range toolCallFutures {
			var toolCallOutput activities.ExecuteToolCallOutput
			err := tcf.future.Get(ctx, &toolCallOutput)
			if err != nil {
				logger.Error("failed to execute tool call in sub-agent", "error", err, "tool_name", tcf.toolCallName)
				// Continue with error output
				errMsg := err.Error()
				toolCallOutput = activities.ExecuteToolCallOutput{
					ToolOutput: fmt.Sprintf("Error executing tool: %v", err),
					ToolError:  &errMsg,
				}
			}

			// Construct output item based on tool metadata for history
			// Get tool metadata for this tool call
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

			// Add tool response to messages
			messages = append(messages, openrouter.OpenAIChatMessage{
				Role:       "tool",
				Content:    toolCallOutput.ToolOutput,
				Name:       tcf.toolCall.Function.Name,
				ToolCallID: tcf.toolCall.ID,
				ToolCalls:  nil,
			})
		}
	}
}
