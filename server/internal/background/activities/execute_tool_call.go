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

type ExecuteToolCallInput struct {
	OrgID        string
	ProjectID    uuid.UUID
	ToolCall     openrouter.ToolCall
	ToolMetadata ToolMetadata
}

type ExecuteToolCallOutput struct {
	ToolOutput string
	ToolError  *string
}

type ExecuteToolCall struct {
	logger        *slog.Logger
	agentsService *agents.Service
}

func NewExecuteToolCall(logger *slog.Logger, agentsService *agents.Service) *ExecuteToolCall {
	return &ExecuteToolCall{
		logger:        logger.With(attr.SlogComponent("execute-tool-call-activity")),
		agentsService: agentsService,
	}
}

func (a *ExecuteToolCall) Do(ctx context.Context, input ExecuteToolCallInput) (*ExecuteToolCallOutput, error) {
	a.logger.InfoContext(ctx, "executing tool call",
		attr.SlogToolName(input.ToolCall.Function.Name))

	var toolOutput string
	var toolError *string

	// Load tool executor from toolset if this is an MCP tool
	if input.ToolMetadata.IsMCPTool && input.ToolMetadata.ToolsetSlug != "" {
		toolsetTools, err := a.agentsService.LoadToolsetTools(
			ctx,
			input.ProjectID,
			input.ToolMetadata.ToolsetSlug,
			input.ToolMetadata.EnvironmentSlug,
			input.ToolMetadata.Headers,
		)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to load toolset %q: %v", input.ToolMetadata.ToolsetSlug, err)
			toolOutput = errMsg
			toolError = &errMsg
			a.logger.ErrorContext(ctx, "Failed to load toolset", attr.SlogError(err), attr.SlogToolsetSlug(input.ToolMetadata.ToolsetSlug))
		} else {
			// Find the executor for this tool
			var executor func(context.Context, string) (string, error)
			for _, tool := range toolsetTools {
				if tool.Definition.Function != nil && tool.Definition.Function.Name == input.ToolCall.Function.Name {
					executor = tool.Executor
					break
				}
			}

			if executor == nil {
				errMsg := fmt.Sprintf("No executor found for %q in toolset %q", input.ToolCall.Function.Name, input.ToolMetadata.ToolsetSlug)
				toolOutput = errMsg
				toolError = &errMsg
				a.logger.ErrorContext(ctx, "Missing executor", attr.SlogToolName(input.ToolCall.Function.Name))
			} else {
				result, err := executor(ctx, input.ToolCall.Function.Arguments)
				if err != nil {
					errMsg := fmt.Sprintf("Error calling tool %q: %v", input.ToolCall.Function.Name, err)
					toolOutput = errMsg
					toolError = &errMsg
					a.logger.ErrorContext(ctx, "Tool error", attr.SlogToolName(input.ToolCall.Function.Name), attr.SlogError(err))
				} else {
					toolOutput = result
				}
			}
		}
	} else {
		errMsg := fmt.Sprintf("No executor found for %q", input.ToolCall.Function.Name)
		toolOutput = errMsg
		toolError = &errMsg
		a.logger.ErrorContext(ctx, "Missing executor or tool metadata", attr.SlogToolName(input.ToolCall.Function.Name))
	}

	return &ExecuteToolCallOutput{
		ToolOutput: toolOutput,
		ToolError:  toolError,
	}, nil
}
