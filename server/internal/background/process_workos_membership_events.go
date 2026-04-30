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

const (
	processWorkOSMembershipEventsWorkflowID  = "v1:process-workos-membership-events"
	processWorkOSMembershipEventsDebounceSig = "v1:process-workos-membership-events/signal"
	reconcileWorkOSMembershipsScheduleID     = "v1:reconcile-workos-memberships-schedule"
	reconcileWorkOSMembershipsScheduledRunID = "v1:reconcile-workos-memberships/scheduled"
)

// ExecuteProcessWorkOSMembershipEventsWorkflowDebounced starts (or signals) the singleton membership
// processing workflow. Webhook receipts call this; debounce signal collapses bursts.
func ExecuteProcessWorkOSMembershipEventsWorkflowDebounced(ctx context.Context, temporalEnv *tenv.Environment) (client.WorkflowRun, error) {
	run, err := temporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		processWorkOSMembershipEventsWorkflowID,
		processWorkOSMembershipEventsDebounceSig,
		"enqueue",
		client.StartWorkflowOptions{
			ID:                       processWorkOSMembershipEventsWorkflowID,
			TaskQueue:                string(temporalEnv.Queue()),
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
			WorkflowRunTimeout:       15 * time.Minute,
			StartDelay:               10 * time.Second,
		},
		ProcessWorkOSMembershipEventsWorkflowDebounced,
	)
	if err != nil {
		return nil, fmt.Errorf("signal with start membership workflow: %w", err)
	}
	return run, nil
}

func ProcessWorkOSMembershipEventsWorkflowDebounced(ctx workflow.Context) (*activities.ProcessWorkOSMembershipEventsResult, error) {
	return Debounce(
		ProcessWorkOSMembershipEventsWorkflow,
		processWorkOSMembershipEventsDebounceSig,
		func(_ struct{}, result *activities.ProcessWorkOSMembershipEventsResult) bool {
			return result.HasMore
		},
	)(ctx, struct{}{})
}

func ProcessWorkOSMembershipEventsWorkflow(ctx workflow.Context, _ struct{}) (*activities.ProcessWorkOSMembershipEventsResult, error) {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			MaximumInterval:    time.Minute,
			BackoffCoefficient: 2,
			MaximumAttempts:    5,
		},
	})

	var processRes activities.ProcessWorkOSMembershipEventsResult
	if err := workflow.ExecuteActivity(ctx, a.ProcessWorkOSMembershipEvents, activities.ProcessWorkOSMembershipEventsParams{
		SinceEventID: nil,
	}).Get(ctx, &processRes); err != nil {
		return nil, fmt.Errorf("failed to process WorkOS membership events: %w", err)
	}

	result := &activities.ProcessWorkOSMembershipEventsResult{
		SinceEventID: processRes.SinceEventID,
		LastEventID:  processRes.LastEventID,
		HasMore:      processRes.HasMore,
	}
	if processRes.HasMore {
		return result, workflow.NewContinueAsNewError(ctx, ProcessWorkOSMembershipEventsWorkflow, struct{}{})
	}
	return result, nil
}

// AddReconcileWorkOSMembershipsSchedule sets up the periodic safety-net trigger for membership sync.
func AddReconcileWorkOSMembershipsSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: reconcileWorkOSMembershipsScheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{Every: 30 * time.Minute},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 reconcileWorkOSMembershipsScheduledRunID,
			Workflow:           ProcessWorkOSMembershipEventsWorkflowDebounced,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: 15 * time.Minute,
		},
	})
	if err != nil {
		return fmt.Errorf("create reconcile workos memberships schedule: %w", err)
	}

	return nil
}
