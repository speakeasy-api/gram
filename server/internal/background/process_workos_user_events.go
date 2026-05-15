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

const processWorkOSUserEventsWorkflowID = "v1:process-workos-user-events"

type ProcessWorkOSUserEventsWorkflowParams struct {
	WorkOSUserID string `json:"workos_user_id"`
}

func processWorkOSUserEventsWorkflowIDForParams(params ProcessWorkOSUserEventsWorkflowParams) string {
	return fmt.Sprintf("%s:%s", processWorkOSUserEventsWorkflowID, params.WorkOSUserID)
}

func processWorkOSUserEventsDebounceSignal(ProcessWorkOSUserEventsWorkflowParams) string {
	return processWorkOSUserEventsWorkflowID + "/signal"
}

func ExecuteProcessWorkOSUserEventsWorkflowDebounced(ctx context.Context, temporalEnv *tenv.Environment, params ProcessWorkOSUserEventsWorkflowParams) (client.WorkflowRun, error) {
	id := processWorkOSUserEventsWorkflowIDForParams(params)
	sig := processWorkOSUserEventsDebounceSignal(params)

	run, err := temporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		id,
		sig,
		"enqueue",
		client.StartWorkflowOptions{
			ID:                       id,
			TaskQueue:                string(temporalEnv.Queue()),
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
			WorkflowRunTimeout:       30 * time.Minute,
			StartDelay:               10 * time.Second,
		},
		ProcessWorkOSUserEventsWorkflowDebounced,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("signal with start user events workflow: %w", err)
	}
	return run, nil
}

func ProcessWorkOSUserEventsWorkflowDebounced(ctx workflow.Context, params ProcessWorkOSUserEventsWorkflowParams) (*activities.ProcessWorkOSUserEventsResult, error) {
	return Debounce(
		ProcessWorkOSUserEventsWorkflow,
		ProcessWorkOSUserEventsWorkflowDebounced,
		processWorkOSUserEventsDebounceSignal,
		func(_ ProcessWorkOSUserEventsWorkflowParams, result *activities.ProcessWorkOSUserEventsResult) bool {
			return result.HasMore
		},
	)(ctx, params)
}

func ProcessWorkOSUserEventsWorkflow(ctx workflow.Context, params ProcessWorkOSUserEventsWorkflowParams) (*activities.ProcessWorkOSUserEventsResult, error) {
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

	var processRes activities.ProcessWorkOSUserEventsResult
	if err := workflow.ExecuteActivity(ctx, a.ProcessWorkOSUserEvents, activities.ProcessWorkOSUserEventsParams{
		WorkOSUserID: params.WorkOSUserID,
		SinceEventID: nil,
	}).Get(ctx, &processRes); err != nil {
		return nil, fmt.Errorf("process WorkOS user events: %w", err)
	}

	return &activities.ProcessWorkOSUserEventsResult{
		SinceEventID: processRes.SinceEventID,
		LastEventID:  processRes.LastEventID,
		HasMore:      processRes.HasMore,
	}, nil
}
