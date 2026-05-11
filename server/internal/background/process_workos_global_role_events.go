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

const processWorkOSGlobalRoleEventsWorkflowID = "v1:process-workos-global-role-events"

type ProcessWorkOSGlobalRoleEventsWorkflowParams struct{}

func processWorkOSGlobalRoleEventsDebounceSignal(ProcessWorkOSGlobalRoleEventsWorkflowParams) string {
	return processWorkOSGlobalRoleEventsWorkflowID + "/signal"
}

// ExecuteProcessWorkOSGlobalRoleEventsWorkflowDebounced starts or signals the
// singleton global role events workflow. Concurrent triggers collapse onto the
// active run.
func ExecuteProcessWorkOSGlobalRoleEventsWorkflowDebounced(ctx context.Context, temporalEnv *tenv.Environment) (client.WorkflowRun, error) {
	params := ProcessWorkOSGlobalRoleEventsWorkflowParams{}
	sig := processWorkOSGlobalRoleEventsDebounceSignal(params)

	run, err := temporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		processWorkOSGlobalRoleEventsWorkflowID,
		sig,
		"enqueue",
		client.StartWorkflowOptions{
			ID:                       processWorkOSGlobalRoleEventsWorkflowID,
			TaskQueue:                string(temporalEnv.Queue()),
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
			WorkflowRunTimeout:       30 * time.Minute,
			StartDelay:               10 * time.Second,
		},
		ProcessWorkOSGlobalRoleEventsWorkflowDebounced,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("signal with start global role events workflow: %w", err)
	}
	return run, nil
}

func ProcessWorkOSGlobalRoleEventsWorkflowDebounced(ctx workflow.Context, params ProcessWorkOSGlobalRoleEventsWorkflowParams) (*activities.ProcessWorkOSGlobalRoleEventsResult, error) {
	return Debounce(
		ProcessWorkOSGlobalRoleEventsWorkflow,
		ProcessWorkOSGlobalRoleEventsWorkflowDebounced,
		processWorkOSGlobalRoleEventsDebounceSignal,
		func(_ ProcessWorkOSGlobalRoleEventsWorkflowParams, result *activities.ProcessWorkOSGlobalRoleEventsResult) bool {
			return result.HasMore
		},
	)(ctx, params)
}

func ProcessWorkOSGlobalRoleEventsWorkflow(ctx workflow.Context, _ ProcessWorkOSGlobalRoleEventsWorkflowParams) (*activities.ProcessWorkOSGlobalRoleEventsResult, error) {
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

	var processRes activities.ProcessWorkOSGlobalRoleEventsResult
	if err := workflow.ExecuteActivity(ctx, a.ProcessWorkOSGlobalRoleEvents, activities.ProcessWorkOSGlobalRoleEventsParams{
		SinceEventID: nil,
	}).Get(ctx, &processRes); err != nil {
		return nil, fmt.Errorf("process WorkOS global role events: %w", err)
	}

	return &activities.ProcessWorkOSGlobalRoleEventsResult{
		SinceEventID: processRes.SinceEventID,
		LastEventID:  processRes.LastEventID,
		HasMore:      processRes.HasMore,
	}, nil
}
