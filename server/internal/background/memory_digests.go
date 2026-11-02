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
	"github.com/speakeasy-api/gram/server/internal/memorydigest"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	memoryDigestsScheduleID = "v1:memory-digests-schedule"
	memoryDigestsWorkflowID = "v1:memory-digests-schedule/scheduled"

	// memoryDigestsHourUTC runs the rollup an hour after midnight UTC, so
	// chats that went idle just before midnight have their summaries.
	memoryDigestsHourUTC = 1

	// memoryDigestsCatchupWindow lets a run start up to a day late after an
	// outage. The job finds every missing period in its lookback, so a
	// skipped tick only delays digests.
	memoryDigestsCatchupWindow = 23 * time.Hour

	// memoryDigestsRunTimeout fits a full backfill run when each batch takes
	// about ten minutes (up to maxMemoryDigestBatchesPerLevel batches for each
	// of the three levels). A run that hits it stops, and the next night
	// resumes. It stays under a day so a run never overlaps the next tick.
	memoryDigestsRunTimeout = 20 * time.Hour

	// maxMemoryDigestBatchesPerLevel bounds one run's work per level. Each
	// batch writes up to 40 digests; the first run after an org enables
	// the flag needs about 90 day digests per active project, and later
	// runs need one batch. Anything left over is picked up the next night.
	maxMemoryDigestBatchesPerLevel = 25
)

// MemoryDigestsWorkflow writes the day, week and month team memory digests
// for every period that has closed and has no digest yet. Levels run in
// order so each folds children written earlier in the same run. A level
// that had failures, or still had candidates when it hit the batch cap,
// stops the run: folding a week or month over a missing child would write an
// incomplete digest that is never rewritten. The next run picks it up.
func MemoryDigestsWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 20 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
		},
	})
	logger := workflow.GetLogger(ctx)
	now := workflow.Now(ctx).UTC()

	var a *Activities
	for _, level := range []memorydigest.Level{memorydigest.LevelDay, memorydigest.LevelWeek, memorydigest.LevelMonth} {
		failed := 0
		pending := true
		for range maxMemoryDigestBatchesPerLevel {
			var result activities.BuildMemoryDigestsResult
			if err := workflow.ExecuteActivity(ctx, a.BuildMemoryDigests, activities.BuildMemoryDigestsArgs{
				Level: string(level),
				Now:   now,
			}).Get(ctx, &result); err != nil {
				return fmt.Errorf("build %s digests: %w", level, err)
			}
			logger.Info("memory digest batch done", "level", string(level), "built", result.Built, "skipped", result.Skipped, "failed", result.Failed)
			failed += result.Failed
			// A batch that wrote nothing would list the same candidates
			// again, so stop rather than spin on them.
			if !result.More || result.Built == 0 {
				pending = false
				break
			}
		}
		if failed > 0 || pending {
			logger.Warn("memory digests stopped before higher levels", "level", string(level), "failed", failed, "pending", pending)
			return nil
		}
	}
	return nil
}

// AddMemoryDigestsSchedule runs MemoryDigestsWorkflow daily at 01:00 UTC.
func AddMemoryDigestsSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := createScheduleWithCatchup(ctx, temporalEnv.Client().ScheduleClient(), client.ScheduleOptions{
		CatchupWindow: memoryDigestsCatchupWindow,
		ID:            memoryDigestsScheduleID,
		Spec: client.ScheduleSpec{
			Calendars: []client.ScheduleCalendarSpec{
				{
					Second:     nil,
					Minute:     nil,
					Hour:       []client.ScheduleRange{{Start: memoryDigestsHourUTC, End: 0, Step: 0}},
					DayOfMonth: nil,
					Month:      nil,
					Year:       nil,
					DayOfWeek:  nil,
					Comment:    "team memory digests, daily 01:00 UTC",
				},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 memoryDigestsWorkflowID,
			Workflow:           MemoryDigestsWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: memoryDigestsRunTimeout,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil {
		return fmt.Errorf("create memory digests schedule: %w", err)
	}
	return nil
}
