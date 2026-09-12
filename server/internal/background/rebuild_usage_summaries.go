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

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	rebuildUsageSummariesInterval            = time.Hour
	rebuildUsageSummariesActivityTimeout     = 55 * time.Minute
	rebuildUsageSummariesWorkflowRunTimeout  = time.Hour
	rebuildUsageSummariesActivityMaxAttempts = 3
)

func RebuildUsageSummariesWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: rebuildUsageSummariesActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Minute,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    rebuildUsageSummariesActivityMaxAttempts,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.RebuildUsageSummaries, workflow.Now(ctx).UTC()).Get(ctx, nil); err != nil {
		return fmt.Errorf("rebuild usage summaries: %w", err)
	}
	return nil
}

func rebuildUsageSummariesScheduleID(taskQueue string) string {
	return fmt.Sprintf("v1:rebuild-usage-summaries-schedule:%s", taskQueue)
}

func rebuildUsageSummariesWorkflowID(taskQueue string) string {
	return fmt.Sprintf("v1:rebuild-usage-summaries:%s", taskQueue)
}

func AddRebuildUsageSummariesSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	taskQueue := string(temporalEnv.Queue())
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: rebuildUsageSummariesScheduleID(taskQueue),
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: rebuildUsageSummariesInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 rebuildUsageSummariesWorkflowID(taskQueue),
			Workflow:           RebuildUsageSummariesWorkflow,
			TaskQueue:          taskQueue,
			WorkflowRunTimeout: rebuildUsageSummariesWorkflowRunTimeout,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create usage summary rebuild schedule: %w", err)
	}
	return nil
}
