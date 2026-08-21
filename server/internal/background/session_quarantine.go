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
	sessionQuarantineReassertWorkflowID = "v1:session-quarantine-reassert"
	sessionQuarantineReassertScheduleID = "v1:session-quarantine-reassert-schedule"
	sessionQuarantineReassertInterval   = 30 * time.Second
	sessionQuarantineReassertTimeout    = 30 * time.Second
)

func SessionQuarantineReassertWorkflow(ctx workflow.Context) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: sessionQuarantineReassertTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(activityCtx, a.ReassertSessionQuarantines).Get(activityCtx, nil); err != nil {
		return fmt.Errorf("reassert session quarantines: %w", err)
	}
	return nil
}

func AddSessionQuarantineReassertSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := buildSessionQuarantineReassertScheduleOptions(temporalEnv)

	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create session quarantine reassert schedule: %w", err)
	}

	if err := scheduleClient.GetHandle(ctx, sessionQuarantineReassertScheduleID).Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			schedule := input.Description.Schedule
			schedule.Spec = &options.Spec
			schedule.Action = options.Action
			if schedule.Policy == nil {
				schedule.Policy = &client.SchedulePolicies{
					Overlap:        enums.SCHEDULE_OVERLAP_POLICY_SKIP,
					CatchupWindow:  0,
					PauseOnFailure: false,
				}
			}
			return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
		},
	}); err != nil {
		return fmt.Errorf("update session quarantine reassert schedule: %w", err)
	}
	return nil
}

func buildSessionQuarantineReassertScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	return client.ScheduleOptions{
		ID:      sessionQuarantineReassertScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: sessionQuarantineReassertInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 sessionQuarantineReassertWorkflowID,
			Workflow:           SessionQuarantineReassertWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: sessionQuarantineReassertTimeout,
		},
	}
}
