package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
	tenvironment "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

const (
	accessPausedEmailWorkflowIDPrefix   = "v1:access-paused-email"
	accessPausedEmailWorkflowRunTimeout = 7 * 24 * time.Hour
)

type TemporalBillingEmailScheduler struct {
	TemporalEnv *tenvironment.Environment
}

var _ billingnotifications.AccessPausedScheduler = (*TemporalBillingEmailScheduler)(nil)

func (s *TemporalBillingEmailScheduler) ScheduleAccessPaused(ctx context.Context, input billingnotifications.SendAccessPausedInput) error {
	if s == nil || s.TemporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}
	_, err := s.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    fmt.Sprintf("%s:%s", accessPausedEmailWorkflowIDPrefix, input.EventID),
		TaskQueue:             string(s.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowRunTimeout:    accessPausedEmailWorkflowRunTimeout,
	}, AccessPausedEmailWorkflow, input)
	if err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &alreadyStarted) {
			return nil
		}
		return fmt.Errorf("enqueue access paused email workflow: %w", err)
	}
	return nil
}

func AccessPausedEmailWorkflow(ctx workflow.Context, input billingnotifications.SendAccessPausedInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: trialLifecycleEmailActivityTimeout,
		RetryPolicy:         trialReminderRetryPolicy(),
	})
	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.SendAccessPausedEmail, input).Get(ctx, nil); err != nil {
		return fmt.Errorf("send access paused email: %w", err)
	}
	return nil
}
