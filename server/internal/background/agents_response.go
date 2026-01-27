package background

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const orchestratorSystemPrompt = `You are an agent orchestrator responsible for solving complex problems by planning and executing tasks.

WORKFLOW:
1. At the start of execution, you MUST send back a plan to solve the request problem. This plan should outline your approach, break down the problem into steps, and identify which tools can help accomplish each step.
2. After sending the plan, execute your plan efficiently


TOOL SELECTION GUIDELINES:

SUB-AGENT TOOLS:
- Sub-agents appear as tools in your available tools list - look for tools whose descriptions indicate they are specialized agents
- Use sub-agent tools when their description matches your task or objective
- IMPORTANT: When multiple tasks can be handled by the same sub-agent, ALWAYS group them together into a single sub-agent call rather than making multiple separate calls
  * Combine all related tasks into one comprehensive TASK description
  * Include all relevant context in a single CONTEXT field
  * This reduces overhead and allows the sub-agent to optimize its workflow
  * Only make separate sub-agent calls if the tasks are truly unrelated or require different sub-agents
- Sub-agents are ideal when:
  * The task falls within a sub-agent's specialized domain
  * You need to accomplish a multi-step workflow that a sub-agent is designed to handle
  * A sub-agent can complete the task more efficiently than making multiple individual tool calls
- When calling a sub-agent tool:
  * Provide a clear TASK describing what you need accomplished (can include multiple related tasks)
  * Provide a clear CONTEXT with all relevant information the sub-agent needs
  * The sub-agent will use its specialized tools and instructions to complete the task

DIRECT TOOL CALLS:
- Use a tool directly when it exactly matches what you need to accomplish
- Check tool names and descriptions to find the best match for your task
- Direct tool calls are appropriate for single, focused operations
- If you have the exact tool you need, use it - don't delegate unnecessarily

MULTIPLE TOOLS FOR ONE OBJECTIVE:
- If you need multiple tools for a single objective:
  * Check if a sub-agent tool exists that can handle the complete workflow
  * If a relevant sub-agent tool exists, use it rather than making multiple sequential tool calls
  * When using a sub-agent, group ALL related tasks that the sub-agent can handle into ONE call
  * If no sub-agent tool matches, make direct tool calls in parallel when possible

PARALLEL EXECUTION:
When making tool calls (either directly or via sub-agents), prioritize parallel execution. If multiple operations don't depend on each other's results, execute them in parallel to improve efficiency.

Think step by step: Review all available tools (including sub-agent tools), match the right tool to each task, and always prefer parallel execution when possible.`

type AgentsResponseWorkflowParams struct {
	OrgID       string
	ProjectID   uuid.UUID
	Request     agents.ResponseRequest
	ShouldStore bool
}

func ExecuteAgentsResponseWorkflow(ctx context.Context, temporalClient client.Client, params AgentsResponseWorkflowParams) (client.WorkflowRun, error) {
	// Generate UUIDv7 for workflow ID, which will also be used as the response ID
	workflowID := uuid.Must(uuid.NewV7()).String()
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                string(TaskQueueMain),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowRunTimeout:       time.Minute * 15,
	}, AgentsResponseWorkflow, params)
}

