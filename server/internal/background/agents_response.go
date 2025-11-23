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

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type AgentsResponseWorkflowParams struct {
	OrgID     string
	ProjectID uuid.UUID
	Request   agents.ResponseRequest
}

func ExecuteAgentsResponseWorkflow(ctx context.Context, temporalClient client.Client, params AgentsResponseWorkflowParams) (client.WorkflowRun, error) {
	// Generate UUIDv7 for workflow ID, which will also be used as the response ID
	workflowID := uuid.Must(uuid.NewV7()).String()
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                string(TaskQueueMain),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowRunTimeout:       time.Minute * 60,
	}, AgentsResponseWorkflow, params)
}

func AgentsResponseWorkflow(ctx workflow.Context, params AgentsResponseWorkflowParams) (*agents.ResponseOutput, error) {
	var a *Activities

	logger := workflow.GetLogger(ctx)
	logger.Info("executing agents response workflow",
		"org_id", params.OrgID,
		"project_id", params.ProjectID.String())

	// Get workflow ID to use as response ID
	workflowInfo := workflow.GetInfo(ctx)
	responseID := workflowInfo.WorkflowExecution.ID

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 1 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	// Preprocess: convert input to messages and load tools
	preprocessInput := activities.PreprocessAgentsInputInput{
		OrgID:      params.OrgID,
		ProjectID:  params.ProjectID,
		Request:    params.Request,
		ResponseID: responseID,
	}

	var preprocessOutput activities.PreprocessAgentsInputOutput
	err := workflow.ExecuteActivity(
		ctx,
		a.PreprocessAgentsInput,
		preprocessInput,
	).Get(ctx, &preprocessOutput)
	if err != nil {
		logger.Error("failed to preprocess agents input", "error", err)
		errMsg := err.Error()
		return buildErrorResponse(ctx, params, responseID, &errMsg), nil
	}

	messages := preprocessOutput.Messages
	toolDefs := preprocessOutput.ToolDefs
	toolMetadata := preprocessOutput.ToolMetadata

	// Add orchestrator system prompt at the beginning
	orchestratorSystemPrompt := `You are an agent orchestrator responsible for solving complex problems by planning and executing tasks.

WORKFLOW:
1. At the start of execution, create a plan to solve the request problem
2. Break down the problem into steps and identify which tools or sub-agents can help accomplish each step
3. Execute your plan efficiently

SUB-AGENT DELEGATION (STRONGLY RECOMMENDED):
You have access to a spawn_sub_agent tool that allows you to delegate work to specialized sub-agents. 

Whenever you need to make MULTIPLE tool calls for a SINGLE objective or related task, you should STRONGLY CONSIDER using spawn_sub_agent instead of making individual tool calls yourself.

USE spawn_sub_agent when:
- Multiple tool calls are part of a single workflow or process
- You have a group of related operations that can be bundled together
- A task requires multiple steps that logically belong together
- You want to offload a complete workflow to a focused sub-agent

EXAMPLES of when to use spawn_sub_agent:
- "Query multiple tools to build a complete picture" → Delegate the group
- Any scenario where you're about to make sequential related tool calls for one objective

When delegating to a sub-agent:
- Provide a clear GOAL describing what the sub-agent should accomplish
- Provide CONTEXT with all relevant information the sub-agent needs
- Provide the specific TOOL NAMES (as strings) the sub-agent needs to complete the goal
- Sub-agents can run in parallel, so delegate independent tasks simultaneously

DIRECT TOOL CALLS:
make direct tool calls when:
- It's a single, isolated operation with no follow-up steps
- The tool call is an independent action and not part of a larger workflow
- You need immediate results before deciding next steps

PARALLEL EXECUTION:
When making tool calls (either directly or via sub-agents), prioritize parallel execution. If multiple operations don't depend on each other's results, execute them in parallel to improve efficiency.

Think step by step, plan your approach, and STRONGLY PREFER delegating grouped work to sub-agents rather than making multiple sequential tool calls yourself.`

	// Combine orchestrator prompt with any existing system message from instructions
	if len(messages) > 0 && messages[0].Role == "system" {
		// Merge with existing system message, labeling user instructions
		combinedPrompt := orchestratorSystemPrompt + "\n\nUser Instructions:\n" + messages[0].Content
		messages[0].Content = combinedPrompt
	} else {
		// Prepend system prompt if no existing system message
		messages = append([]openrouter.OpenAIChatMessage{
			{
				Role:       "system",
				Content:    orchestratorSystemPrompt,
				ToolCalls:  nil,
				ToolCallID: "",
				Name:       "",
			},
		}, messages...)
	}

	// Add spawn_sub_agent tool to the main agent
	spawnSubAgentTool := getSpawnSubAgentTool()
	toolDefs = append(toolDefs, spawnSubAgentTool)
	// Mark it as a special tool (not MCP) so it gets handled in the workflow
	toolMetadata["spawn_sub_agent"] = activities.ToolMetadata{
		ToolsetSlug:     "",
		EnvironmentSlug: "",
		Headers:         nil,
		IsMCPTool:       false,
		ServerLabel:     "",
	}

	var output []agents.OutputItem
	var totalUsage agents.ResponseUsage

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
			logger.Error("failed to execute model call", "error", err)
			errMsg := err.Error()
			return buildErrorResponse(ctx, params, responseID, &errMsg), nil
		}

		if modelCallOutput.Error != nil {
			logger.Error("model call returned error", "error", modelCallOutput.Error)
			errMsg := modelCallOutput.Error.Error()
			return buildErrorResponse(ctx, params, responseID, &errMsg), nil
		}

		msg := modelCallOutput.Message
		messages = append(messages, *msg)

		// Generate unique message ID for this chat completion
		messageID := "msg_" + workflow.Now(ctx).Format("20060102150405") + "_" + uuid.Must(uuid.NewV7()).String()[:8]

		// Add output item for every chat completion
		if len(msg.ToolCalls) == 0 {
			// Final message without tool calls
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
						Text: msg.Content,
					},
				},
			})
		}

		// Execute tool calls in parallel
		type toolCallFuture struct {
			toolCall     openrouter.ToolCall
			future       workflow.Future
			toolCallName string
			isSubAgent   bool
		}

		toolCallFutures := make([]toolCallFuture, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			// Check if this is spawn_sub_agent tool
			if tc.Function.Name == "spawn_sub_agent" {
				// Handle spawn_sub_agent by executing child workflow
				logger.Info("parsing spawn_sub_agent arguments", "arguments", tc.Function.Arguments)
				childWorkflowParams, err := parseSpawnSubAgentArgs(tc.Function.Arguments, params.OrgID, params.ProjectID, responseID, toolDefs, toolMetadata, logger)
				if err != nil {
					logger.Error("failed to parse spawn_sub_agent arguments", "error", err)
					errMsg := fmt.Sprintf("Error parsing spawn_sub_agent arguments: %v", err)
					messages = append(messages, openrouter.OpenAIChatMessage{
						Role:       "tool",
						Content:    errMsg,
						Name:       tc.Function.Name,
						ToolCallID: tc.ID,
						ToolCalls:  nil,
					})
					continue
				}

				logger.Info("spawning sub-agent workflow",
					"goal", childWorkflowParams.Goal,
					"tool_count", len(childWorkflowParams.Tools))

				// Execute child workflow
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
				// Regular tool call - execute via activity
				toolMeta, ok := toolMetadata[tc.Function.Name]
				if !ok {
					logger.Warn("tool metadata not found", "tool_name", tc.Function.Name)
					toolMeta = activities.ToolMetadata{
						ToolsetSlug:     "",
						EnvironmentSlug: "",
						Headers:         nil,
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
					isSubAgent:   false,
				})
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
					// Sub-agent returns simple result string
					toolCallOutput = activities.ExecuteToolCallOutput{
						ToolOutput: subAgentResult.Result,
						ToolError:  subAgentResult.Error,
					}
				}
			} else {
				// Regular tool call
				err := tcf.future.Get(ctx, &toolCallOutput)
				if err != nil {
					logger.Error("failed to execute tool call", "error", err, "tool_name", tcf.toolCallName)
					// Continue with error output
					errMsg := err.Error()
					toolCallOutput = activities.ExecuteToolCallOutput{
						ToolOutput: fmt.Sprintf("Error executing tool: %v", err),
						ToolError:  &errMsg,
					}
				}
			}

			// Construct output item based on tool metadata
			// Get tool metadata for this tool call
			toolMeta, ok := toolMetadata[tcf.toolCall.Function.Name]
			if (ok && toolMeta.IsMCPTool) || tcf.isSubAgent {
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

	if len(output) == 0 {
		errMsg := "agent loop completed without producing output"
		return buildErrorResponse(ctx, params, responseID, &errMsg), nil
	}

	// Build successful response
	return &agents.ResponseOutput{
		ID:                 responseID,
		Object:             "response",
		CreatedAt:          workflow.Now(ctx).Unix(),
		Status:             "completed",
		Error:              nil,
		Instructions:       params.Request.Instructions,
		Model:              params.Request.Model,
		Output:             output,
		PreviousResponseID: params.Request.PreviousResponseID,
		Temperature:        getTemperature(params.Request.Temperature),
		Text: agents.ResponseText{
			Format: agents.TextFormat{Type: "text"},
		},
		Usage: totalUsage,
	}, nil
}

