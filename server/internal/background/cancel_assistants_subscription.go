package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// AssistantsSubscriptionCancelScheduler schedules a cancel-at-period-end
// follow-up for a Polar subscription that just granted assistants credits.
// Implemented by Temporal in production; auth tests can swap a no-op.
type AssistantsSubscriptionCancelScheduler interface {
	ScheduleCancelAssistantsSubscription(ctx context.Context, subscriptionID string) error
}

type TemporalAssistantsSubscriptionCancelScheduler struct {
	TemporalEnv *tenv.Environment
}

func (t *TemporalAssistantsSubscriptionCancelScheduler) ScheduleCancelAssistantsSubscription(ctx context.Context, subscriptionID string) error {
	if subscriptionID == "" {
		return fmt.Errorf("subscription ID is required")
	}
	id := fmt.Sprintf("v1:cancel-assistants-subscription:%s", subscriptionID)
	_, err := t.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(t.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowRunTimeout:    10 * time.Minute,
	}, CancelAssistantsSubscriptionWorkflow, activities.CancelAssistantsSubscriptionArgs{
		SubscriptionID: subscriptionID,
	})
	if err != nil {
		return fmt.Errorf("schedule cancel assistants subscription workflow: %w", err)
	}
	return nil
}

func CancelAssistantsSubscriptionWorkflow(ctx workflow.Context, args activities.CancelAssistantsSubscriptionArgs) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		// Persistent failures escalate after ~5 attempts over ~5 minutes —
		// surfaced to Temporal monitoring so on-call sees a stuck workflow
		// rather than a silent never-cancelled subscription that would re-grant
		// 20 credits next cycle.
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    5,
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.CancelAssistantsSubscription, args).Get(ctx, nil); err != nil {
		return fmt.Errorf("cancel assistants subscription: %w", err)
	}
	return nil
}
