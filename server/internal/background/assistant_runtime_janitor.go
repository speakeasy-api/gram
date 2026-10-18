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

const (
	assistantRuntimeJanitorWorkflowID = "v1:assistant-runtime-janitor"
	assistantRuntimeJanitorScheduleID = "v1:assistant-runtime-janitor-schedule"

	// AssistantRuntimeJanitorInterval is how often the janitor sweeps. The
	// task is bounded per run by AssistantRuntimeJanitorBatchSize, so a
	// hourly cadence with a 200-row batch keeps each sweep short while
	// covering up to 4800 rows/day on the steady state.
	AssistantRuntimeJanitorInterval = time.Hour

	// Allow lateness up to one interval minus 1s; skip older missed ticks.
	assistantRuntimeJanitorCatchupWindow = AssistantRuntimeJanitorInterval - time.Second

	// AssistantRuntimeJanitorBatchSize caps rows reaped per sweep. Each row
	// makes backend API calls. GKE bounds individual delete calls; local
	// Docker cleanup relies on activity-context cancellation.
	AssistantRuntimeJanitorBatchSize int32 = 200

	// assistantRuntimeJanitorActivityTimeout is the upper bound on a single
	// sweep. The heartbeat timeout detects a stalled sweep sooner; local
	// cleanup commands stop when the activity context is canceled.
	assistantRuntimeJanitorActivityTimeout = 6 * time.Hour

	// assistantRuntimeJanitorHeartbeatTimeout allows normal per-row GKE
	// cleanup calls to finish before detecting a stalled sweep. It is not a
	// per-call deadline for local Docker cleanup.
	assistantRuntimeJanitorHeartbeatTimeout = 3 * time.Minute

	// AssistantRuntimeJanitorInactivityThreshold is the quiet period an
	// assistant must have before its backend resources become eligible
	// for collection. Long enough that a project's normal cold-warm-cold
	// rhythm keeps it out of the candidate set; short enough that
	// abandoned tenants don't retain runtime resources for weeks.
	AssistantRuntimeJanitorInactivityThreshold = 7 * 24 * time.Hour

	// AssistantRuntimeJanitorStoppedTTL is the age (against the row's own
	// updated_at) at which a stopped or failed runtime becomes eligible for
	// per-thread collection, regardless of sibling activity on the assistant.
	// Daily/weekly triggers re-Stop the row well inside this window, so only
	// truly dormant threads age out; the assistant app survives so the next
	// admit cold-launches with the same IP and secrets.
	AssistantRuntimeJanitorStoppedTTL = 14 * 24 * time.Hour
)

// AssistantRuntimeJanitorWorkflowParams lets operators override the bake-in
// defaults per scheduled run, e.g. shorten the threshold during incident
// remediation. Empty fields fall back to the package constants.
type AssistantRuntimeJanitorWorkflowParams struct {
	InactivityThreshold time.Duration
	StoppedTTL          time.Duration
	BatchSize           int32
}

type AssistantRuntimeJanitorWorkflowResult struct {
	Reaped int
	Errors int
}

// AssistantRuntimeJanitorWorkflow reaps orphaned backend resources (sandbox
// claims, long-lived runner state) whose Stop/Reap never completed — finalized
// runtime rows, plus live rows under soft-deleted assistants — once the
// owning assistant has had no runtime activity for
// AssistantRuntimeJanitorInactivityThreshold. A live runtime row under a
// live assistant is never a candidate: an idle runtime keeps its VM until
// the assistant is deleted.
func AssistantRuntimeJanitorWorkflow(ctx workflow.Context, params AssistantRuntimeJanitorWorkflowParams) (*AssistantRuntimeJanitorWorkflowResult, error) {
	var a *Activities

	threshold := params.InactivityThreshold
	if threshold <= 0 {
		threshold = AssistantRuntimeJanitorInactivityThreshold
	}
	stoppedTTL := params.StoppedTTL
	if stoppedTTL <= 0 {
		stoppedTTL = AssistantRuntimeJanitorStoppedTTL
	}
	batchSize := params.BatchSize
	if batchSize <= 0 {
		batchSize = AssistantRuntimeJanitorBatchSize
	}

	logger := workflow.GetLogger(ctx)

	// HeartbeatTimeout is the liveness signal; the reap loop beats after
	// each row. StartToCloseTimeout is just a ceiling for the worst-case
	// sweep wall time. MaximumAttempts is 2 so a transient DB error on the
	// initial candidate list does not waste the hourly slot; per-row backend
	// failures are already swallowed inside the activity and do not retry.
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: assistantRuntimeJanitorActivityTimeout,
		HeartbeatTimeout:    assistantRuntimeJanitorHeartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    2,
			InitialInterval:    5 * time.Second,
			MaximumInterval:    1 * time.Minute,
			BackoffCoefficient: 2,
		},
	})

	var inactiveResult activities.ReapInactiveAssistantRuntimesResult
	if err := workflow.ExecuteActivity(ctx, a.ReapInactiveAssistantRuntimes, activities.ReapInactiveAssistantRuntimesRequest{
		InactivityThreshold: threshold,
		BatchSize:           batchSize,
	}).Get(ctx, &inactiveResult); err != nil {
		return nil, fmt.Errorf("reap inactive assistant runtimes: %w", err)
	}

	var stoppedResult activities.ReapStoppedAssistantRuntimesResult
	if err := workflow.ExecuteActivity(ctx, a.ReapStoppedAssistantRuntimes, activities.ReapStoppedAssistantRuntimesRequest{
		StoppedTTL: stoppedTTL,
		BatchSize:  batchSize,
	}).Get(ctx, &stoppedResult); err != nil {
		return nil, fmt.Errorf("reap stopped assistant runtimes: %w", err)
	}

	reaped := inactiveResult.Reaped + stoppedResult.Reaped
	errs := inactiveResult.Errors + stoppedResult.Errors
	logger.Info("assistant runtime janitor completed",
		"reaped", reaped,
		"errors", errs,
		"inactive_reaped", inactiveResult.Reaped,
		"stopped_reaped", stoppedResult.Reaped,
	)

	return &AssistantRuntimeJanitorWorkflowResult{
		Reaped: reaped,
		Errors: errs,
	}, nil
}

func AddAssistantRuntimeJanitorSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := createScheduleWithCatchup(ctx, temporalEnv.Client().ScheduleClient(), client.ScheduleOptions{
		CatchupWindow: assistantRuntimeJanitorCatchupWindow,
		ID:            assistantRuntimeJanitorScheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: AssistantRuntimeJanitorInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 assistantRuntimeJanitorWorkflowID,
			Workflow:           AssistantRuntimeJanitorWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: assistantRuntimeJanitorActivityTimeout + 15*time.Minute,
			Args: []any{AssistantRuntimeJanitorWorkflowParams{
				InactivityThreshold: 0,
				StoppedTTL:          0,
				BatchSize:           0,
			}},
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create assistant runtime janitor schedule: %w", err)
	}
	return nil
}
