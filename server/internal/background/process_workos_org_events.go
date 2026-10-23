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

// processWorkOSOrganizationEventsEmptyPageRetryDelay is how long a run waits
// before reading the events list again when its first read came back empty.
// WorkOS can deliver a webhook before the event is visible to the Events API,
// so a webhook-triggered run may find nothing. Without a second read the event
// would sit unapplied until some later event for the organization arrived.
const processWorkOSOrganizationEventsEmptyPageRetryDelay = 30 * time.Second

const processWorkOSOrganizationEventsEmptyPageRetryChangeID = "workos-org-events-empty-page-retry"

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
			// 30m fits the activity's worst-case retry budget (5 attempts ×
			// 5m StartToCloseTimeout + backoff). Tighter would cut the retry
			// policy off mid-retry on a sustained WorkOS/DB slowdown.
			WorkflowRunTimeout: 30 * time.Minute,
			// StartDelay is the debounce coalescing window: concurrent
			// SignalWithStartWorkflow calls within this window land on the same
			// pending execution rather than spawning serial runs. Tune this to
			// trade webhook latency against batching efficiency.
			StartDelay: 10 * time.Second,
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
	return Debounce(
		ProcessWorkOSOrganizationEventsWorkflow,          // wrapped: runs one page per execution
		ProcessWorkOSOrganizationEventsWorkflowDebounced, // continueAsSelf: keeps debounce on the next run
		processWorkOSOrganizationEventsDebounceSignal,
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

	processPage := func() (activities.ProcessWorkOSOrganizationEventsResult, error) {
		var res activities.ProcessWorkOSOrganizationEventsResult
		if err := workflow.ExecuteActivity(ctx, a.ProcessWorkOSOrganizationEvents, activities.ProcessWorkOSOrganizationEventsParams{
			WorkOSOrganizationID: params.WorkOSOrganizationID,
			SinceEventID:         nil,
		}).Get(ctx, &res); err != nil {
			return res, fmt.Errorf("failed to process WorkOS events: %w", err)
		}
		return res, nil
	}

	processRes, err := processPage()
	if err != nil {
		return nil, err
	}

	// An empty page right after a webhook usually means the triggering event
	// is not listable yet. Read once more after a short wait so it is applied
	// now rather than whenever the organization's next event arrives.
	if processRes.LastEventID == "" &&
		workflow.GetVersion(ctx, processWorkOSOrganizationEventsEmptyPageRetryChangeID, workflow.DefaultVersion, 1) == 1 {
		if err := workflow.Sleep(ctx, processWorkOSOrganizationEventsEmptyPageRetryDelay); err != nil {
			return nil, fmt.Errorf("wait before re-reading WorkOS events: %w", err)
		}
		processRes, err = processPage()
		if err != nil {
			return nil, err
		}
	}

	// HasMore continuation is delegated to the Debounce wrapper via reenqueue so
	// signals coming in during this run are coalesced with the next page.
	return &ProcessWorkOSEventsResult{HasMore: processRes.HasMore}, nil
}
