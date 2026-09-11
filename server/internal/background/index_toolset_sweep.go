package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	indexToolsetSweepInterval     = 5 * time.Minute
	indexToolsetSweepProjectLimit = 100
	indexToolsetSweepScanLimit    = 100
	indexToolsetSweepStartLimit   = 10
	indexToolsetSweepRunTimeout   = 4 * time.Minute
)

type IndexToolsetSweepParams struct {
	ProjectIDs []uuid.UUID
}

func IndexToolsetSweepWorkflow(ctx workflow.Context, params IndexToolsetSweepParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	rotationSeed := workflow.Now(ctx).Unix() / int64(indexToolsetSweepInterval/time.Second)
	var a *Activities
	projectIDs := params.ProjectIDs
	if len(projectIDs) == 0 {
		if err := workflow.ExecuteActivity(ctx, a.ListProjectsForToolsetIndexing, activities.ListProjectsForToolsetIndexingInput{
			RotationSeed: rotationSeed,
			ProjectLimit: indexToolsetSweepProjectLimit,
		}).Get(ctx, &projectIDs); err != nil {
			return fmt.Errorf("discover projects for toolset indexing: %w", err)
		}
	}
	if len(projectIDs) == 0 {
		return nil
	}

	var targets []activities.ToolsetIndexTarget
	if err := workflow.ExecuteActivity(ctx, a.ListToolsetsForIndexing, activities.ListToolsetsForIndexingInput{
		RotationSeed: rotationSeed,
		ScanLimit:    indexToolsetSweepScanLimit,
		ProjectIDs:   projectIDs,
	}).Get(ctx, &targets); err != nil {
		return fmt.Errorf("discover toolsets for indexing: %w", err)
	}

	logger := workflow.GetLogger(ctx)
	started := 0
	var startErrors []error
	for _, target := range targets {
		params := IndexToolsetParams{
			ProjectID:             target.ProjectID,
			ToolsetID:             target.ToolsetID,
			ToolsetSlug:           target.ToolsetSlug,
			ToolsetVersion:        target.ToolsetVersion,
			DeploymentID:          target.DeploymentID,
			PermanentFailureCount: 0,
		}
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:            indexToolsetWorkflowID(params),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
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

func StartIndexToolsetSweepForDeployment(ctx workflow.Context, projectID, deploymentID uuid.UUID) error {
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:            fmt.Sprintf("v1:index-toolset-deployment-sweep:%s", deploymentID),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
	})
	err := workflow.ExecuteChildWorkflow(childCtx, IndexToolsetSweepWorkflow, IndexToolsetSweepParams{
		ProjectIDs: []uuid.UUID{projectID},
	}).GetChildWorkflowExecution().Get(childCtx, nil)
	if temporal.IsWorkflowExecutionAlreadyStartedError(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("start deployment toolset indexing sweep: %w", err)
	}
	return nil
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

func indexToolsetSweepScheduleOptions(queue string) client.ScheduleOptions {
	scheduleID := indexToolsetSweepScheduleID(queue)
	return client.ScheduleOptions{
		ID:      scheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: indexToolsetSweepInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 scheduleID + "/scheduled",
			Workflow:           IndexToolsetSweepWorkflow,
			Args:               []any{IndexToolsetSweepParams{ProjectIDs: nil}},
			TaskQueue:          queue,
			WorkflowRunTimeout: indexToolsetSweepRunTimeout,
		},
	}
}

func AddIndexToolsetSweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	queue := string(temporalEnv.Queue())
	scheduleID := indexToolsetSweepScheduleID(queue)
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := indexToolsetSweepScheduleOptions(queue)

	_, err := scheduleClient.Create(ctx, options)
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, scheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &options.Spec
				input.Description.Schedule.Action = options.Action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update index toolset sweep schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create index toolset sweep schedule: %w", err)
	}
	return nil
}
