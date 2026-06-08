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

const processWorkOSDirectoryAttributesEventsWorkflowID = "v1:process-workos-directory-attributes-events"

type ProcessWorkOSDirectoryAttributesEventsWorkflowParams struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
}

func processWorkOSDirectoryAttributesEventsWorkflowIDForParams(params ProcessWorkOSDirectoryAttributesEventsWorkflowParams) string {
	return fmt.Sprintf("%s:%s:%s", processWorkOSDirectoryAttributesEventsWorkflowID, params.EntityType, params.EntityID)
}

func processWorkOSDirectoryAttributesEventsDebounceSignal(ProcessWorkOSDirectoryAttributesEventsWorkflowParams) string {
	return processWorkOSDirectoryAttributesEventsWorkflowID + "/signal"
}

func ExecuteProcessWorkOSDirectoryAttributesEventsWorkflowDebounced(ctx context.Context, temporalEnv *tenv.Environment, params ProcessWorkOSDirectoryAttributesEventsWorkflowParams) (client.WorkflowRun, error) {
	id := processWorkOSDirectoryAttributesEventsWorkflowIDForParams(params)
	sig := processWorkOSDirectoryAttributesEventsDebounceSignal(params)

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
		ProcessWorkOSDirectoryAttributesEventsWorkflowDebounced,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("signal with start directory attributes events workflow: %w", err)
	}
	return run, nil
}

func ProcessWorkOSDirectoryAttributesEventsWorkflowDebounced(ctx workflow.Context, params ProcessWorkOSDirectoryAttributesEventsWorkflowParams) (*activities.ProcessWorkOSDirectoryAttributesEventsResult, error) {
	return Debounce(
		ProcessWorkOSDirectoryAttributesEventsWorkflow,
		ProcessWorkOSDirectoryAttributesEventsWorkflowDebounced,
		processWorkOSDirectoryAttributesEventsDebounceSignal,
		func(_ ProcessWorkOSDirectoryAttributesEventsWorkflowParams, result *activities.ProcessWorkOSDirectoryAttributesEventsResult) bool {
			return result.HasMore
		},
	)(ctx, params)
}

func ProcessWorkOSDirectoryAttributesEventsWorkflow(ctx workflow.Context, params ProcessWorkOSDirectoryAttributesEventsWorkflowParams) (*activities.ProcessWorkOSDirectoryAttributesEventsResult, error) {
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

	var processRes activities.ProcessWorkOSDirectoryAttributesEventsResult
	if err := workflow.ExecuteActivity(ctx, a.ProcessWorkOSDirectoryAttributesEvents, activities.ProcessWorkOSDirectoryAttributesEventsParams{
		EntityType:   params.EntityType,
		EntityID:     params.EntityID,
		SinceEventID: nil,
	}).Get(ctx, &processRes); err != nil {
		return nil, fmt.Errorf("process WorkOS directory attributes events: %w", err)
	}

	return &activities.ProcessWorkOSDirectoryAttributesEventsResult{
		SinceEventID: processRes.SinceEventID,
		LastEventID:  processRes.LastEventID,
		HasMore:      processRes.HasMore,
	}, nil
}
