package activities

import (
	"context"
	"fmt"
	"log/slog"

	or "github.com/speakeasy-api/gram/openrouter/models/components"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type ExecuteModelCallInput struct {
	OrgID       string
	ProjectID   string
	Messages    []or.Message
	ToolDefs    []openrouter.Tool
	Temperature *float64
	Model       string
}

type ExecuteModelCallOutput struct {
	Message *or.Message
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
