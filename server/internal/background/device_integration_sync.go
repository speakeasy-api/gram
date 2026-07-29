package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// The device integration sync scheduler mirrors the AI usage poller: a
// cron-scheduled coordinator fans out one child workflow per due sync, with
// workflow-id dedupe so a slow sync is never started twice. Payloads carry
// sync ids only — credentials are decrypted inside the activity, never in
// Temporal history.
const (
	deviceIntegrationSyncCoordinatorWorkflowID = "v1:device-integration-sync-coordinator"
	deviceIntegrationSyncCoordinatorScheduleID = "v1:device-integration-sync-coordinator-schedule"

	// deviceIntegrationSyncCoordinatorInterval is how often the coordinator
	// looks for due syncs. Schedules declare their own cadence; this only
	// bounds pickup latency.
	deviceIntegrationSyncCoordinatorInterval = 5 * time.Minute

	// deviceIntegrationSyncCoordinatorRunTimeout bounds one coordinator run,
	// including the children it waits on. Batches run serially, so this must
	// cover SEVERAL sequential child budgets (2h schedule-to-close each), or
	// a timing-out coordinator would terminate in-flight syncs via the
	// default parent-close policy. Matches the AI usage poller's 8h.
	deviceIntegrationSyncCoordinatorRunTimeout = 8 * time.Hour

	// deviceIntegrationSyncCoordinatorChildConcurrency caps how many sync
	// children one coordinator pass starts per batch.
	deviceIntegrationSyncCoordinatorChildConcurrency = 25

	deviceIntegrationSyncCoordinatorActivityTimeout      = 30 * time.Second
	deviceIntegrationSyncCoordinatorRetryInitialInterval = 5 * time.Second

	// deviceIntegrationSyncActivityTimeout is the budget for one full sync
	// run — a paginated inventory pull over a large fleet included.
	deviceIntegrationSyncActivityTimeout = 30 * time.Minute

	// deviceIntegrationSyncActivityScheduleToCloseTimeout bounds the run
	// including its retries. Business failures record sync state and return
	// nil, so retries here cover infrastructure errors only.
	deviceIntegrationSyncActivityScheduleToCloseTimeout = 2 * time.Hour

	deviceIntegrationSyncActivityMaxAttempts          = 3
	deviceIntegrationSyncActivityRetryInitialInterval = time.Minute
	deviceIntegrationSyncActivityRetryMaximumInterval = 15 * time.Minute
)

func DeviceIntegrationSyncCoordinatorWorkflow(ctx workflow.Context) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: deviceIntegrationSyncCoordinatorActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    deviceIntegrationSyncCoordinatorRetryInitialInterval,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities

	// A child that fails on infrastructure errors leaves its sync still due,
	// so attempted ids are excluded at the QUERY so re-listing cannot loop on
	// them — and, because the exclusion happens server-side, stuck syncs at
	// the head of the due list cannot starve later candidates out of the
	// LIMIT window. One attempt per sync per coordinator pass; the next
	// scheduled pass retries.
	attempted := map[string]bool{}
	var attemptedIDs []uuid.UUID

	for {
		var candidates []deviceintegrations.SyncCandidate
		if err := workflow.ExecuteActivity(activityCtx, a.GetDeviceIntegrationSyncCandidates, activities.GetDeviceIntegrationSyncCandidatesInput{
			Limit:          deviceIntegrationSyncCoordinatorChildConcurrency,
			ExcludeSyncIDs: attemptedIDs,
		}).Get(activityCtx, &candidates); err != nil {
			return fmt.Errorf("get device integration sync candidates: %w", err)
		}
		fresh := candidates[:0]
		for _, candidate := range candidates {
			if !attempted[candidate.SyncID.String()] {
				fresh = append(fresh, candidate)
			}
		}
		candidates = fresh
		if len(candidates) == 0 {
			break
		}

		type runningSync struct {
			candidate deviceintegrations.SyncCandidate
			child     workflow.ChildWorkflowFuture
		}

		batch := make([]runningSync, 0, len(candidates))
		for _, candidate := range candidates {
			attempted[candidate.SyncID.String()] = true
			attemptedIDs = append(attemptedIDs, candidate.SyncID)
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:            buildDeviceIntegrationSyncWorkflowID(candidate.OrganizationSlug, candidate.SyncID),
				WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
				// ABANDON: if the coordinator dies or times out, in-flight
				// syncs run to completion; workflow-id dedupe keeps the next
				// pass from double-starting them.
				ParentClosePolicy:   enums.PARENT_CLOSE_POLICY_ABANDON,
				WaitForCancellation: true,
			})

			child := workflow.ExecuteChildWorkflow(childCtx, DeviceIntegrationSyncWorkflow, candidate.SyncID.String())
			if err := child.GetChildWorkflowExecution().Get(ctx, nil); err != nil {
				if !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
					// One sync's start failure must not strand every other
					// due candidate in the pass.
					workflow.GetLogger(ctx).Warn("start device integration sync child failed", "sync_id", candidate.SyncID.String(), "error", err.Error())
					continue
				}
				continue
			}

			batch = append(batch, runningSync{
				candidate: candidate,
				child:     child,
			})
		}

		if len(batch) == 0 {
			break
		}

		selector := workflow.NewSelector(ctx)
		remaining := len(batch)
		for _, run := range batch {
			selector.AddFuture(run.child, func(f workflow.Future) {
				remaining--
			})
		}

		for remaining > 0 {
			selector.Select(ctx)
		}
	}

	return nil
}

