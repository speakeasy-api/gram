package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

// setScheduleCatchup changes only outage recovery policy, preserving operator
// pauses, notes, overlap behavior, and pause-on-failure. Zero means the server's
// default window, not disabled catchup.
func setScheduleCatchup(schedule *client.Schedule, window time.Duration) {
	if schedule.Policy == nil {
		schedule.Policy = &client.SchedulePolicies{
			Overlap:        enums.SCHEDULE_OVERLAP_POLICY_SKIP,
			CatchupWindow:  window,
			PauseOnFailure: false,
		}
	} else {
		schedule.Policy.CatchupWindow = window
	}
}

// createScheduleWithCatchup migrates policy for registrations that otherwise
// leave existing specs/actions alone. A preview cannot update another queue's
// schedule. Registrations with action/spec reconciliation update policy there.
func createScheduleWithCatchup(ctx context.Context, sc client.ScheduleClient, options client.ScheduleOptions) (client.ScheduleHandle, error) {
	handle, err := sc.Create(ctx, options)
	if err == nil {
		return handle, nil
	}
	if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return nil, fmt.Errorf("create schedule %s: %w", options.ID, err)
	}
	handle = sc.GetHandle(ctx, options.ID)
	err = handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			current, currentOK := input.Description.Schedule.Action.(*client.ScheduleWorkflowAction)
			desired, desiredOK := options.Action.(*client.ScheduleWorkflowAction)
			if !currentOK || !desiredOK || current.TaskQueue != desired.TaskQueue {
				return nil, fmt.Errorf("schedule %s is owned by another task queue", options.ID)
			}
			setScheduleCatchup(&input.Description.Schedule, options.CatchupWindow)
			return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("update schedule %s catchup: %w", options.ID, err)
	}
	return handle, nil
}
