package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type ExecuteModelCallInput struct {
	OrgID       string
	ProjectID   string
	Messages    []openrouter.OpenAIChatMessage
	ToolDefs    []openrouter.Tool
	Temperature *float64
	Model       string
}

type ExecuteModelCallOutput struct {
	Message *openrouter.OpenAIChatMessage
	Error   error
}

type ExecuteModelCall struct {
	logger        *slog.Logger
	agentsService *agents.Service
}

func NewExecuteModelCall(logger *slog.Logger, agentsService *agents.Service) *ExecuteModelCall {
	return &ExecuteModelCall{
		logger:        logger.With(attr.SlogComponent("execute-model-call-activity")),
		agentsService: agentsService,
	}
}

func (a *ExecuteModelCall) Do(ctx context.Context, input ExecuteModelCallInput) (*ExecuteModelCallOutput, error) {
	a.logger.InfoContext(ctx, "executing model call",
		attr.SlogOrganizationID(input.OrgID))

	// Use the chat client from agents service
	msg, err := a.agentsService.GetCompletionFromMessages(ctx, input.OrgID, input.ProjectID, input.Messages, input.ToolDefs, input.Temperature, input.Model)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to get completion", attr.SlogError(err))
		return &ExecuteModelCallOutput{
			Message: nil,
			Error:   fmt.Errorf("failed to get completion: %w", err),
		}, nil
	}

	return &ExecuteModelCallOutput{
		Message: msg,
		Error:   nil,
	}, nil
}
