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
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// The Okta applications sync mirrors the device integration sync: a scheduled
// coordinator fans out one child per due connection, bounded per batch, with
// workflow-id dedupe so a running sync is never started twice. Payloads carry
// connection ids only.
const (
	oktaApplicationSyncCoordinatorWorkflowID = "v1:okta-application-sync-coordinator"

	// oktaApplicationSyncCoordinatorInterval bounds pickup latency; each
	// connection's own interval (default 6h) decides when it is due.
	oktaApplicationSyncCoordinatorInterval = 15 * time.Minute

	oktaApplicationSyncCoordinatorRunTimeout = 8 * time.Hour

	// oktaApplicationSyncChildConcurrency caps children started per batch;
	// the coordinator waits for a batch before selecting the next.
	oktaApplicationSyncChildConcurrency = 5

	// oktaApplicationSyncMaxBatchesPerPass bounds one coordinator run so a
	// backlog never outlives the run timeout; the next tick continues.
	oktaApplicationSyncMaxBatchesPerPass = 4

	oktaApplicationSyncCoordinatorActivityTimeout = 30 * time.Second

	oktaApplicationSyncActivityTimeout                = 30 * time.Minute
	oktaApplicationSyncActivityScheduleToCloseTimeout = 2 * time.Hour
	oktaApplicationSyncActivityMaxAttempts            = oktaapplications.MaxAttempts
	oktaApplicationSyncActivityRetryInitialInterval   = time.Minute
	oktaApplicationSyncActivityRetryMaximumInterval   = 15 * time.Minute
)

func oktaApplicationSyncCoordinatorScheduleID(queue string) string {
	return fmt.Sprintf("v1:okta-application-sync-coordinator:%s", queue)
}

func OktaApplicationSyncCoordinatorWorkflow(ctx workflow.Context) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: oktaApplicationSyncCoordinatorActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities
	attempted := map[uuid.UUID]bool{}
	var attemptedIDs []uuid.UUID

	for range oktaApplicationSyncMaxBatchesPerPass {
		var candidates []oktaapplications.SyncCandidate
		if err := workflow.ExecuteActivity(activityCtx, a.GetOktaApplicationSyncCandidates, activities.GetOktaApplicationSyncCandidatesInput{
			Limit:                oktaApplicationSyncChildConcurrency,
			ExcludeConnectionIDs: attemptedIDs,
		}).Get(activityCtx, &candidates); err != nil {
			return fmt.Errorf("get okta application sync candidates: %w", err)
		}
		fresh := candidates[:0]
		for _, c := range candidates {
			if !attempted[c.ConnectionID] {
				fresh = append(fresh, c)
			}
		}
		candidates = fresh
		if len(candidates) == 0 {
			return nil
		}

		batch := make([]workflow.ChildWorkflowFuture, 0, len(candidates))
		for _, candidate := range candidates {
			attempted[candidate.ConnectionID] = true
			attemptedIDs = append(attemptedIDs, candidate.ConnectionID)
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID: buildOktaApplicationSyncWorkflowID(candidate.ConnectionID),
				// A running child rejects a second start for the same connection.
				WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
				ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
				WaitForCancellation:   true,
			})
			child := workflow.ExecuteChildWorkflow(childCtx, OktaApplicationSyncWorkflow, candidate.ConnectionID.String())
			if err := child.GetChildWorkflowExecution().Get(ctx, nil); err != nil {
				if !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
					workflow.GetLogger(ctx).Warn("start okta application sync child failed", "connection_id", candidate.ConnectionID.String(), "error", err.Error())
				}
				continue
			}
			batch = append(batch, child)
		}
		if len(batch) == 0 {
			continue
		}

		selector := workflow.NewSelector(ctx)
		remaining := len(batch)
		for _, child := range batch {
			selector.AddFuture(child, func(workflow.Future) { remaining-- })
		}
		for remaining > 0 {
			selector.Select(ctx)
		}
	}
	return nil
}

