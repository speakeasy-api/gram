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

// FleetHealthCacheRefresher populates the shared cache without an HTTP request.
type FleetHealthCacheRefresher interface {
	Refresh(context.Context) error
}

const (
	// fleetHealthCacheActivity names the shared-cache refresh registered by the worker.
	fleetHealthCacheActivity = "RefreshFleetHealthCache"
	// fleetHealthCacheAttemptTimeout gives the five-minute reader budget time to report failure.
	fleetHealthCacheAttemptTimeout = 6 * time.Minute
	// fleetHealthCacheMaxAttempts bounds retries during a storage outage.
	fleetHealthCacheMaxAttempts = 3
	// fleetHealthCacheRetryInterval avoids immediately repeating expensive reads after an outage.
	fleetHealthCacheRetryInterval = time.Minute
	// fleetHealthCacheRunTimeout covers three attempts, backoff, and queue headroom.
	fleetHealthCacheRunTimeout = 25 * time.Minute
	// fleetHealthCacheCatchupWindow allows a nightly run delayed by worker maintenance.
	fleetHealthCacheCatchupWindow = 12 * time.Hour
)

// FleetHealthCacheWorkflow warms the default staff page window with one activity.
func FleetHealthCacheWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: fleetHealthCacheAttemptTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: fleetHealthCacheMaxAttempts,
			InitialInterval: fleetHealthCacheRetryInterval,
		},
	})
	if err := workflow.ExecuteActivity(ctx, fleetHealthCacheActivity).Get(ctx, nil); err != nil {
		return fmt.Errorf("refresh fleet health cache: %w", err)
	}
	return nil
}

// addFleetHealthCacheSchedule warms the cache at 02:00 UTC and once on creation.
// Only the shared worker owns this fleet-wide schedule; previews cannot replace it.
func addFleetHealthCacheSchedule(ctx context.Context, env *tenv.Environment) error {
	if env.Queue() != sharedTaskQueue {
		return nil
	}
	id := fmt.Sprintf("v1:fleet-health-cache:%s", env.Queue())
	_, err := env.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:   id,
		Spec: client.ScheduleSpec{CronExpressions: []string{"0 2 * * *"}, TimeZoneName: "UTC"},
		Action: &client.ScheduleWorkflowAction{
			ID:                 id + "/scheduled",
			Workflow:           FleetHealthCacheWorkflow,
			TaskQueue:          string(env.Queue()),
			WorkflowRunTimeout: fleetHealthCacheRunTimeout,
		},
		Overlap:            enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow:      fleetHealthCacheCatchupWindow,
		TriggerImmediately: true,
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create fleet health cache schedule: %w", err)
	}
	return nil
}
