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

type ProcessWorkOSEventsParams struct {
	WorkOSOrganizationID string `json:"workos_organization_id,omitempty"`
}

type ProcessWorkOSEventsResult struct {
	HasMore bool `json:"has_more,omitempty"`
}

func processWorkOSOrganizationEventsWorkflowID(params ProcessWorkOSEventsParams) string {
	return fmt.Sprintf("v1:process-workos-org-events:%s", params.WorkOSOrganizationID)
}

func processWorkOSOrganizationEventsDebounceSignal(params ProcessWorkOSEventsParams) string {
	return fmt.Sprintf("%s/signal", processWorkOSOrganizationEventsWorkflowID(params))
}

// ExecuteProcessWorkOSOrganizationEventsWorkflowDebounced is the public entry
// point used by future triggers (webhook, reconcile schedule, manual CLI). It
// uses signal-with-start so concurrent triggers for the same org collapse onto
// a single workflow execution.
func ExecuteProcessWorkOSOrganizationEventsWorkflowDebounced(ctx context.Context, temporalEnv *tenv.Environment, params ProcessWorkOSEventsParams) (client.WorkflowRun, error) {
	id := processWorkOSOrganizationEventsWorkflowID(params)
	sig := processWorkOSOrganizationEventsDebounceSignal(params)

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
			WorkflowRunTimeout:       15 * time.Minute,
			StartDelay:               10 * time.Second,
		},
		ProcessWorkOSOrganizationEventsWorkflowDebounced,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("signal with start workflow: %w", err)
	}
	return run, nil
}

func ProcessWorkOSOrganizationEventsWorkflowDebounced(ctx workflow.Context, params ProcessWorkOSEventsParams) (*ProcessWorkOSEventsResult, error) {
	sig := processWorkOSOrganizationEventsDebounceSignal(params)
	return Debounce(
		ProcessWorkOSOrganizationEventsWorkflow,
		sig,
		func(_ ProcessWorkOSEventsParams, result *ProcessWorkOSEventsResult) bool {
			return result.HasMore
		},
	)(ctx, params)
}

func ProcessWorkOSOrganizationEventsWorkflow(ctx workflow.Context, params ProcessWorkOSEventsParams) (*ProcessWorkOSEventsResult, error) {
	// Activities are registered on the worker; the receiver is only used here to
	// reference activity names.
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

	var processRes activities.ProcessWorkOSOrganizationEventsResult
	if err := workflow.ExecuteActivity(ctx, a.ProcessWorkOSOrganizationEvents, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: params.WorkOSOrganizationID,
		SinceEventID:         nil,
	}).Get(ctx, &processRes); err != nil {
		return nil, fmt.Errorf("failed to process WorkOS events: %w", err)
	}

	result := &ProcessWorkOSEventsResult{HasMore: processRes.HasMore}
	if processRes.HasMore {
		return result, workflow.NewContinueAsNewError(ctx, ProcessWorkOSOrganizationEventsWorkflow, params)
	}
	return result, nil
}
