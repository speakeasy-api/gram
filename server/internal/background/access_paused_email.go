package background

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
	"go.temporal.io/sdk/workflow"
)

const accessPausedEmailWorkflowIDPrefix = "v1:access-paused-email"

func (s *TemporalBillingEmailScheduler) ScheduleAccessPaused(ctx context.Context, input billingnotifications.SendAccessPausedInput) error {
	if err := s.enqueue(ctx, accessPausedEmailWorkflowIDPrefix, input.EventID, AccessPausedEmailWorkflow, input); err != nil {
		return fmt.Errorf("enqueue access paused email workflow: %w", err)
	}
	return nil
}

func AccessPausedEmailWorkflow(ctx workflow.Context, input billingnotifications.SendAccessPausedInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: trialLifecycleEmailActivityTimeout,
		RetryPolicy:         billingEmailRetryPolicy(),
	})
	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.SendAccessPausedEmail, input).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Error("access paused email delivery failed", "organization_id", input.OrganizationID, "event_id", input.EventID, "error", err)
		return fmt.Errorf("send access paused email: %w", err)
	}
	return nil
}
