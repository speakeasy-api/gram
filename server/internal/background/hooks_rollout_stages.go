package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hooksrollout"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// HooksRolloutStageAdvancer moves a running staged hooks rollout to its next
// stage once its soak has passed.
type HooksRolloutStageAdvancer interface {
	Advance(context.Context) error
}

type hooksRolloutStageAdvancer struct {
	logger *slog.Logger
	stages *hooksrollout.StageStore
}

func newHooksRolloutStageAdvancer(logger *slog.Logger, db hooksrollout.Beginner) *hooksRolloutStageAdvancer {
	return &hooksRolloutStageAdvancer{logger: logger, stages: hooksrollout.NewStageStore(db)}
}

func (a *hooksRolloutStageAdvancer) Advance(ctx context.Context) error {
	stage, err := a.stages.Advance(ctx, time.Now())
	if err != nil {
		return fmt.Errorf("advance hooks rollout stage: %w", err)
	}
	if stage != nil {
		a.logger.InfoContext(ctx, "advanced staged hooks rollout",
			attr.SlogValueInt(stage.Percent),
			attr.SlogValueString(string(stage.State)),
		)
	}
	return nil
}

const (
	hooksRolloutStagesActivity       = "AdvanceHooksRolloutStage"
	hooksRolloutStagesAttemptTimeout = time.Minute
	hooksRolloutStagesMaxAttempts    = 3
	hooksRolloutStagesRetryInterval  = 30 * time.Second
	hooksRolloutStagesRunTimeout     = 10 * time.Minute
)

// HooksRolloutStagesWorkflow advances the staged hooks rollout with one activity.
func HooksRolloutStagesWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: hooksRolloutStagesAttemptTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: hooksRolloutStagesMaxAttempts,
			InitialInterval: hooksRolloutStagesRetryInterval,
		},
	})
	if err := workflow.ExecuteActivity(ctx, hooksRolloutStagesActivity).Get(ctx, nil); err != nil {
		return fmt.Errorf("advance hooks rollout stage: %w", err)
	}
	return nil
}

// addHooksRolloutStagesSchedule checks the staged rollout hourly, so a stage
// moves on within an hour of its soak ending. Only the shared worker owns this
// fleet-wide schedule.
func addHooksRolloutStagesSchedule(ctx context.Context, env *tenv.Environment) error {
	if env.Queue() != sharedTaskQueue {
		return nil
	}
	id := fmt.Sprintf("v1:hooks-rollout-stages:%s", env.Queue())
	_, err := env.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:   id,
		Spec: client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: time.Hour}}},
		Action: &client.ScheduleWorkflowAction{
			ID:                 id + "/scheduled",
			Workflow:           HooksRolloutStagesWorkflow,
			TaskQueue:          string(env.Queue()),
			WorkflowRunTimeout: hooksRolloutStagesRunTimeout,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create hooks rollout stages schedule: %w", err)
	}
	return nil
}
