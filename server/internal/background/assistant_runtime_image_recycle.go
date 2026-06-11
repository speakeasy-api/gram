package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	assistantRuntimeImageRecycleWorkflowIDPrefix = "v1:assistant-runtime-image-recycle:"

	// assistantRuntimeImageRecycleActivityTimeout bounds one sweep. Each
	// stale row pays an in-place machine update: image pull + reboot +
	// health wait, serially. Liveness is enforced by the heartbeat timeout,
	// not this ceiling.
	assistantRuntimeImageRecycleActivityTimeout = 6 * time.Hour

	// assistantRuntimeImageRecycleHeartbeatTimeout must comfortably exceed
	// the worst-case time spent on a single row: a flaps update, a machine
	// start, the started wait and the 45s health wait.
	assistantRuntimeImageRecycleHeartbeatTimeout = 10 * time.Minute
)

type AssistantRuntimeImageRecycleWorkflowResult struct {
	Recycled int
	Skipped  int
	Errors   int
}

// AssistantRuntimeImageRecycleWorkflow sweeps every active v2 assistant
// runtime once and rolls idle machines onto the currently configured runtime
// image. Kicked at worker startup with a workflow ID keyed on the image
// reference, so concurrent replica kicks collapse into one fleet sweep; busy
// machines are skipped and picked up lazily by the per-admission recycle.
func AssistantRuntimeImageRecycleWorkflow(ctx workflow.Context) (*AssistantRuntimeImageRecycleWorkflowResult, error) {
	var a *Activities

	logger := workflow.GetLogger(ctx)

	// MaximumAttempts is 2 so a transient DB error on the candidate list does
	// not lose the deploy's only sweep; per-row Fly failures are swallowed
	// inside the activity and never retry.
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: assistantRuntimeImageRecycleActivityTimeout,
		HeartbeatTimeout:    assistantRuntimeImageRecycleHeartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    2,
			InitialInterval:    5 * time.Second,
			MaximumInterval:    1 * time.Minute,
			BackoffCoefficient: 2,
		},
	})

	var result activities.RecycleAssistantRuntimeImagesResult
	if err := workflow.ExecuteActivity(ctx, a.RecycleAssistantRuntimeImages).Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("recycle assistant runtime images: %w", err)
	}

	logger.Info("assistant runtime image recycle completed",
		"recycled", result.Recycled,
		"skipped", result.Skipped,
		"errors", result.Errors,
	)

	return &AssistantRuntimeImageRecycleWorkflowResult{
		Recycled: result.Recycled,
		Skipped:  result.Skipped,
		Errors:   result.Errors,
	}, nil
}

// KickAssistantRuntimeImageRecycle starts one image recycle sweep keyed on
// the given image reference. Concurrent kicks from sibling worker replicas
// collapse into a single run; once that run completes, a later kick (worker
// restart, rollback to a previously swept image) starts a fresh sweep.
func KickAssistantRuntimeImageRecycle(ctx context.Context, temporalEnv *tenv.Environment, imageRef string) error {
	wfID := assistantRuntimeImageRecycleWorkflowIDPrefix + imageRef
	// ALLOW_DUPLICATE rather than REJECT_DUPLICATE: a rollback re-deploys an
	// image ref whose sweep already completed, and it still needs a fresh
	// sweep to roll the fleet back. Concurrent kicks from sibling replicas
	// are still collapsed — starting a workflow whose ID is currently
	// running returns WorkflowExecutionAlreadyStarted regardless of reuse
	// policy. The residual cost is a redundant sweep after a worker restart
	// on an unchanged image, which no-ops row by row.
	_, err := temporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    wfID,
		TaskQueue:             string(temporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		// Sized for the activity's full retry budget (2 attempts at the
		// StartToClose ceiling) so the second attempt is never truncated.
		WorkflowRunTimeout: 2*assistantRuntimeImageRecycleActivityTimeout + 15*time.Minute,
	}, AssistantRuntimeImageRecycleWorkflow)
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &alreadyStarted) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("start assistant runtime image recycle workflow: %w", err)
	}
	return nil
}
