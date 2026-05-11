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
	assistantMemoriesReaperWorkflowID = "v1:assistant-memories-reaper"
	assistantMemoriesReaperScheduleID = "v1:assistant-memories-reaper-schedule"
	assistantMemoriesReaperInterval   = 7 * 24 * time.Hour

	// AssistantMemoryReapAfter is the grace period a soft-deleted
	// assistant_memories row stays around before the reaper hard-deletes
	// it. Long enough for accidental-delete recovery, short enough that
	// tombstones don't accumulate indefinitely.
	AssistantMemoryReapAfter = 30 * 24 * time.Hour
)

// AssistantMemoriesReaperWorkflow hard-deletes assistant_memories rows whose
// soft-delete tombstone is older than AssistantMemoryReapAfter. The
// underlying recursive CTE also collects every predecessor superseded by a
// reaped head, so historical chains drop on the same schedule.
func AssistantMemoriesReaperWorkflow(ctx workflow.Context) error {
	var a *Activities

	logger := workflow.GetLogger(ctx)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			MaximumInterval:    1 * time.Minute,
			BackoffCoefficient: 2,
		},
	})

	cutoff := workflow.Now(ctx).Add(-AssistantMemoryReapAfter)

	var rows int64
	if err := workflow.ExecuteActivity(ctx, a.ReapSoftDeletedAssistantMemories, cutoff).Get(ctx, &rows); err != nil {
		return fmt.Errorf("reap soft-deleted assistant memories: %w", err)
	}

	logger.Info("assistant memories reaper completed", "rows_affected", rows)

	return nil
}

func AddAssistantMemoriesReaperSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: assistantMemoriesReaperScheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: assistantMemoriesReaperInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 assistantMemoriesReaperWorkflowID,
			Workflow:           AssistantMemoriesReaperWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: 15 * time.Minute,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create assistant memories reaper schedule: %w", err)
	}
	return nil
}
