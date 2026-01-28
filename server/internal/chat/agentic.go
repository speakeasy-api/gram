package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/google/uuid"

	or "github.com/OpenRouterTeam/go-sdk/models/components"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// AgenticConfig holds configuration for agentic chat mode
type AgenticConfig struct {
	Enabled              bool
	MaxDepth             int
	DefaultExecutionMode string // "sequential", "parallel", "auto"
	Toolsets             []agents.Toolset
}

// ParseAgenticConfig parses agentic configuration from request headers
func ParseAgenticConfig(r *http.Request) *AgenticConfig {
	enabled := r.Header.Get("Gram-Agents-Enabled") == "true"
	if !enabled {
		return nil
	}

	maxDepth := 3 // default
	if depthStr := r.Header.Get("Gram-Agents-Max-Depth"); depthStr != "" {
		if _, err := fmt.Sscanf(depthStr, "%d", &maxDepth); err != nil {
			maxDepth = 3
		}
	}

	executionMode := r.Header.Get("Gram-Agents-Execution-Mode")
	if executionMode == "" {
		executionMode = "auto"
	}

	// Parse toolsets from JSON header if provided
	var toolsets []agents.Toolset
	if toolsetsJSON := r.Header.Get("Gram-Agents-Toolsets"); toolsetsJSON != "" {
		_ = json.Unmarshal([]byte(toolsetsJSON), &toolsets)
	}

	return &AgenticConfig{
		Enabled:              true,
		MaxDepth:             maxDepth,
		DefaultExecutionMode: executionMode,
		Toolsets:             toolsets,
	}
}

// AgenticChatHandler handles chat requests with agent support
type AgenticChatHandler struct {
	agentsService *agents.Service
	subAgentExec  *agents.StreamingSubAgentExecutor
	logger        *slog.Logger
}

// NewAgenticChatHandler creates a new agentic chat handler
func NewAgenticChatHandler(agentsService *agents.Service, logger *slog.Logger) *AgenticChatHandler {
	return &AgenticChatHandler{
		agentsService: agentsService,
		subAgentExec:  agents.NewStreamingSubAgentExecutor(agentsService),
		logger:        logger.With(attr.SlogComponent("agentic-chat")),
	}
}

