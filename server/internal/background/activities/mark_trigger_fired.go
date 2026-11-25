package activities

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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

	// A trigger firing on its schedule has no request behind it. Marked here
	// rather than in MarkInstanceFired so the App method keeps whatever
	// surface its caller carries.
	ctx = contextvalues.SetActingSurface(ctx, string(audit.SurfaceSystem))

	if err := m.app.MarkInstanceFired(ctx, input.TriggerInstanceID); err != nil {
		return fmt.Errorf("mark trigger fired: %w", err)
	}
	return nil
}
