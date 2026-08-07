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

// Auto-refresh is a best-effort keepalive. One hourly workflow chain drains
// atomically claimed batches of sessions unused for 24 hours and refreshes them
// in small parallel batches. Failed sessions rotate out for 24 hours, and a
// provider returning 429 is skipped for the rest of the workflow run.
const (
	remoteSessionRefreshScheduleID = "v1:remote-session-refresh-schedule"
	remoteSessionRefreshWorkflowID = "v1:remote-session-refresh"

	remoteSessionRefreshInterval       = time.Hour
	remoteSessionRefreshScheduleJitter = 20 * time.Minute
	remoteSessionRefreshBatchSize      = 10
	remoteSessionRefreshRunBudget      = 45 * time.Minute

	remoteSessionRefreshClaimActivityTimeout = 30 * time.Second
	remoteSessionRefreshActivityTimeout      = 60 * time.Second
	// The workflow checks its 45-minute budget after every batch, so one
	// activity timeout still leaves ample room below this one-hour limit.
	remoteSessionRefreshWorkflowTimeout = time.Hour
)

func RemoteSessionRefreshWorkflow(ctx workflow.Context) error {
	claimCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: remoteSessionRefreshClaimActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})
	refreshCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: remoteSessionRefreshActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			// A timeout or worker crash can occur after the provider consumed a
			// rotating refresh token but before Gram persisted it. A later
			// keepalive attempt is the safe retry boundary.
			MaximumAttempts: 1,
		},
	})

	var a *Activities
	type pendingRefresh struct {
		candidate activities.RemoteSessionRefreshCandidate
		future    workflow.Future
	}

	startedAt := workflow.Now(ctx)
	rateLimitedProviders := map[string]bool{}

	for {
		var candidates []activities.RemoteSessionRefreshCandidate
		if err := workflow.ExecuteActivity(
			claimCtx,
			a.ClaimDueRemoteSessionRefreshCandidates,
			activities.ClaimDueRemoteSessionRefreshCandidatesInput{Limit: remoteSessionRefreshBatchSize},
		).Get(claimCtx, &candidates); err != nil {
			return fmt.Errorf("claim due remote session refresh candidates: %w", err)
		}
		if len(candidates) == 0 {
			return nil
		}

		pending := make([]pendingRefresh, 0, len(candidates))
		for _, candidate := range candidates {
			// Claiming already stamped this row, so skipping a rate-limited
			// provider gives it a durable 24-hour backoff.
			if candidate.ProviderKey != "" && rateLimitedProviders[candidate.ProviderKey] {
				continue
			}
			pending = append(pending, pendingRefresh{
				candidate: candidate,
				future: workflow.ExecuteActivity(
					refreshCtx,
					a.RefreshRemoteSession,
					activities.RefreshRemoteSessionInput{
						SessionID:      candidate.SessionID,
						OrganizationID: candidate.OrganizationID,
					},
				),
			})
		}

		for _, refresh := range pending {
			var result activities.RefreshRemoteSessionResult
			if err := refresh.future.Get(refreshCtx, &result); err != nil {
				continue
			}
			if result.RateLimited && refresh.candidate.ProviderKey != "" {
				rateLimitedProviders[refresh.candidate.ProviderKey] = true
			}
		}

		if workflow.Now(ctx).Sub(startedAt) >= remoteSessionRefreshRunBudget ||
			workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, RemoteSessionRefreshWorkflow)
		}
	}
}

func AddRemoteSessionRefreshSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := client.ScheduleOptions{
		ID:      remoteSessionRefreshScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: remoteSessionRefreshInterval}},
			Jitter:    remoteSessionRefreshScheduleJitter,
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        remoteSessionRefreshWorkflowID,
			Workflow:  RemoteSessionRefreshWorkflow,
			TaskQueue: string(temporalEnv.Queue()),
			// Do not set WorkflowExecutionTimeout: it spans Continue-As-New
			// runs and would eventually kill an active drainer chain.
			WorkflowRunTimeout: remoteSessionRefreshWorkflowTimeout,
		},
	}

	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create remote session refresh schedule: %w", err)
	}

	if err := scheduleClient.GetHandle(ctx, remoteSessionRefreshScheduleID).Update(ctx, client.ScheduleUpdateOptions{
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
			schedule.Policy.Overlap = enums.SCHEDULE_OVERLAP_POLICY_SKIP
			return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
		},
	}); err != nil {
		return fmt.Errorf("update remote session refresh schedule: %w", err)
	}
	return nil
}
