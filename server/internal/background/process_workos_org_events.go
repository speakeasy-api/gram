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

func ReconcileWorkOSOrganizationsWorkflow(ctx workflow.Context) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 1 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	var workosOrgIDs []string
	if err := workflow.ExecuteActivity(ctx, a.GetAllWorkOSLinkedOrganizations).Get(ctx, &workosOrgIDs); err != nil {
		return fmt.Errorf("get workos linked organizations: %w", err)
	}

	for _, workosOrgID := range workosOrgIDs {
		params := ProcessWorkOSEventsParams{WorkOSOrganizationID: workosOrgID}
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:            processWorkOSOrganizationEventsWorkflowID(params),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
		})
		// Wait only for dispatch, not completion. Ignore errors from orgs already syncing.
		var childExec workflow.Execution
		if err := workflow.ExecuteChildWorkflow(childCtx, ProcessWorkOSOrganizationEventsWorkflowDebounced, params).
			GetChildWorkflowExecution().Get(ctx, &childExec); err != nil {
			workflow.GetLogger(ctx).Warn("skipping workos org sync", "workos_org_id", workosOrgID, "error", err)
		}
	}

	return nil
}

func AddReconcileWorkOSOrganizationsSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleID := "v1:reconcile-workos-organizations-schedule"
	workflowID := "v1:reconcile-workos-organizations/scheduled"

	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: scheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{Every: 30 * time.Minute},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 workflowID,
			Workflow:           ReconcileWorkOSOrganizationsWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: 15 * time.Minute,
		},
	})
	if err != nil {
		return fmt.Errorf("create reconcile workos organizations schedule: %w", err)
	}

	return nil
}

func processWorkOSOrganizationEventsWorkflowID(params ProcessWorkOSEventsParams) string {
	return fmt.Sprintf("v1:process-workos-org-events:%s", params.WorkOSOrganizationID)
}

func processWorkOSOrganizationEventsDebounceSignal(params ProcessWorkOSEventsParams) string {
	return fmt.Sprintf("%s/signal", processWorkOSOrganizationEventsWorkflowID(params))
}

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

	result := &ProcessWorkOSEventsResult{HasMore: processRes.HasMore}
	if processRes.HasMore {
		return result, workflow.NewContinueAsNewError(ctx, ProcessWorkOSOrganizationEventsWorkflow, params)
	}
	return result, nil
}
