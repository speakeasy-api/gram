package background

import (
	"context"
	"errors"
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
	indexToolsetSweepInterval   = 5 * time.Minute
	indexToolsetSweepScanLimit  = 100
	indexToolsetSweepStartLimit = 10
)

func IndexToolsetSweepWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	var a *Activities
	var targets []activities.ToolsetIndexTarget
	if err := workflow.ExecuteActivity(ctx, a.ListToolsetsForIndexing, activities.ListToolsetsForIndexingInput{
		RotationSeed: workflow.Now(ctx).Unix() / int64(indexToolsetSweepInterval/time.Second),
		ScanLimit:    indexToolsetSweepScanLimit,
	}).Get(ctx, &targets); err != nil {
		return fmt.Errorf("discover toolsets for indexing: %w", err)
	}

	logger := workflow.GetLogger(ctx)
	started := 0
	var startErrors []error
	for _, target := range targets {
		params := IndexToolsetParams{
			ProjectID:             target.ProjectID,
			ToolsetSlug:           target.ToolsetSlug,
			IndexRevision:         target.IndexRevision,
			PermanentFailureCount: 0,
		}
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:            indexToolsetWorkflowID(params),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
			ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
		})
		if err := workflow.ExecuteChildWorkflow(childCtx, IndexToolsetWorkflow, params).
			GetChildWorkflowExecution().Get(childCtx, nil); err != nil {
			startErr := unexpectedIndexToolsetStartError(err)
			if startErr == nil {
				continue
			}

			logger.Error(
				"failed to start toolset indexing workflow",
				"project_id", params.ProjectID.String(),
				"toolset_slug", params.ToolsetSlug,
				"error", err.Error(),
			)
			startErrors = append(startErrors, startErr)
			continue
		}

		started++
		if started >= indexToolsetSweepStartLimit {
			break
		}
	}

	return errors.Join(startErrors...)
}

func unexpectedIndexToolsetStartError(err error) error {
	if temporal.IsWorkflowExecutionAlreadyStartedError(err) {
		return nil
	}

	return fmt.Errorf("start toolset indexing workflow: %w", err)
}

func indexToolsetSweepScheduleID(queue string) string {
	return fmt.Sprintf("v1:index-toolset-sweep:%s", queue)
}

func AddIndexToolsetSweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	queue := string(temporalEnv.Queue())
	scheduleID := indexToolsetSweepScheduleID(queue)
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:      scheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: indexToolsetSweepInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 scheduleID + "/scheduled",
			Workflow:           IndexToolsetSweepWorkflow,
			TaskQueue:          queue,
			WorkflowRunTimeout: 2 * time.Minute,
		},
	})
	if errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create index toolset sweep schedule: %w", err)
	}
	return nil
}
