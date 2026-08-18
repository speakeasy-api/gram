package background

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
	"go.temporal.io/sdk/workflow"
)

const paygActivatedEmailWorkflowIDPrefix = "v1:payg-activated-email"

func (s *TemporalBillingEmailScheduler) SchedulePaygActivated(ctx context.Context, input billingnotifications.SendPaygActivatedInput) error {
	if err := s.enqueue(ctx, paygActivatedEmailWorkflowIDPrefix, input.EventID, PaygActivatedEmailWorkflow, input); err != nil {
		return fmt.Errorf("enqueue PAYG activated email workflow: %w", err)
	}
	return nil
}

func PaygActivatedEmailWorkflow(ctx workflow.Context, input billingnotifications.SendPaygActivatedInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: trialLifecycleEmailActivityTimeout,
		RetryPolicy:         billingEmailRetryPolicy(),
	})
	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.SendPaygActivatedEmail, input).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Error("PAYG activated email delivery failed", "organization_id", input.OrganizationID, "event_id", input.EventID, "error", err)
		return fmt.Errorf("send PAYG activated email: %w", err)
	}
	return nil
}
