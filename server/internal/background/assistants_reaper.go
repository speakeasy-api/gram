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
	assistantReaperWorkflowID = "v1:assistant-reaper"
	assistantReaperScheduleID = "v1:assistant-reaper-schedule"
	assistantReaperInterval   = 60 * time.Second
)

// AssistantReaperWorkflow is a singleton sweep that reclaims runtime rows
// and events abandoned by crashed workers/servers. A Temporal Schedule
// fires it every minute; per-assistant coordinator workflows no longer run
// their own periodic reap. Affected assistants get a kick signal so their
// coordinator re-admits promptly instead of waiting on organic traffic.
func AssistantReaperWorkflow(ctx workflow.Context) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	var result activities.ReapStuckAssistantRuntimesResult
	if err := workflow.ExecuteActivity(ctx, a.ReapStuckAssistantRuntimes).Get(ctx, &result); err != nil {
		return fmt.Errorf("reap stuck assistant runtimes: %w", err)
	}

	for _, assistantID := range result.AffectedAssistantIDs {
		if err := workflow.ExecuteActivity(ctx, a.SignalAssistantCoordinator, activities.SignalAssistantCoordinatorInput{
			AssistantID: assistantID,
		}).Get(ctx, nil); err != nil {
			return fmt.Errorf("signal coordinator after reap: %w", err)
		}
	}

	return nil
}

func AddAssistantReaperSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: assistantReaperScheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: assistantReaperInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 assistantReaperWorkflowID,
			Workflow:           AssistantReaperWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: 5 * time.Minute,
		},
		// Skip overlapping runs so a slow DB doesn't stack reapers.
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create assistant reaper schedule: %w", err)
	}
	return nil
}
