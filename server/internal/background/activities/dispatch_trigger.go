package activities

import (
	"context"
	"fmt"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
)

type DispatchTriggerInput struct {
	Task *bgtriggers.Task
}

type DispatchTrigger struct {
	app *bgtriggers.App
}

func NewDispatchTrigger(app *bgtriggers.App) *DispatchTrigger {
	return &DispatchTrigger{
		app: app,
	}
}

func (d *DispatchTrigger) Do(ctx context.Context, input DispatchTriggerInput) error {
	if input.Task == nil {
		// nothing to do
		return nil
	}

	if d.app == nil {
		return fmt.Errorf("trigger app is not configured")
	}

	if err := d.app.Dispatch(ctx, *input.Task); err != nil {
		return fmt.Errorf("dispatch: %w", err)
	}

	return nil
}
