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
	// skillInjectionScanBatchSize mirrors the judge's per-Classify fan-out. Each
	// row is one ~10s judge call, so a small batch keeps the activity well under
	// its StartToCloseTimeout while the sweep loops until the backlog drains.
	// ponytail: fixed 8/min drains large first-deploy backlogs slowly; raise the
	// batch or add per-project fan-out if that latency ever matters.
	skillInjectionScanBatchSize  int32 = 8
	skillInjectionScanMaxBatches       = 20
	skillInjectionScanScheduleID       = "v1:skill-injection-scan-schedule"
	skillInjectionScanWorkflowID       = skillInjectionScanScheduleID + "/scheduled"
	skillInjectionScanInterval         = time.Minute
	// skillInjectionScanBatchTimeout bounds one batch activity. A batch is 8
	// ~10s judge calls, so this is generous headroom, not the expected duration.
	skillInjectionScanBatchTimeout = 5 * time.Minute
	// skillInjectionScanRunTimeout caps one drain run's liveness, it does not
	// guarantee a full drain: the 1-minute schedule (SKIP overlap) is the outer
	// retry loop, and each completed batch persists its verdicts, so a run killed
	// mid-drain simply resumes on the next tick with the remaining backlog. We keep
	// the cap short on purpose — a longer timeout would let one wedged run block
	// every scheduled tick behind it. Sized to cover a no-retry drain
	// (maxBatches * batchTimeout) plus slack for the low per-batch retry budget.
	skillInjectionScanRunTimeout = skillInjectionScanMaxBatches*skillInjectionScanBatchTimeout + 10*time.Minute
)

// ScanSkillVersionsForInjectionWorkflow drains the prompt-injection scan queue.
// Scanned rows leave the partial index, so each batch re-queries from the top
// without a cursor; the workflow is idempotent and safe to overlap-skip.
func ScanSkillVersionsForInjectionWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: skillInjectionScanBatchTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			// The schedule reruns every minute and is the real retry loop, so keep
			// per-batch retries low: one in-activity retry smooths a single blip
			// without the run's worst case ballooning past its timeout.
			MaximumAttempts:    2,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	var a *Activities
	for range skillInjectionScanMaxBatches {
		var result activities.ScanSkillVersionsForInjectionResult
		if err := workflow.ExecuteActivity(ctx, a.ScanSkillVersionsForInjection, activities.ScanSkillVersionsForInjectionParams{
			BatchSize: skillInjectionScanBatchSize,
		}).Get(ctx, &result); err != nil {
			return fmt.Errorf("scan skill versions for injection: %w", err)
		}
		if !result.HasMore {
			return nil
		}
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, ScanSkillVersionsForInjectionWorkflow)
		}
	}
	return workflow.NewContinueAsNewError(ctx, ScanSkillVersionsForInjectionWorkflow)
}

func AddSkillInjectionScanSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	spec := client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: skillInjectionScanInterval}}}
	action := &client.ScheduleWorkflowAction{
		ID:                 skillInjectionScanWorkflowID,
		Workflow:           ScanSkillVersionsForInjectionWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: skillInjectionScanRunTimeout,
	}

	_, err := scheduleClient.Create(ctx, client.ScheduleOptions{
		ID:      skillInjectionScanScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, skillInjectionScanScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
			},
		}); err != nil {
			return fmt.Errorf("update skill injection scan schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create skill injection scan schedule: %w", err)
	}
	return nil
}
