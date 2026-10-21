package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	// googleChatRenewalScheduleID belongs to the shared worker only, never previews.
	googleChatRenewalScheduleID = "v1:googlechat-subscription-renewal"

	// googleChatRenewalInterval leaves multiple opportunities within the four-hour lease.
	googleChatRenewalInterval = time.Hour

	// googleChatRenewalTimeout bounds a fleet sweep, including provider delays.
	googleChatRenewalTimeout = 10 * time.Minute
)

// GoogleChatSubscriptionRenewalWorkflow renews subscriptions in one batched
// activity. Baseline cost is 720 starts + 720 activities/month per namespace;
// there is no per-subscription workflow or LLM call.
func GoogleChatSubscriptionRenewalWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: googleChatRenewalTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Minute, BackoffCoefficient: 2, MaximumAttempts: 3,
		},
	})
	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.RenewGoogleChatSubscriptions, workflow.Now(ctx)).Get(ctx, nil); err != nil {
		return fmt.Errorf("renew Google Chat subscriptions: %w", err)
	}
	return nil
}

func AddGoogleChatSubscriptionRenewalSchedule(ctx context.Context, env *tenv.Environment) error {
	_, err := createScheduleWithCatchup(ctx, env.Client().ScheduleClient(), client.ScheduleOptions{
		ID: googleChatRenewalScheduleID, CatchupWindow: googleChatRenewalInterval - time.Second,
		Spec: client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: googleChatRenewalInterval}}},
		Action: &client.ScheduleWorkflowAction{
			ID: googleChatRenewalScheduleID + "/scheduled", Workflow: GoogleChatSubscriptionRenewalWorkflow,
			TaskQueue: string(env.Queue()), WorkflowRunTimeout: 3*googleChatRenewalTimeout + 5*time.Minute,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil {
		return fmt.Errorf("create Google Chat subscription renewal schedule: %w", err)
	}
	return nil
}