func OktaApplicationSyncWorkflow(ctx workflow.Context, connectionID string) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    oktaApplicationSyncActivityTimeout,
		ScheduleToCloseTimeout: oktaApplicationSyncActivityScheduleToCloseTimeout,
		HeartbeatTimeout:       time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    oktaApplicationSyncActivityMaxAttempts,
			InitialInterval:    oktaApplicationSyncActivityRetryInitialInterval,
			BackoffCoefficient: 2,
			MaximumInterval:    oktaApplicationSyncActivityRetryMaximumInterval,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.RunOktaApplicationSync, connectionID).Get(ctx, nil); err != nil {
		// Activity timeout/worker death cannot reliably execute activity-local
		// cleanup. Cancellation also needs a fresh context for durable cleanup.
		finalCtx, cancel := workflow.NewDisconnectedContext(ctx)
		defer cancel()
		finalCtx = workflow.WithActivityOptions(finalCtx, workflow.ActivityOptions{
			StartToCloseTimeout:    time.Minute,
			ScheduleToCloseTimeout: 15 * time.Minute,
			RetryPolicy:            &temporal.RetryPolicy{InitialInterval: time.Second, MaximumInterval: time.Minute},
		})
		if finalErr := workflow.ExecuteActivity(finalCtx, a.FinalizeOktaApplicationSync, activities.FinalizeOktaApplicationSyncInput{
			ConnectionID: connectionID, Cutoff: workflow.Now(ctx),
		}).Get(finalCtx, nil); finalErr != nil {
			return fmt.Errorf("finalize okta application sync: %w", errors.Join(err, finalErr))
		}
		return fmt.Errorf("run okta application sync: %w", err)
	}
	return nil
}

func buildOktaApplicationSyncWorkflowID(connectionID uuid.UUID) string {
	return fmt.Sprintf("v1:okta-application-sync:%s", connectionID.String())
}

func AddOktaApplicationSyncCoordinatorSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := buildOktaApplicationSyncScheduleOptions(temporalEnv)

	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create okta application sync schedule: %w", err)
	}

	// Queue-scoped, so the update only touches this deployment's own copy
	// and cadence changes land without a manual delete.
	if err := scheduleClient.GetHandle(ctx, options.ID).Update(ctx, client.ScheduleUpdateOptions{
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
		return fmt.Errorf("update okta application sync schedule: %w", err)
	}
	return nil
}

func buildOktaApplicationSyncScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	queue := string(temporalEnv.Queue())
	return client.ScheduleOptions{
		ID:      oktaApplicationSyncCoordinatorScheduleID(queue),
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: oktaApplicationSyncCoordinatorInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 fmt.Sprintf("%s:%s", oktaApplicationSyncCoordinatorWorkflowID, queue),
			Workflow:           OktaApplicationSyncCoordinatorWorkflow,
			TaskQueue:          queue,
			WorkflowRunTimeout: oktaApplicationSyncCoordinatorRunTimeout,
		},
	}
}

// OktaApplicationSyncTrigger runs the coordinator promptly after a manual
// sync request. BUFFER_ONE queues behind an in-flight pass, which cannot see
// a connection made due after it selected candidates.
type OktaApplicationSyncTrigger struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

var _ identityproviderconnections.ApplicationSyncTrigger = (*OktaApplicationSyncTrigger)(nil)

func (t *OktaApplicationSyncTrigger) TriggerApplicationSync(ctx context.Context) error {
	if t == nil || t.TemporalEnv == nil {
		return fmt.Errorf("okta application sync trigger is not configured")
	}
	scheduleID := oktaApplicationSyncCoordinatorScheduleID(string(t.TemporalEnv.Queue()))
	handle := t.TemporalEnv.Client().ScheduleClient().GetHandle(ctx, scheduleID)
	if err := handle.Trigger(ctx, client.ScheduleTriggerOptions{
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE,
	}); err != nil {
		return fmt.Errorf("trigger okta application sync coordinator: %w", err)
	}
	if t.Logger != nil {
		t.Logger.DebugContext(ctx, "triggered okta application sync coordinator", attr.SlogTemporalWorkflowID(scheduleID))
	}
	return nil
}