func DeviceIntegrationSyncWorkflow(ctx workflow.Context, input string) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    deviceIntegrationSyncActivityTimeout,
		ScheduleToCloseTimeout: deviceIntegrationSyncActivityScheduleToCloseTimeout,
		// Heartbeating lets the server detect a dead attempt quickly and,
		// crucially, cancels the stale goroutine's context on timeout — a
		// timed-out attempt must not keep upserting devices alongside its
		// replacement.
		HeartbeatTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    deviceIntegrationSyncActivityMaxAttempts,
			InitialInterval:    deviceIntegrationSyncActivityRetryInitialInterval,
			BackoffCoefficient: 2,
			MaximumInterval:    deviceIntegrationSyncActivityRetryMaximumInterval,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.RunDeviceIntegrationSync, input).Get(ctx, nil); err != nil {
		return fmt.Errorf("run device integration sync: %w", err)
	}

	return nil
}

func buildDeviceIntegrationSyncWorkflowID(organizationSlug string, syncID uuid.UUID) string {
	return fmt.Sprintf("v1:device-integration-sync:%s:%s", organizationSlug, syncID.String())
}

func AddDeviceIntegrationSyncCoordinatorSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := buildDeviceIntegrationSyncScheduleOptions(temporalEnv)

	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create device integration sync schedule: %w", err)
	}

	if err := scheduleClient.GetHandle(ctx, deviceIntegrationSyncCoordinatorScheduleID).Update(ctx, client.ScheduleUpdateOptions{
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
		return fmt.Errorf("update device integration sync schedule: %w", err)
	}
	return nil
}

func buildDeviceIntegrationSyncScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	return client.ScheduleOptions{
		ID:      deviceIntegrationSyncCoordinatorScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: deviceIntegrationSyncCoordinatorInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 deviceIntegrationSyncCoordinatorWorkflowID,
			Workflow:           DeviceIntegrationSyncCoordinatorWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: deviceIntegrationSyncCoordinatorRunTimeout,
		},
	}
}

// DeviceIntegrationSyncTrigger runs the sync coordinator schedule
// immediately, so user actions that make a sync due (enabling a connection,
// fixing credentials, "Sync now") take effect in seconds instead of at the
// next tick. BUFFER_ONE queues the trigger behind an in-flight coordinator
// run: a run that already selected its candidates cannot see work made due
// after selection, so a skipped (rather than buffered) trigger would
// silently fall back to full tick latency.
type DeviceIntegrationSyncTrigger struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

var _ deviceintegrations.SyncTrigger = (*DeviceIntegrationSyncTrigger)(nil)

func (t *DeviceIntegrationSyncTrigger) TriggerSyncNow(ctx context.Context) error {
	// Guard the receiver: a typed-nil trigger handed through the interface
	// passes the caller's interface-nil check, and kickSync runs on a
	// goroutine where a panic takes down the process, not a request.
	if t == nil || t.TemporalEnv == nil {
		return fmt.Errorf("device integration sync trigger is not configured")
	}
	handle := t.TemporalEnv.Client().ScheduleClient().GetHandle(ctx, deviceIntegrationSyncCoordinatorScheduleID)
	if err := handle.Trigger(ctx, client.ScheduleTriggerOptions{
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE,
	}); err != nil {
		return fmt.Errorf("trigger device integration sync coordinator: %w", err)
	}
	if t.Logger != nil {
		t.Logger.DebugContext(ctx, "triggered device integration sync coordinator",
			attr.SlogTemporalWorkflowID(deviceIntegrationSyncCoordinatorScheduleID))
	}
	return nil
}