func buildErrorResponse(ctx workflow.Context, params AgentsResponseWorkflowParams, responseID string, errMsg *string) *agents.ResponseOutput {
	return &agents.ResponseOutput{
		ID:                 responseID,
		Object:             "response",
		CreatedAt:          workflow.Now(ctx).Unix(),
		Status:             "failed",
		Error:              errMsg,
		Instructions:       params.Request.Instructions,
		Model:              params.Request.Model,
		Output:             []agents.OutputItem{},
		PreviousResponseID: params.Request.PreviousResponseID,
		Temperature:        getTemperature(params.Request.Temperature),
		Text: agents.ResponseText{
			Format: agents.TextFormat{Type: "text"},
		},
		Usage: agents.ResponseUsage{
			InputTokens:  0,
			OutputTokens: 0,
			TotalTokens:  0,
		},
	}
}

func getTemperature(temp *float64) float64 {
	if temp != nil {
		return *temp
	}
	return 1.0
}

// parseSpawnSubAgentArgs parses the arguments for spawn_sub_agent tool call
func parseSpawnSubAgentArgs(argsJSON string, orgID string, projectID uuid.UUID, parentID string, availableToolDefs []openrouter.Tool, toolMetadata map[string]activities.ToolMetadata, logger interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}) (SubAgentWorkflowParams, error) {
	var args struct {
		Goal    string   `json:"goal"`
		Context string   `json:"context"`
		Tools   []string `json:"tools"` // Tool names from LLM
	}

	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return SubAgentWorkflowParams{}, fmt.Errorf("failed to unmarshal spawn_sub_agent arguments: %w", err)
	}

	if args.Goal == "" {
		return SubAgentWorkflowParams{}, fmt.Errorf("goal is required for spawn_sub_agent")
	}

	if args.Context == "" {
		return SubAgentWorkflowParams{}, fmt.Errorf("context is required for spawn_sub_agent")
	}

	// Build a map of tool names to full tool definitions
	toolDefMap := make(map[string]openrouter.Tool)
	for _, toolDef := range availableToolDefs {
		if toolDef.Function != nil {
			toolDefMap[toolDef.Function.Name] = toolDef
		}
	}

	// Log available tool names for debugging
	availableToolNames := make([]string, 0, len(toolDefMap))
	for name := range toolDefMap {
		availableToolNames = append(availableToolNames, name)
	}
	logger.Info("parsing spawn_sub_agent",
		"requested_tools", args.Tools,
		"available_tool_count", len(availableToolNames),
		"available_tools", availableToolNames)

	// Look up full tool definitions for the tools requested by the LLM
	// Pass complete tool definitions just like we do in the main workflow
	subAgentTools := make([]openrouter.Tool, 0, len(args.Tools))
	subAgentToolMetadata := make(map[string]activities.ToolMetadata)
	for _, toolName := range args.Tools {
		// Try exact match first
		toolDef, ok := toolDefMap[toolName]
		if !ok {
			// Try removing common prefixes (e.g., "functions.")
			normalizedName := toolName
			if strings.HasPrefix(toolName, "functions.") {
				normalizedName = strings.TrimPrefix(toolName, "functions.")
			}
			toolDef, ok = toolDefMap[normalizedName]
			if !ok {
				// Try with underscore instead of dot
				normalizedName = strings.ReplaceAll(toolName, ".", "_")
				toolDef, ok = toolDefMap[normalizedName]
			}
		}

		if ok {
			// Use the actual tool name from the definition, not the requested name
			actualToolName := toolDef.Function.Name
			// Pass the complete tool definition with full parameter schema
			subAgentTools = append(subAgentTools, toolDef)
			// Pass the tool metadata so sub-agent can load executors
			if meta, ok := toolMetadata[actualToolName]; ok {
				subAgentToolMetadata[actualToolName] = meta
			} else {
				// Default metadata for tools without toolset context
				subAgentToolMetadata[actualToolName] = activities.ToolMetadata{
					ToolsetSlug:     "",
					EnvironmentSlug: "",
					Headers:         nil,
					IsMCPTool:       false,
					ServerLabel:     "",
				}
			}
			logger.Info("matched tool", "requested", toolName, "actual", actualToolName)
		} else {
			logger.Warn("tool not found", "requested_tool", toolName, "available_tools", availableToolNames)
			return SubAgentWorkflowParams{}, fmt.Errorf("tool %q not found in available tools. Available tools: %v", toolName, availableToolNames)
		}
	}

	return SubAgentWorkflowParams{
		OrgID:        orgID,
		ProjectID:    projectID,
		Goal:         args.Goal,
		Context:      args.Context,
		Tools:        subAgentTools,
		ToolMetadata: subAgentToolMetadata,
		ParentID:     parentID,
	}, nil
}

// getSpawnSubAgentTool returns the tool definition for spawn_sub_agent
func getSpawnSubAgentTool() openrouter.Tool {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"goal": map[string]interface{}{
				"type":        "string",
				"description": "The specific objective or goal the sub-agent should accomplish",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"description": "Additional context or information relevant to completing the goal",
			},
			"tools": map[string]interface{}{
				"type":        "array",
				"description": "List of tool names (strings) to make available to the sub-agent. Use the EXACT tool names as they appear in your available tools list. Do NOT add any prefixes like 'functions.' - use the exact tool name as available to you.",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"required": []string{"goal", "context", "tools"},
	}

	schemaJSON, _ := json.Marshal(schema)

	return openrouter.Tool{
		Type: "function",
		Function: &openrouter.FunctionDefinition{
			Name:        "spawn_sub_agent",
			Description: "Spawn a sub-agent with a specific goal, context, and set of tools. The sub-agent will execute its task using the tools you delegate and return the result.",
			Parameters:  schemaJSON,
		},
	}
}
