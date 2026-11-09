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

	"github.com/speakeasy-api/gram/server/internal/sessionhandoff"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	sessionHandoffCleanupMaxBatches         = 100
	sessionHandoffCleanupActivityTimeout    = 10 * time.Minute
	sessionHandoffCleanupMaxAttempts        = 3
	sessionHandoffCleanupRetryInterval      = time.Minute
	sessionHandoffCleanupBackoffCoefficient = 2
	sessionHandoffCleanupScheduleInterval   = time.Hour
	sessionHandoffCleanupWorkflowRunTimeout = 40 * time.Minute
	sessionHandoffCleanupScheduleID         = "v1:session-handoff-expired-unread-cleanup"
	sessionHandoffCleanupWorkflowID         = "v1:session-handoff-expired-unread-cleanup/scheduled"
)

// SessionHandoffCleanupWorkflow removes expired unread handoff blobs. One
// global hourly workflow and activity cost about 1,440 Temporal actions per
// month per namespace; retries raise the maximum to about 2,880 actions.
func SessionHandoffCleanupWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: sessionHandoffCleanupActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    sessionHandoffCleanupMaxAttempts,
			InitialInterval:    sessionHandoffCleanupRetryInterval,
			BackoffCoefficient: sessionHandoffCleanupBackoffCoefficient,
		},
	})
	var activities *Activities
	return workflow.ExecuteActivity(ctx, activities.CleanupSessionHandoffStorage).Get(ctx, nil)
}

func (a *Activities) CleanupSessionHandoffStorage(ctx context.Context) error {
	return errors.Join(
		a.CleanupExpiredUnreadSessionHandoffs(ctx),
		a.CleanupStaleSessionHandoffUploadIntents(ctx),
	)
}

func (a *Activities) CleanupExpiredUnreadSessionHandoffs(ctx context.Context) error {
	var cursor *sessionhandoff.ExpiredUnreadCursor
	var cleanupErrors []error
	for range sessionHandoffCleanupMaxBatches {
		next, more, err := a.sessionHandoffCleaner.CleanAfter(ctx, cursor)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("clean expired unread session handoffs: %w", err))
		}
		if !more {
			return errors.Join(cleanupErrors...)
		}
		cursor = next
	}
	cleanupErrors = append(cleanupErrors, fmt.Errorf("session handoff cleanup per-attempt batch budget exhausted"))
	return errors.Join(cleanupErrors...)
}

func (a *Activities) CleanupStaleSessionHandoffUploadIntents(ctx context.Context) error {
	staleBefore := time.Now().Add(-sessionhandoff.UploadIntentStaleAge)
	var cursor *sessionhandoff.UploadIntentCursor
	var cleanupErrors []error
	for range sessionHandoffCleanupMaxBatches {
		next, more, err := a.sessionHandoffIntentCleaner.CleanAfter(ctx, staleBefore, cursor)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("clean stale session handoff upload intents: %w", err))
		}
		if !more {
			return errors.Join(cleanupErrors...)
		}
		cursor = next
	}
	cleanupErrors = append(cleanupErrors, fmt.Errorf("session handoff upload intent cleanup batch budget exhausted"))
	return errors.Join(cleanupErrors...)
}

func AddSessionHandoffCleanupSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	if temporalEnv.Queue() != sharedTaskQueue {
		return nil
	}
	options := sessionHandoffCleanupScheduleOptions(string(temporalEnv.Queue()))
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, options)
	if errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create session handoff cleanup schedule: %w", err)
	}
	return nil
}

func sessionHandoffCleanupScheduleOptions(taskQueue string) client.ScheduleOptions {
	return client.ScheduleOptions{
		ID:            sessionHandoffCleanupScheduleID,
		CatchupWindow: sessionHandoffCleanupScheduleInterval - time.Second,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{
			Every: sessionHandoffCleanupScheduleInterval,
		}}},
		Action: &client.ScheduleWorkflowAction{
			ID:                 sessionHandoffCleanupWorkflowID,
			Workflow:           SessionHandoffCleanupWorkflow,
			TaskQueue:          taskQueue,
			WorkflowRunTimeout: sessionHandoffCleanupWorkflowRunTimeout,
		},
	}

}