func AgentsResponseWorkflow(ctx workflow.Context, params AgentsResponseWorkflowParams) (*agents.AgentsResponseWorkflowResult, error) {
	var a *Activities

	logger := workflow.GetLogger(ctx)
	logger.Info("executing agents response workflow",
		"org_id", params.OrgID,
		"project_id", params.ProjectID.String())

	// Get workflow ID to use as response ID
	workflowInfo := workflow.GetInfo(ctx)
	responseID := workflowInfo.WorkflowExecution.ID

	if err := workflow.SetQueryHandler(ctx, "org_id", func() (string, error) {
		return params.OrgID, nil
	}); err != nil {
		logger.Warn("failed to register query handler", "error", err)
	}

	if err := workflow.SetQueryHandler(ctx, "project_id", func() (uuid.UUID, error) {
		return params.ProjectID, nil
	}); err != nil {
		logger.Warn("failed to register query handler", "error", err)
	}

	// Register query handler to expose request parameters
	if err := workflow.SetQueryHandler(ctx, "request", func() (agents.ResponseRequest, error) {
		return params.Request, nil
	}); err != nil {
		logger.Warn("failed to register query handler", "error", err)
	}

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	// Record agent execution start
	startTime := workflow.Now(ctx)
	if params.ShouldStore {
		if err := workflow.ExecuteActivity(
			ctx,
			a.RecordAgentExecution,
			activities.RecordAgentExecutionInput{
				ExecutionID: responseID,
				ProjectID:   params.ProjectID,
				Status:      "in_progress",
				StartedAt:   startTime,
				CompletedAt: nil,
			},
		).Get(ctx, nil); err != nil {
			logger.Warn("failed to record agent execution start", "error", err)
		}
	}

	executionHistory := agents.ExecutionHistory{
		MainThreadToolCalls: 0,
		SubAgentToolCalls:   make(map[string]int),
	}

	// Preprocess: convert input to messages and load tools
	// This will also handle fetching and prepending previous response if PreviousResponseID is provided
	preprocessInput := activities.PreprocessAgentsInputInput{
		OrgID:      params.OrgID,
		ProjectID:  params.ProjectID,
		Request:    params.Request,
		ResponseID: responseID,
	}

	var preprocessOutput activities.PreprocessAgentsInputOutput
	var err error
	err = workflow.ExecuteActivity(
		ctx,
		a.PreprocessAgentsInput,
		preprocessInput,
	).Get(ctx, &preprocessOutput)
	if err != nil {
		logger.Error("failed to preprocess agents input", "error", err)
		errMsg := err.Error()
		result := buildErrorResponse(ctx, params, responseID, &errMsg, executionHistory, agents.InputDetails{Instructions: params.Request.Instructions, Prompt: ""})
		// Record agent execution completion (failed)
		if params.ShouldStore {
			completedTime := workflow.Now(ctx)
			_ = workflow.ExecuteActivity(
				ctx,
				a.RecordAgentExecution,
				activities.RecordAgentExecutionInput{
					ExecutionID: responseID,
					ProjectID:   params.ProjectID,
					Status:      "failed",
					StartedAt:   startTime,
					CompletedAt: &completedTime,
				},
			).Get(ctx, nil)
		}
		return result, nil
	}

	messages := preprocessOutput.Messages
	toolDefs := preprocessOutput.ToolDefs
	toolMetadata := preprocessOutput.ToolMetadata

	// Capture the user prompt from the last message before adding orchestrator system prompt
	var userPrompt string
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.Type == or.MessageTypeUser && lastMsg.UserMessage != nil && lastMsg.UserMessage.Content.Str != nil {
			userPrompt = *lastMsg.UserMessage.Content.Str
		}
	}

	inputDetails := agents.InputDetails{
		Instructions: params.Request.Instructions,
		Prompt:       userPrompt,
	}

	if len(messages) > 0 && messages[0].Type == or.MessageTypeSystem && messages[0].SystemMessage != nil && messages[0].SystemMessage.Content.Str != nil && *messages[0].SystemMessage.Content.Str != "" {
		combinedPrompt := orchestratorSystemPrompt + "\n\nUser Instructions:\n" + *messages[0].SystemMessage.Content.Str
		messages[0].SystemMessage.Content = or.CreateSystemMessageContentStr(combinedPrompt)
	} else {
		messages = append([]or.Message{
			or.CreateMessageSystem(or.SystemMessage{
				Content: or.CreateSystemMessageContentStr(orchestratorSystemPrompt),
				Name:    nil,
			}),
		}, messages...)
	}

	// Create sub-agent tools from request.SubAgents
	subAgentConfigs := make(map[string]agents.SubAgent)
	for _, subAgent := range params.Request.SubAgents {
		toolName := sanitizeToolName(subAgent.Name)
		if toolName == "" {
			toolName = "sub_agent"
		}
		subAgentTool := createSubAgentTool(toolName, subAgent.Description)
		toolDefs = append(toolDefs, subAgentTool)

		toolMetadata[toolName] = activities.ToolMetadata{
			ToolURN:         nil,
			EnvironmentSlug: subAgent.EnvironmentSlug,
			IsMCPTool:       false,
			ServerLabel:     "",
		}

		subAgentConfigs[toolName] = subAgent
	}

	var output []agents.OutputItem

	// Agentic loop: continue until we get a final message without tool calls
	for {
		modelCallInput := activities.ExecuteModelCallInput{
			OrgID:       params.OrgID,
			ProjectID:   params.ProjectID.String(),
			Messages:    messages,
			ToolDefs:    toolDefs,
			Temperature: params.Request.Temperature,
			Model:       params.Request.Model,
		}

		var modelCallOutput activities.ExecuteModelCallOutput
		err := workflow.ExecuteActivity(
			ctx,
			a.ExecuteModelCall,
			modelCallInput,
		).Get(ctx, &modelCallOutput)
		if err != nil {
			logger.Error("failed to execute model call", "error", err)
			errMsg := err.Error()
			return buildErrorResponse(ctx, params, responseID, &errMsg, executionHistory, inputDetails), nil
		}

		if modelCallOutput.Error != nil {
			logger.Error("model call returned error", "error", modelCallOutput.Error)
			errMsg := modelCallOutput.Error.Error()
			return buildErrorResponse(ctx, params, responseID, &errMsg, executionHistory, inputDetails), nil
		}

		msg := *modelCallOutput.Message
		messages = append(messages, msg)

		// Generate unique message ID for this chat completion
		messageID := "msg_" + workflow.Now(ctx).Format("20060102150405") + "_" + uuid.Must(uuid.NewV7()).String()[:8]

		// Add output item for every chat completion
		if msg.Type != or.MessageTypeAssistant {
			// Final message without tool calls
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
			break
		} else {
			// Message with tool calls - add as in_progress message
			output = append(output, agents.OutputMessage{
				Type:   "message",
				ID:     messageID,
				Status: "in_progress",
				Role:   "assistant",
				Content: []agents.OutputTextContent{
					{
						Type: "output_text",
						Text: openrouter.GetText(msg),
					},
				},
			})
		}

		// Execute tool calls in parallel
		type toolCallFuture struct {
			toolCall     or.ChatMessageToolCall
			future       workflow.Future
			toolCallName string
			isSubAgent   bool
		}

		toolCallFutures := make([]toolCallFuture, 0, len(msg.AssistantMessage.ToolCalls))
		for _, tc := range msg.AssistantMessage.ToolCalls {
			// Check if this is a sub-agent tool
			if subAgentConfig, isSubAgent := subAgentConfigs[tc.Function.Name]; isSubAgent {
				var args struct {
					Task    string `json:"task"`
					Context string `json:"context"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					logger.Error("failed to parse sub-agent arguments", "error", err)
					errMsg := fmt.Sprintf("Error parsing sub-agent arguments: %v", err)
					messages = append(messages, or.CreateMessageTool(or.ToolResponseMessage{
						Content:    or.CreateToolResponseMessageContentStr(errMsg),
						ToolCallID: tc.ID,
					}))
					continue
				}

				childWorkflowParams := SubAgentWorkflowParams{
					OrgID:        params.OrgID,
					ProjectID:    params.ProjectID,
					Goal:         args.Task,
					Instructions: subAgentConfig.Instructions,
					Context:      args.Context,
					ToolURNs:     subAgentConfig.Tools,
					Toolsets:     subAgentConfig.Toolsets,
					ToolMetadata: nil, // Will be loaded by the sub-agent workflow
					ParentID:     responseID,
					Temperature:  params.Request.Temperature,
					Model:        params.Request.Model,
					Environment:  subAgentConfig.EnvironmentSlug,
				}

				childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
					WorkflowID:         uuid.Must(uuid.NewV7()).String(),
					TaskQueue:          string(TaskQueueMain),
					WorkflowRunTimeout: 5 * time.Minute,
				})

				future := workflow.ExecuteChildWorkflow(childCtx, SubAgentWorkflow, childWorkflowParams)
				toolCallFutures = append(toolCallFutures, toolCallFuture{
					toolCall:     tc,
					future:       future,
					toolCallName: tc.Function.Name,
					isSubAgent:   true,
				})
			} else {
				toolMeta, ok := toolMetadata[tc.Function.Name]
				if !ok {
					logger.Warn("tool metadata not found", "tool_name", tc.Function.Name)
					toolMeta = activities.ToolMetadata{
						ToolURN:         nil,
						EnvironmentSlug: "",
						IsMCPTool:       false,
						ServerLabel:     "",
					}
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
					isSubAgent:   false,
				})
				executionHistory.MainThreadToolCalls++
			}
		}

		// Wait for all tool calls to complete and process results
		for _, tcf := range toolCallFutures {
			var toolCallOutput activities.ExecuteToolCallOutput

			if tcf.isSubAgent {
				// Handle sub-agent workflow result
				var subAgentResult SubAgentWorkflowResult
				err := tcf.future.Get(ctx, &subAgentResult)
				if err != nil {
					logger.Error("failed to execute sub-agent workflow", "error", err, "tool_name", tcf.toolCallName)
					errMsg := err.Error()
					toolCallOutput = activities.ExecuteToolCallOutput{
						ToolOutput: fmt.Sprintf("Error executing sub-agent: %v", err),
						ToolError:  &errMsg,
					}
				} else {
					toolCallOutput = activities.ExecuteToolCallOutput{
						ToolOutput: subAgentResult.Result,
						ToolError:  subAgentResult.Error,
					}
					if subAgentConfig, ok := subAgentConfigs[tcf.toolCallName]; ok {
						subAgentName := subAgentConfig.Name
						if subAgentName == "" {
							subAgentName = tcf.toolCallName
						}
						executionHistory.SubAgentToolCalls[subAgentName] += subAgentResult.ToolCallCount
					}
				}
			} else {
				err := tcf.future.Get(ctx, &toolCallOutput)
				if err != nil {
					logger.Error("failed to execute tool call", "error", err, "tool_name", tcf.toolCallName)
					errMsg := err.Error()
					toolCallOutput = activities.ExecuteToolCallOutput{
						ToolOutput: fmt.Sprintf("Error executing tool: %v", err),
						ToolError:  &errMsg,
					}
				}
			}

			toolMeta, ok := toolMetadata[tcf.toolCall.Function.Name]
			switch {
			case tcf.isSubAgent:
				output = append(output, agents.FunctionToolCall{
					Type:      "function_call",
					ID:        tcf.toolCall.ID,
					CallID:    tcf.toolCall.ID,
					Name:      tcf.toolCall.Function.Name,
					Arguments: tcf.toolCall.Function.Arguments,
					Status:    "completed",
				})
				output = append(output, agents.FunctionToolCallOutput{
					Type:   "function_call_output",
					CallID: tcf.toolCall.ID,
					Output: toolCallOutput.ToolOutput,
				})
			case ok && toolMeta.IsMCPTool:
				output = append(output, agents.MCPToolCall{
					Type:        "mcp_call",
					ID:          tcf.toolCall.ID,
					ServerLabel: toolMeta.ServerLabel,
					Name:        tcf.toolCall.Function.Name,
					Arguments:   tcf.toolCall.Function.Arguments,
					Output:      toolCallOutput.ToolOutput,
					Error:       toolCallOutput.ToolError,
					Status:      "completed",
				})
			}

			messages = append(messages, or.CreateMessageTool(or.ToolResponseMessage{
				Content:    or.CreateToolResponseMessageContentStr(toolCallOutput.ToolOutput),
				ToolCallID: tcf.toolCall.ID,
			}))
		}
	}

	if len(output) == 0 {
		errMsg := "agent loop completed without producing output"
		result := buildErrorResponse(ctx, params, responseID, &errMsg, executionHistory, inputDetails)
		// Record agent execution completion (failed)
		if params.ShouldStore {
			completedTime := workflow.Now(ctx)
			_ = workflow.ExecuteActivity(
				ctx,
				a.RecordAgentExecution,
				activities.RecordAgentExecutionInput{
					ExecutionID: responseID,
					ProjectID:   params.ProjectID,
					Status:      "failed",
					StartedAt:   startTime,
					CompletedAt: &completedTime,
				},
			).Get(ctx, nil)
		}
		return result, nil
	}

	// Extract result text from the last output message
	var result string
	if msg, ok := output[len(output)-1].(agents.OutputMessage); ok && len(msg.Content) > 0 {
		result = msg.Content[0].Text
	}

	// Record agent execution completion (success)
	if params.ShouldStore {
		completedTime := workflow.Now(ctx)
		err = workflow.ExecuteActivity(
			ctx,
			a.RecordAgentExecution,
			activities.RecordAgentExecutionInput{
				ExecutionID: responseID,
				ProjectID:   params.ProjectID,
				Status:      "completed",
				StartedAt:   startTime,
				CompletedAt: &completedTime,
			},
		).Get(ctx, nil)
		if err != nil {
			logger.Warn("failed to record agent execution completion", "error", err)
		}
	}

	return &agents.AgentsResponseWorkflowResult{
		ResponseOutput: agents.ResponseOutput{
			ID:                 responseID,
			Object:             "response",
			CreatedAt:          workflow.Now(ctx).Unix(),
			Status:             "completed",
			Error:              nil,
			Instructions:       params.Request.Instructions,
			Model:              params.Request.Model,
			Output:             agents.OutputItems(output),
			PreviousResponseID: params.Request.PreviousResponseID,
			Temperature:        getTemperature(params.Request.Temperature),
			Text: agents.ResponseText{
				Format: agents.TextFormat{Type: "text"},
			},
			Result: result,
		},
		ExecutionHistory: executionHistory,
		OrgID:            params.OrgID,
		ProjectID:        params.ProjectID,
		InputDetails:     inputDetails,
	}, nil
}

func buildErrorResponse(ctx workflow.Context, params AgentsResponseWorkflowParams, responseID string, errMsg *string, executionHistory agents.ExecutionHistory, inputDetails agents.InputDetails) *agents.AgentsResponseWorkflowResult {
	return &agents.AgentsResponseWorkflowResult{
		ResponseOutput: agents.ResponseOutput{
			ID:                 responseID,
			Object:             "response",
			CreatedAt:          workflow.Now(ctx).Unix(),
			Status:             "failed",
			Error:              errMsg,
			Instructions:       params.Request.Instructions,
			Model:              params.Request.Model,
			Output:             agents.OutputItems{},
			PreviousResponseID: params.Request.PreviousResponseID,
			Temperature:        getTemperature(params.Request.Temperature),
			Text: agents.ResponseText{
				Format: agents.TextFormat{Type: "text"},
			},
			Result: "",
		},
		ExecutionHistory: executionHistory,
		OrgID:            params.OrgID,
		ProjectID:        params.ProjectID,
		InputDetails:     inputDetails,
	}
}

func getTemperature(temp *float64) float64 {
	if temp != nil {
		return *temp
	}
	return 1.0
}

func createSubAgentTool(name, description string) openrouter.Tool {
	toolDescription := description
	if toolDescription == "" {
		toolDescription = "A specialized sub-agent"
	}

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The specific task or request to send to this sub-agent",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"description": "Additional conversation context or information relevant to completing the task",
			},
		},
		"required": []string{"task", "context"},
	}

	schemaJSON, _ := json.Marshal(schema)

	return openrouter.Tool{
		Type: "function",
		Function: &openrouter.FunctionDefinition{
			Name:        name,
			Description: toolDescription,
			Parameters:  schemaJSON,
		},
	}
}

func sanitizeToolName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")

	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		}
	}

	finalName := result.String()
	if len(finalName) > 50 {
		finalName = finalName[:50]
	}

	if len(finalName) > 0 && finalName[0] >= '0' && finalName[0] <= '9' {
		finalName = "agent_" + finalName
	}

	if finalName == "" {
		return "sub_agent"
	}

	return finalName
}
