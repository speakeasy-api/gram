package activities

import (
	"context"
	"fmt"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
)

type ProcessScheduledTriggerInput struct {
	TriggerInstanceID string `json:"trigger_instance_id"`
	FiredAt           string `json:"fired_at,omitempty"`
}

type ProcessScheduledTriggerResult struct {
	Task *bgtriggers.Task `json:"task,omitempty"`
}

type ProcessScheduledTrigger struct {
	app *bgtriggers.App
}

func NewProcessScheduledTrigger(app *bgtriggers.App) *ProcessScheduledTrigger {
	return &ProcessScheduledTrigger{
		app: app,
	}
}

func (p *ProcessScheduledTrigger) Do(ctx context.Context, input ProcessScheduledTriggerInput) (*ProcessScheduledTriggerResult, error) {
	if p.app == nil {
		return nil, fmt.Errorf("trigger app is not configured")
	}

	task, err := p.app.ProcessScheduled(ctx, bgtriggers.ProcessScheduledInput{
		TriggerInstanceID: input.TriggerInstanceID,
		FiredAt:           input.FiredAt,
	})
	if err != nil {
		return nil, fmt.Errorf("process scheduled trigger: %w", err)
	}

	return &ProcessScheduledTriggerResult{Task: task}, nil
}
