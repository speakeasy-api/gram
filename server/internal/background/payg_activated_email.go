package background

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

const paygActivatedEmailWorkflowIDPrefix = "v1:payg-activated-email"

func (s *TemporalBillingEmailScheduler) SchedulePaygActivated(ctx context.Context, input billingnotifications.SendPaygActivatedInput) error {
	if s == nil || s.TemporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}
	_, err := s.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                                       fmt.Sprintf("%s:%s", paygActivatedEmailWorkflowIDPrefix, input.EventID),
		TaskQueue:                                string(s.TemporalEnv.Queue()),
		WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
		WorkflowRunTimeout:                       billingEmailWorkflowRunTimeout,
	}, PaygActivatedEmailWorkflow, input)
	if err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &alreadyStarted) {
			return nil
		}
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
