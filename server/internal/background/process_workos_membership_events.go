package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	processWorkOSMembershipEventsWorkflowID = "v1:process-workos-membership-events"
)

type ProcessWorkOSMembershipEventsWorkflowParams struct{}

func processWorkOSMembershipEventsDebounceSignal(ProcessWorkOSMembershipEventsWorkflowParams) string {
	return processWorkOSMembershipEventsWorkflowID + "/signal"
}

// ExecuteProcessWorkOSMembershipEventsWorkflowDebounced starts or signals the
// singleton membership stream workflow. Concurrent triggers collapse onto the
// active run and HasMore continues as the debounced wrapper.
func ExecuteProcessWorkOSMembershipEventsWorkflowDebounced(ctx context.Context, temporalEnv *tenv.Environment) (client.WorkflowRun, error) {
	params := ProcessWorkOSMembershipEventsWorkflowParams{}
	sig := processWorkOSMembershipEventsDebounceSignal(params)

	run, err := temporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		processWorkOSMembershipEventsWorkflowID,
		sig,
		"enqueue",
		client.StartWorkflowOptions{
			ID:                       processWorkOSMembershipEventsWorkflowID,
			TaskQueue:                string(temporalEnv.Queue()),
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
			WorkflowRunTimeout:       30 * time.Minute,
			StartDelay:               10 * time.Second,
		},
		ProcessWorkOSMembershipEventsWorkflowDebounced,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("signal with start WorkOS membership workflow: %w", err)
	}
	return run, nil
}

func ProcessWorkOSMembershipEventsWorkflowDebounced(ctx workflow.Context, params ProcessWorkOSMembershipEventsWorkflowParams) (*activities.ProcessWorkOSMembershipEventsResult, error) {
	return Debounce(
		ProcessWorkOSMembershipEventsWorkflow,
		ProcessWorkOSMembershipEventsWorkflowDebounced,
		processWorkOSMembershipEventsDebounceSignal,
		func(_ ProcessWorkOSMembershipEventsWorkflowParams, result *activities.ProcessWorkOSMembershipEventsResult) bool {
			return result.HasMore
		},
	)(ctx, params)
}

func ProcessWorkOSMembershipEventsWorkflow(ctx workflow.Context, _ ProcessWorkOSMembershipEventsWorkflowParams) (*activities.ProcessWorkOSMembershipEventsResult, error) {
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
		return nil, fmt.Errorf("process WorkOS membership events: %w", err)
	}

	return &activities.ProcessWorkOSMembershipEventsResult{
		SinceEventID: processRes.SinceEventID,
		LastEventID:  processRes.LastEventID,
		HasMore:      processRes.HasMore,
	}, nil
}
