package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/sessionhandoff"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	sessionHandoffCleanupMaxBatches               = 100
	expiredUnreadProjectBatchSize           int32 = 100
	sessionHandoffCleanupActivityTimeout          = 10 * time.Minute
	sessionHandoffCleanupMaxAttempts              = 3
	sessionHandoffCleanupRetryInterval            = time.Minute
	sessionHandoffCleanupBackoffCoefficient       = 2
	sessionHandoffCleanupScheduleInterval         = time.Hour
	sessionHandoffCleanupWorkflowRunTimeout       = 40 * time.Minute
	sessionHandoffCleanupScheduleID               = "v1:session-handoff-expired-unread-cleanup"
	sessionHandoffCleanupWorkflowID               = "v1:session-handoff-expired-unread-cleanup/scheduled"
)

// SessionHandoffCleanupWorkflow removes expired unread handoff blobs and stale
// upload intents. One hourly workflow plus two activities costs about 2,160
// Temporal actions per month per namespace; exhausting both retry budgets raises
// the maximum to about 5,040 actions.
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
	expired := workflow.ExecuteActivity(ctx, activities.CleanupExpiredUnreadSessionHandoffs)
	intents := workflow.ExecuteActivity(ctx, activities.CleanupStaleSessionHandoffUploadIntents)
	return errors.Join(expired.Get(ctx, nil), intents.Get(ctx, nil))
}

func (a *Activities) CleanupExpiredUnreadSessionHandoffs(ctx context.Context) error {
	var projectCursor *uuid.UUID
	var cleanupErrors []error
	remainingBatches := sessionHandoffCleanupMaxBatches
	for range sessionHandoffCleanupMaxBatches {
		projects, moreProjects, err := a.sessionHandoffCleaner.ListProjects(ctx, projectCursor, expiredUnreadProjectBatchSize)
		if err != nil {
			return errors.Join(append(cleanupErrors, err)...)
		}
		for _, projectID := range projects {
			var cursor *sessionhandoff.ExpiredUnreadCursor
			for {
				if remainingBatches == 0 {
					return errors.Join(append(cleanupErrors, fmt.Errorf("session handoff cleanup per-attempt batch budget exhausted"))...)
				}
				remainingBatches--
				next, more, cleanErr := a.sessionHandoffCleaner.CleanProjectAfter(ctx, projectID, cursor)
				if cleanErr != nil {
					cleanupErrors = append(cleanupErrors, fmt.Errorf("clean expired unread session handoffs for project: %w", cleanErr))
				}
				if !more {
					break
				}
				cursor = next
			}
		}
		if !moreProjects {
			return errors.Join(cleanupErrors...)
		}
		projectCursor = &projects[len(projects)-1]
	}
	return errors.Join(append(cleanupErrors, fmt.Errorf("session handoff project traversal batch budget exhausted"))...)
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
