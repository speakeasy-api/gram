package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	or "github.com/speakeasy-api/gram/openrouter/models/components"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

type ExecuteToolCallInput struct {
	OrgID        string
	ProjectID    uuid.UUID
	ToolCall     or.ChatMessageToolCall
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
	var toolOutput string
	var toolError *string

	if input.ToolMetadata.ToolURN != nil {
		result, err := a.agentsService.ExecuteTool(
			ctx,
			input.ProjectID,
			*input.ToolMetadata.ToolURN,
			input.ToolMetadata.EnvironmentSlug,
			input.ToolCall.Function.Arguments,
		)
		if err != nil {
			errMsg := fmt.Sprintf("Error calling tool %q: %v", input.ToolCall.Function.Name, err)
			toolOutput = errMsg
			toolError = &errMsg
			a.logger.ErrorContext(ctx, "Tool error", attr.SlogToolName(input.ToolCall.Function.Name), attr.SlogError(err))
		} else {
			toolOutput = result
		}
	} else {
		errMsg := fmt.Sprintf("No ToolURN found for %q", input.ToolCall.Function.Name)
		toolOutput = errMsg
		toolError = &errMsg
		a.logger.ErrorContext(ctx, "Missing ToolURN", attr.SlogToolName(input.ToolCall.Function.Name))
	}

	return &ExecuteToolCallOutput{
		ToolOutput: toolOutput,
		ToolError:  toolError,
	}, nil
}
