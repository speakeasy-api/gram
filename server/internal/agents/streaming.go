package agents

import (
	"context"
	"encoding/json"
	"fmt"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// StreamingSubAgentParams holds parameters for executing a streaming sub-agent
type StreamingSubAgentParams struct {
	AgentID      string
	ParentID     *string
	OrgID        string
	ProjectID    uuid.UUID
	Name         string
	Task         string
	Context      string
	Instructions string
	Toolsets     []Toolset
	Model        string
	Temperature  *float64
	MaxDepth     int
	CurrentDepth int
}

// SubAgentEventCallback is called when a sub-agent emits an event
type SubAgentEventCallback func(event SubAgentEvent) error

// StreamingSubAgentExecutor executes sub-agents with streaming event callbacks
type StreamingSubAgentExecutor struct {
	service *Service
}

// NewStreamingSubAgentExecutor creates a new streaming sub-agent executor
func NewStreamingSubAgentExecutor(service *Service) *StreamingSubAgentExecutor {
	return &StreamingSubAgentExecutor{
		service: service,
	}
}

// ExecuteSubAgent executes a sub-agent and streams events via callback
func (e *StreamingSubAgentExecutor) ExecuteSubAgent(
	ctx context.Context,
	params StreamingSubAgentParams,
	onEvent SubAgentEventCallback,
) (*SubAgentCompleteEvent, error) {
	// Check depth limit
	if params.CurrentDepth >= params.MaxDepth {
		errMsg := fmt.Sprintf("maximum agent nesting depth (%d) exceeded", params.MaxDepth)
		return &SubAgentCompleteEvent{
			Type:   SubAgentEventComplete,
			ID:     params.AgentID,
			Status: "failed",
			Result: nil,
			Error:  &errMsg,
		}, nil
	}

	// Emit spawn event
	spawnEvent := SubAgentSpawnEvent{
		Type:        SubAgentEventSpawn,
		ID:          params.AgentID,
		ParentID:    params.ParentID,
		Name:        params.Name,
		Task:        params.Task,
		Description: params.Instructions,
	}
	if err := onEvent(spawnEvent); err != nil {
		return nil, fmt.Errorf("failed to emit spawn event: %w", err)
	}

	// Build system prompt
	systemPrompt := `You are a task-based agent with access to specific tools to achieve a specific objective. Your role is to complete the given objective using the tools provided to you.

You will receive:
- Goal: The specific objective you need to accomplish
- Context: Additional context or information relevant to completing the goal

Please use the tools available to you to complete the goal. Think step by step and use the tools as needed to achieve the objective.`

	if params.Instructions != "" {
		systemPrompt += fmt.Sprintf("\n\nInstructions: %s", params.Instructions)
	}

	messages := []or.Message{
		or.CreateMessageSystem(or.SystemMessage{
			Content: or.CreateSystemMessageContentStr(systemPrompt),
			Name:    nil,
		}),
		or.CreateMessageUser(or.UserMessage{
			Content: or.CreateUserMessageContentStr(fmt.Sprintf("Goal: %s\n\nContext: %s", params.Task, params.Context)),
			Name:    nil,
		}),
	}

	// Load tools from toolsets
	var toolDefs []openrouter.Tool
	toolMetadata := make(map[string]AgentTool)

	for _, toolset := range params.Toolsets {
		tools, err := e.service.LoadToolsetTools(ctx, params.ProjectID, toolset.ToolsetSlug, toolset.EnvironmentSlug)
		if err != nil {
			e.service.logger.WarnContext(ctx, "failed to load toolset tools for "+toolset.ToolsetSlug,
				attr.SlogError(err))
			continue
		}
		for _, tool := range tools {
			toolDefs = append(toolDefs, tool.Definition)
			if tool.Definition.Function != nil {
				toolMetadata[tool.Definition.Function.Name] = tool
			}
		}
	}

	// Note: Sub-agents do NOT get the spawn_agent tool.
	// Only the main orchestrator (in agentic.go) can spawn sub-agents.
	// This prevents excessive nesting and slow serial API calls.

	var finalResult string

	// Agentic loop with iteration limit to prevent infinite loops
	const maxIterations = 15
	iteration := 0
	for {
		iteration++
		if iteration > maxIterations {
			e.service.logger.WarnContext(ctx, fmt.Sprintf("sub-agent %s hit max iterations (%d), terminating", params.AgentID, maxIterations))
			errMsg := fmt.Sprintf("sub-agent exceeded maximum iterations (%d)", maxIterations)
			return &SubAgentCompleteEvent{
				Type:   SubAgentEventComplete,
				ID:     params.AgentID,
				Status: "failed",
				Result: nil,
				Error:  &errMsg,
			}, nil
		}

		// Check context cancellation
		if err := ctx.Err(); err != nil {
			errMsg := fmt.Sprintf("context cancelled: %v", err)
			return &SubAgentCompleteEvent{
				Type:   SubAgentEventComplete,
				ID:     params.AgentID,
				Status: "failed",
				Result: nil,
				Error:  &errMsg,
			}, nil
		}

		// Stream completion from model, emitting delta events for each chunk
		msg, err := e.service.StreamCompletionFromMessages(
			ctx,
			params.OrgID,
			params.ProjectID.String(),
			messages,
			toolDefs,
			params.Temperature,
			params.Model,
			func(delta string) error {
				// Emit delta event for each streaming chunk
				deltaEvent := SubAgentDeltaEvent{
					Type:    SubAgentEventDelta,
					ID:      params.AgentID,
					Content: delta,
				}
				return onEvent(deltaEvent)
			},
		)
		if err != nil {
			errMsg := fmt.Sprintf("model call failed: %v", err)
			return &SubAgentCompleteEvent{
				Type:   SubAgentEventComplete,
				ID:     params.AgentID,
				Status: "failed",
				Result: nil,
				Error:  &errMsg,
			}, nil
		}

		messages = append(messages, *msg)

		// Check if this is a final message (no tool calls)
		// Content was already streamed via deltas in the onChunk callback
		if msg.Type != or.MessageTypeAssistant || len(msg.AssistantMessage.ToolCalls) == 0 {
			finalResult = openrouter.GetText(*msg)
			break
		}

		// Process tool calls (sub-agents only have regular tools, no spawn_agent)
		for _, tc := range msg.AssistantMessage.ToolCalls {
			var argsMap map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &argsMap); err != nil {
				argsMap = map[string]any{"raw": tc.Function.Arguments}
			}

			// Emit tool call event
			toolCallEvent := SubAgentToolCallEvent{
				Type:       SubAgentEventToolCall,
				ID:         params.AgentID,
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Args:       argsMap,
			}
			if err := onEvent(toolCallEvent); err != nil {
				return nil, fmt.Errorf("failed to emit tool call event: %w", err)
			}

			// Execute the tool
			var toolOutput string
			var isError bool

			toolInfo, ok := toolMetadata[tc.Function.Name]
			if ok && toolInfo.ToolURN != nil {
				result, err := e.service.ExecuteTool(
					ctx,
					params.ProjectID,
					*toolInfo.ToolURN,
					"", // Use default environment
					tc.Function.Arguments,
				)
				if err != nil {
					toolOutput = fmt.Sprintf("Error: %v", err)
					isError = true
				} else {
					toolOutput = result
				}
			} else {
				toolOutput = fmt.Sprintf("Unknown tool: %s", tc.Function.Name)
				isError = true
			}

			// Emit tool result event
			toolResultEvent := SubAgentToolResultEvent{
				Type:       SubAgentEventToolResult,
				ID:         params.AgentID,
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Result:     toolOutput,
				IsError:    isError,
			}
			if err := onEvent(toolResultEvent); err != nil {
				return nil, fmt.Errorf("failed to emit tool result event: %w", err)
			}

			// Add tool response to messages
			toolResult := or.CreateMessageTool(or.ToolResponseMessage{
				Content:    or.CreateToolResponseMessageContentStr(toolOutput),
				ToolCallID: tc.ID,
			})
			messages = append(messages, toolResult)
		}
	}

	// Emit complete event
	completeEvent := SubAgentCompleteEvent{
		Type:   SubAgentEventComplete,
		ID:     params.AgentID,
		Status: "completed",
		Result: &finalResult,
		Error:  nil,
	}
	if err := onEvent(completeEvent); err != nil {
		return nil, fmt.Errorf("failed to emit complete event: %w", err)
	}

	return &completeEvent, nil
}
