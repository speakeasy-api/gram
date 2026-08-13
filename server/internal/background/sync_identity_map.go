package background

// Identity map sync. The ClickHouse identity_map table folds each unambiguous
// directory or linked-account email to its owning user so analytics reads can
// resolve one employee to one identity (via joinGet). The schedule below is
// the sole trigger: every tick performs a full rebuild from Postgres into
// identity_map_staging and an atomic EXCHANGE TABLES swap, so link changes —
// including deletions — converge within one interval and readers never see a
// partial map. The interval is the accepted staleness bound for analytics
// folding; there is deliberately no write-path trigger yet.

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
	identityMapSyncScheduleID = "v1:identity-map-sync-schedule"
	identityMapSyncWorkflowID = identityMapSyncScheduleID + "/scheduled"
	identityMapSyncInterval   = 15 * time.Minute
)

func SyncIdentityMapWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.SyncIdentityMap).Get(ctx, nil); err != nil {
		return fmt.Errorf("sync identity map: %w", err)
	}
	return nil
}

func AddIdentityMapSyncSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: identityMapSyncInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 identityMapSyncWorkflowID,
		Workflow:           SyncIdentityMapWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: 10 * time.Minute,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      identityMapSyncScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		// The schedule survives deploys; push the current spec and action so
		// interval or workflow changes take effect on running environments.
		if err := sc.GetHandle(ctx, identityMapSyncScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update identity map sync schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create identity map sync schedule: %w", err)
	}
	return nil
}
