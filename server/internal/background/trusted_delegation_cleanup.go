package background

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	trustedDelegationCleanupBatchSize            int32 = 500
	trustedDelegationCleanupMaxBatchesPerAttempt       = 100
	trustedDelegationCleanupActivityTimeout            = 10 * time.Minute
	trustedDelegationCleanupMaxAttempts                = 3
	trustedDelegationCleanupRetryInterval              = time.Minute
	trustedDelegationCleanupBackoffCoefficient         = 2
	trustedDelegationCleanupSchedulingMargin           = 7 * time.Minute
)

// TrustedDelegationCleanupWorkflow erases unusable credentials, never renews them.
// One global hourly sweep, one activity, no per-human schedules or request-path
// signals. Temporal actions/month per namespace: ~720 starts + 720 activities
// = 1,440 normally (2,880 with all three attempts), fixed rather than per user.
// Each preview task queue adds its own fixed schedule. The activity drains at
// most 50,000 rows per activity attempt. Three configured attempts can process
// up to 150,000 rows in one execution; manual reruns get fresh budgets.
// Monitor saturation to keep erasure within 24 hours.
func TrustedDelegationCleanupWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: trustedDelegationCleanupActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    trustedDelegationCleanupMaxAttempts,
			InitialInterval:    trustedDelegationCleanupRetryInterval,
			BackoffCoefficient: trustedDelegationCleanupBackoffCoefficient,
		},
	})
	var a *Activities
	return workflow.ExecuteActivity(ctx, a.CleanupTrustedDelegationCredentials).Get(ctx, nil)
}

// CleanupTrustedDelegationCredentials bounds transactions and work per attempt.
// Each retry starts a fresh batch budget; exhaustion remains retryable.
// SQL only selects rows still eligible for cleanup, making retries idempotent.
func (a *Activities) CleanupTrustedDelegationCredentials(ctx context.Context) error {
	q := repo.New(a.db)
	organizations, err := q.ListTrustedDelegationCleanupOrganizations(ctx)
	if err != nil {
		return fmt.Errorf("list delegation cleanup organizations: %w", err)
	}
	return cleanupTrustedDelegationOrganizations(ctx, organizations, q.CleanupTrustedDelegationCredentialsBatch)
}

func cleanupTrustedDelegationOrganizations(ctx context.Context, organizations []pgtype.Text, cleanup func(context.Context, repo.CleanupTrustedDelegationCredentialsBatchParams) (int64, error)) error {
	return cleanupTrustedDelegationBatches(ctx, func(ctx context.Context, limit int32) (int64, error) {
		var total int64
		for len(organizations) > 0 && total < int64(limit) {
			remaining := limit - int32(total)
			n, err := cleanup(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: organizations[0], BatchSize: remaining})
			if err != nil {
				return total, err
			}
			total += n
			if n < int64(remaining) {
				organizations = organizations[1:]
			}
		}
		return total, nil
	})
}

func cleanupTrustedDelegationBatches(ctx context.Context, cleanup func(context.Context, int32) (int64, error)) error {
	for range trustedDelegationCleanupMaxBatchesPerAttempt {
		n, err := cleanup(ctx, trustedDelegationCleanupBatchSize)
		if err != nil {
			return fmt.Errorf("cleanup trusted delegation credentials: %w", err)
		}
		if n < int64(trustedDelegationCleanupBatchSize) {
			return nil
		}
	}
	return fmt.Errorf("trusted delegation credential cleanup per-attempt batch budget exhausted")
}

// Include every attempt, intervening backoff, and queue/workflow-task headroom.
func trustedDelegationCleanupRunTimeout() time.Duration {
	budget := trustedDelegationCleanupMaxAttempts * trustedDelegationCleanupActivityTimeout
	interval := trustedDelegationCleanupRetryInterval
	for attempt := 1; attempt < trustedDelegationCleanupMaxAttempts; attempt++ {
		budget += interval
		interval *= trustedDelegationCleanupBackoffCoefficient
	}
	return budget + trustedDelegationCleanupSchedulingMargin
}

func AddTrustedDelegationCleanupSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	id := fmt.Sprintf("v1:trusted-delegation-cleanup:%s", temporalEnv.Queue())
	sc := temporalEnv.Client().ScheduleClient()
	options := client.ScheduleOptions{
		ID:            id,
		CatchupWindow: time.Hour - time.Second,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: time.Hour}}},
		Action: &client.ScheduleWorkflowAction{
			ID: id + "/scheduled", Workflow: TrustedDelegationCleanupWorkflow,
			TaskQueue: string(temporalEnv.Queue()), WorkflowRunTimeout: trustedDelegationCleanupRunTimeout(),
		},
	}
	_, err := sc.Create(ctx, options)
	if errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		err = sc.GetHandle(ctx, id).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				schedule := input.Description.Schedule
				action, ok := schedule.Action.(*client.ScheduleWorkflowAction)
				if !ok || action.TaskQueue != string(temporalEnv.Queue()) {
					return nil, fmt.Errorf("trusted delegation cleanup schedule is owned by another task queue")
				}
				if action.WorkflowRunTimeout == trustedDelegationCleanupRunTimeout() &&
					schedule.Policy != nil && schedule.Policy.CatchupWindow == options.CatchupWindow {
					return nil, temporal.ErrSkipScheduleUpdate
				}
				// Preserve operator pause state and other settings while migrating limits.
				action.WorkflowRunTimeout = trustedDelegationCleanupRunTimeout()
				if schedule.Policy == nil {
					var policy client.SchedulePolicies
					schedule.Policy = &policy
				}
				schedule.Policy.CatchupWindow = options.CatchupWindow
				return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
			},
		})
		if err != nil {
			return fmt.Errorf("update trusted delegation cleanup schedule: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("create trusted delegation cleanup schedule: %w", err)
	}
	return nil
}
