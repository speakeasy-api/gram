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

type ProcessWorkOSEventsParams struct {
	WorkOSOrganizationID string `json:"workos_organization_id,omitempty"`
}

type ProcessWorkOSEventsResult struct {
	HasMore bool `json:"has_more,omitempty"`
}

// func AddProcessWorkOSEventsSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
// 	scheduleID := "v1:process-workos-events-schedule"
// 	workflowID := "v1:process-workos-events/scheduled"

// 	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
// 		ID: scheduleID,
// 		Spec: client.ScheduleSpec{
// 			Intervals: []client.ScheduleIntervalSpec{
// 				{
// 					Every: 30 * time.Minute,
// 				},
// 			},
// 		},
// 		Action: &client.ScheduleWorkflowAction{
// 			ID:                 workflowID,
// 			Workflow:           ProcessWorkOSEventsWorkflow,
// 			TaskQueue:          string(temporalEnv.Queue()),
// 			WorkflowRunTimeout: 15 * time.Minute,
// 		},
// 	})
// 	if err != nil {
// 		return fmt.Errorf("failed to refresh billing usage schedule: %w", err)
// 	}

// 	return nil
// }

func processWorkOSOrganizationEventsWorkflowID(params ProcessWorkOSEventsParams) string {
	return fmt.Sprintf("v1:process-workos-org-events:%s", params.WorkOSOrganizationID)
}

func processWorkOSOrganizationEventsDebounceSignal(params ProcessWorkOSEventsParams) string {
	return fmt.Sprintf("%s/signal", processWorkOSOrganizationEventsWorkflowID(params))
}

func ExecuteProcessWorkOSOrganizationEventsWorkflowDebounced(ctx context.Context, temporalEnv *tenv.Environment, params ProcessWorkOSEventsParams) (client.WorkflowRun, error) {
	id := processWorkOSOrganizationEventsWorkflowID(params)
	sig := processWorkOSOrganizationEventsDebounceSignal(params)

	return temporalEnv.Client().SignalWithStartWorkflow(
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
}

func ProcessWorkOSOrganizationEventsWorkflowDebounced(ctx workflow.Context, params ProcessWorkOSEventsParams) (*ProcessWorkOSEventsResult, error) {
	sig := processWorkOSOrganizationEventsDebounceSignal(params)
	return Debounce(
		ProcessWorkOSOrganizationEventsWorkflow,
		sig,
		func(params ProcessWorkOSEventsParams, result *ProcessWorkOSEventsResult) bool {
			return result.HasMore
		},
	)(ctx, params)
}

func ProcessWorkOSOrganizationEventsWorkflow(ctx workflow.Context, params ProcessWorkOSEventsParams) (*ProcessWorkOSEventsResult, error) {
	// This can stay nil/unassigned. Temporal just uses this to get activity names.
	// The actual activities are registered in the CLI layer (internal/background/worker.go).
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

	return &ProcessWorkOSEventsResult{
		HasMore: processRes.HasMore,
	}, nil
}