// HandleAgenticCompletion handles a chat completion request in agentic mode
func (h *AgenticChatHandler) HandleAgenticCompletion(
	ctx context.Context,
	w http.ResponseWriter,
	orgID string,
	projectID uuid.UUID,
	chatRequest openrouter.OpenAIChatRequest,
	agenticConfig *AgenticConfig,
) error {
	h.logger.InfoContext(ctx, "HandleAgenticCompletion: setting SSE headers")

	// Set up SSE headers - must be done BEFORE WriteHeader
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	h.logger.InfoContext(ctx, "HandleAgenticCompletion: headers set",
		attr.SlogHTTPResponseHeaderContentType(w.Header().Get("Content-Type")),
	)

	// Get flusher if available for SSE streaming
	flusher, canFlush := w.(http.Flusher)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	// Write headers immediately so client sees the event-stream content type
	w.WriteHeader(http.StatusOK)
	flush()

	h.logger.InfoContext(ctx, "HandleAgenticCompletion: WriteHeader(200) called and flushed")

	h.logger.InfoContext(ctx, "starting agentic completion",
		attr.SlogProjectID(projectID.String()),
		attr.SlogOrganizationID(orgID),
	)

	// Messages are already in the correct format
	messages := chatRequest.Messages

	// Load tools from toolsets
	var toolDefs []openrouter.Tool
	toolMetadata := make(map[string]agents.AgentTool)

	for _, toolset := range agenticConfig.Toolsets {
		tools, err := h.agentsService.LoadToolsetTools(ctx, projectID, toolset.ToolsetSlug, toolset.EnvironmentSlug)
		if err != nil {
			h.logger.WarnContext(ctx, "failed to load toolset tools for "+toolset.ToolsetSlug,
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

	// Add any tools from the original request
	toolDefs = append(toolDefs, chatRequest.Tools...)

	// Add spawn_agent tool if depth allows
	if agenticConfig.MaxDepth > 0 {
		toolDefs = append(toolDefs, agents.SpawnAgentToolDefinition())
	}

	// Convert temperature to *float64
	var temperature *float64
	if chatRequest.Temperature != 0 {
		temp := float64(chatRequest.Temperature)
		temperature = &temp
	}

	// Write an OpenAI-compatible streaming chunk
	// Sub-agent events are embedded as HTML comments: <!--GRAM_AGENT:{...}-->
	writeChunk := func(content string, finishReason *string) error {
		chunk := openrouter.StreamingChunk{
			ID:                "chatcmpl-" + uuid.New().String(),
			Object:            "chat.completion.chunk",
			Created:           0,
			Model:             chatRequest.Model,
			SystemFingerprint: "",
			Choices: []openrouter.ChunkChoice{
				{
					Index: 0,
					Delta: openrouter.ChunkDelta{
						Content:   content,
						ToolCalls: nil,
					},
					FinishReason: finishReason,
				},
			},
			Usage: nil,
		}
		chunkJSON, err := json.Marshal(chunk)
		if err != nil {
			return fmt.Errorf("marshal chunk: %w", err)
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", chunkJSON); err != nil {
			return fmt.Errorf("write chunk: %w", err)
		}
		flush()
		return nil
	}

	// Write a sub-agent event embedded in an OpenAI-compatible chunk
	writeAgentEvent := func(eventData any) error {
		jsonData, err := json.Marshal(eventData)
		if err != nil {
			return fmt.Errorf("marshal agent event: %w", err)
		}
		// HTML-escape the JSON to prevent --> in content from breaking the marker
		// This escapes <, >, and & to their unicode equivalents (\u003c, \u003e, \u0026)
		var escapedJSON bytes.Buffer
		json.HTMLEscape(&escapedJSON, jsonData)
		// Embed as HTML comment so it's hidden from normal text rendering
		// but can be parsed by the client
		content := fmt.Sprintf("<!--GRAM_AGENT:%s-->", escapedJSON.String())
		return writeChunk(content, nil)
	}

	// Agentic loop with iteration limit to prevent infinite loops
	const maxIterations = 20
	iteration := 0
	for {
		iteration++
		if iteration > maxIterations {
			h.logger.WarnContext(ctx, fmt.Sprintf("agentic loop hit max iterations (%d), terminating", maxIterations))
			// Send done marker and exit gracefully
			if _, err := fmt.Fprintf(w, "data: [DONE]\n\n"); err != nil {
				return fmt.Errorf("write done marker: %w", err)
			}
			flush()
			return nil
		}

		// Check context cancellation
		if err := ctx.Err(); err != nil {
			h.logger.WarnContext(ctx, "context cancelled during agentic loop", attr.SlogError(err))
			return fmt.Errorf("context cancelled: %w", err)
		}

		h.logger.InfoContext(ctx, fmt.Sprintf("calling StreamCompletionFromMessages (iteration %d)", iteration),
			attr.SlogProjectID(projectID.String()),
		)

		// Track whether this response has tool calls (determined at end of stream)
		var hasToolCalls bool

		// Stream completion from model - always stream content immediately
		msg, err := h.agentsService.StreamCompletionFromMessages(
			ctx,
			orgID,
			projectID.String(),
			messages,
			toolDefs,
			temperature,
			chatRequest.Model,
			func(delta string) error {
				// Stream content immediately as it arrives
				return writeChunk(delta, nil)
			},
		)
		if err != nil {
			h.logger.ErrorContext(ctx, "StreamCompletionFromMessages failed", attr.SlogError(err))
			return fmt.Errorf("model call failed: %w", err)
		}

		h.logger.InfoContext(ctx, "StreamCompletionFromMessages succeeded")

		messages = append(messages, *msg)

		// Check for tool calls
		if msg.Type == or.MessageTypeAssistant && msg.AssistantMessage != nil {
			hasToolCalls = len(msg.AssistantMessage.ToolCalls) > 0

			// If no tool calls, we're done (content was already streamed)
			if !hasToolCalls {
				// Send done marker
				if _, err := fmt.Fprintf(w, "data: [DONE]\n\n"); err != nil {
					return fmt.Errorf("write done marker: %w", err)
				}
				flush()
				return nil
			}

			// Separate spawn_agent calls from regular tool calls
			type spawnCall struct {
				tc   or.ChatMessageToolCall
				args *agents.SpawnAgentArgs
			}
			var spawnCalls []spawnCall
			var regularCalls []or.ChatMessageToolCall

			for _, tc := range msg.AssistantMessage.ToolCalls {
				if agents.IsSpawnAgentTool(tc.Function.Name) {
					args, err := agents.ParseSpawnAgentArgs(tc.Function.Arguments)
					if err != nil {
						// Return error as tool result
						toolResult := or.CreateMessageTool(or.ToolResponseMessage{
							Content:    or.CreateToolResponseMessageContentStr(fmt.Sprintf("Error parsing spawn_agent args: %v", err)),
							ToolCallID: tc.ID,
						})
						messages = append(messages, toolResult)
						continue
					}
					spawnCalls = append(spawnCalls, spawnCall{tc: tc, args: args})
				} else {
					regularCalls = append(regularCalls, tc)
				}
			}

			// Execute all spawn_agent calls in parallel
			if len(spawnCalls) > 0 {
				var wg sync.WaitGroup
				var mu sync.Mutex // Protects writes to the HTTP response

				// Thread-safe write function
				writeAgentEventSafe := func(eventData any) error {
					mu.Lock()
					defer mu.Unlock()
					return writeAgentEvent(eventData)
				}

				// Channel to collect results
				type spawnResult struct {
					toolCallID string
					result     string
				}
				results := make(chan spawnResult, len(spawnCalls))

				for _, sc := range spawnCalls {
					wg.Add(1)
					go func(sc spawnCall) {
						defer wg.Done()

						// Use the tool call ID as the agent ID so the frontend can
						// match spawn_agent tool calls to their agent views
						childAgentID := sc.tc.ID
						childParams := agents.StreamingSubAgentParams{
							AgentID:      childAgentID,
							ParentID:     nil, // Top-level spawn
							OrgID:        orgID,
							ProjectID:    projectID,
							Name:         sc.args.Name,
							Task:         sc.args.Task,
							Context:      sc.args.Context,
							Instructions: "",
							Toolsets:     agenticConfig.Toolsets,
							Model:        chatRequest.Model,
							Temperature:  temperature,
							MaxDepth:     agenticConfig.MaxDepth,
							CurrentDepth: 0,
						}

						childResult, err := h.subAgentExec.ExecuteSubAgent(ctx, childParams, func(event agents.SubAgentEvent) error {
							return writeAgentEventSafe(event)
						})

						var resultContent string
						if err != nil {
							resultContent = fmt.Sprintf("Sub-agent execution error: %v", err)
						} else if childResult.Status == "completed" && childResult.Result != nil {
							resultContent = *childResult.Result
						} else if childResult.Error != nil {
							resultContent = fmt.Sprintf("Sub-agent failed: %s", *childResult.Error)
						} else {
							resultContent = "Sub-agent completed with no result"
						}

						results <- spawnResult{toolCallID: sc.tc.ID, result: resultContent}
					}(sc)
				}

				// Wait for all spawn_agent calls to complete
				go func() {
					wg.Wait()
					close(results)
				}()

				// Collect results and add to messages
				for r := range results {
					toolResult := or.CreateMessageTool(or.ToolResponseMessage{
						Content:    or.CreateToolResponseMessageContentStr(r.result),
						ToolCallID: r.toolCallID,
					})
					messages = append(messages, toolResult)
				}
			}

			// Process regular tool calls sequentially
			for _, tc := range regularCalls {
				var argsMap map[string]any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &argsMap); err != nil {
					argsMap = map[string]any{"raw": tc.Function.Arguments}
				}

				toolCallEvent := agents.SubAgentToolCallEvent{
					Type:       agents.SubAgentEventToolCall,
					ID:         "main",
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
					Args:       argsMap,
				}
				if err := writeAgentEvent(toolCallEvent); err != nil {
					h.logger.WarnContext(ctx, "failed to write tool call event", attr.SlogError(err))
				}

				// Execute the tool
				var toolOutput string
				var isError bool

				toolInfo, ok := toolMetadata[tc.Function.Name]
				if ok && toolInfo.ToolURN != nil {
					result, err := h.agentsService.ExecuteTool(
						ctx,
						projectID,
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
				toolResultEvent := agents.SubAgentToolResultEvent{
					Type:       agents.SubAgentEventToolResult,
					ID:         "main",
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
					Result:     toolOutput,
					IsError:    isError,
				}
				if err := writeAgentEvent(toolResultEvent); err != nil {
					h.logger.WarnContext(ctx, "failed to write tool result event", attr.SlogError(err))
				}

				// Add tool response to messages
				toolResult := or.CreateMessageTool(or.ToolResponseMessage{
					Content:    or.CreateToolResponseMessageContentStr(toolOutput),
					ToolCallID: tc.ID,
				})
				messages = append(messages, toolResult)
			}
		} else {
			// Non-assistant message (shouldn't happen in normal flow)
			// Send done marker
			if _, err := fmt.Fprintf(w, "data: [DONE]\n\n"); err != nil {
				return fmt.Errorf("write done marker: %w", err)
			}
			flush()
			return nil
		}
	}
}

// ShouldUseAgenticMode determines if the request should use agentic mode
func ShouldUseAgenticMode(r *http.Request) bool {
	return r.Header.Get("Gram-Agents-Enabled") == "true"
}
