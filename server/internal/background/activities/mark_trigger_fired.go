package activities

import (
	"context"
	"fmt"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
)

type MarkTriggerFiredInput struct {
	TriggerInstanceID string `json:"trigger_instance_id"`
}

type MarkTriggerFired struct {
	app *bgtriggers.App
}

func NewMarkTriggerFired(app *bgtriggers.App) *MarkTriggerFired {
	return &MarkTriggerFired{app: app}
}

func (m *MarkTriggerFired) Do(ctx context.Context, input MarkTriggerFiredInput) error {
	if m.app == nil {
		return fmt.Errorf("trigger app is not configured")
	}
	if err := m.app.MarkInstanceFired(ctx, input.TriggerInstanceID); err != nil {
		return fmt.Errorf("mark trigger fired: %w", err)
	}
	return nil
}
