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

	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	mcpFindingEvidenceCleanupBatchSize            int32 = 500
	mcpFindingEvidenceCleanupMaxBatchesPerAttempt       = 100
	mcpFindingEvidenceCleanupActivityTimeout            = 10 * time.Minute
	mcpFindingEvidenceCleanupMaxAttempts                = 3
	mcpFindingEvidenceCleanupRetryInterval              = time.Minute
	mcpFindingEvidenceCleanupBackoffCoefficient         = 2
	mcpFindingEvidenceCleanupSchedulingMargin           = 7 * time.Minute
)

// MCPFindingEvidenceCleanupWorkflow removes evidence after its retention window.
// One global hourly sweep and one activity cost about 1,440 Temporal actions per
// month per namespace, fixed rather than scaling with tenants or findings. Three
// configured attempts cost at most about 2,880 actions per month. Each activity
// attempt deletes at most 50,000 rows per evidence table in bounded transactions.
func MCPFindingEvidenceCleanupWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: mcpFindingEvidenceCleanupActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    mcpFindingEvidenceCleanupMaxAttempts,
			InitialInterval:    mcpFindingEvidenceCleanupRetryInterval,
			BackoffCoefficient: mcpFindingEvidenceCleanupBackoffCoefficient,
		},
	})
	var a *Activities
	return workflow.ExecuteActivity(ctx, a.CleanupMCPFindingEvidence).Get(ctx, nil)
}

// CleanupMCPFindingEvidence removes expired finding matches and execution
// payloads, bounding each transaction and each table per activity attempt.
func (a *Activities) CleanupMCPFindingEvidence(ctx context.Context) error {
	queries := riskrepo.New(a.db)
	return cleanupMCPEvidence(ctx, queries.CleanupExpiredMCPFindingEvidenceBatch, queries.CleanupExpiredMCPExecutionEvidenceBatch)
}

func cleanupMCPEvidence(ctx context.Context, findings, executions func(context.Context, int32) (int64, error)) error {
	// A saturated finding table must not starve payload cleanup.
	return errors.Join(
		cleanupMCPFindingEvidenceBatches(ctx, "finding matches", findings),
		cleanupMCPFindingEvidenceBatches(ctx, "execution payloads", executions),
	)
}

func cleanupMCPFindingEvidenceBatches(ctx context.Context, table string, cleanup func(context.Context, int32) (int64, error)) error {
	for range mcpFindingEvidenceCleanupMaxBatchesPerAttempt {
		n, err := cleanup(ctx, mcpFindingEvidenceCleanupBatchSize)
		if err != nil {
			return fmt.Errorf("cleanup MCP %s batch: %w", table, err)
		}
		if n < int64(mcpFindingEvidenceCleanupBatchSize) {
			return nil
		}
	}
	return fmt.Errorf("MCP %s cleanup per-attempt batch budget exhausted", table)
}

// Include every attempt, intervening backoff, and queue/workflow-task headroom.
func mcpFindingEvidenceCleanupRunTimeout() time.Duration {
	budget := mcpFindingEvidenceCleanupMaxAttempts * mcpFindingEvidenceCleanupActivityTimeout
	interval := mcpFindingEvidenceCleanupRetryInterval
	for attempt := 1; attempt < mcpFindingEvidenceCleanupMaxAttempts; attempt++ {
		budget += interval
		interval *= mcpFindingEvidenceCleanupBackoffCoefficient
	}
	return budget + mcpFindingEvidenceCleanupSchedulingMargin
}

func AddMCPFindingEvidenceCleanupSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	id := fmt.Sprintf("v1:mcp-finding-evidence-cleanup:%s", temporalEnv.Queue())
	sc := temporalEnv.Client().ScheduleClient()
	options := client.ScheduleOptions{
		ID:            id,
		CatchupWindow: time.Hour - time.Second,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: time.Hour}}},
		Action: &client.ScheduleWorkflowAction{
			ID:                 id + "/scheduled",
			Workflow:           MCPFindingEvidenceCleanupWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: mcpFindingEvidenceCleanupRunTimeout(),
		},
	}
	_, err := sc.Create(ctx, options)
	if errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		err = sc.GetHandle(ctx, id).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				schedule := input.Description.Schedule
				action, ok := schedule.Action.(*client.ScheduleWorkflowAction)
				if !ok || action.TaskQueue != string(temporalEnv.Queue()) {
					return nil, fmt.Errorf("MCP finding evidence cleanup schedule is owned by another task queue")
				}
				if action.WorkflowRunTimeout == mcpFindingEvidenceCleanupRunTimeout() &&
					schedule.Policy != nil && schedule.Policy.CatchupWindow == options.CatchupWindow {
					return nil, temporal.ErrSkipScheduleUpdate
				}
				action.WorkflowRunTimeout = mcpFindingEvidenceCleanupRunTimeout()
				if schedule.Policy == nil {
					var policy client.SchedulePolicies
					schedule.Policy = &policy
				}
				schedule.Policy.CatchupWindow = options.CatchupWindow
				return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
			},
		})
		if err != nil {
			return fmt.Errorf("update MCP finding evidence cleanup schedule: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("create MCP finding evidence cleanup schedule: %w", err)
	}
	return nil
}
