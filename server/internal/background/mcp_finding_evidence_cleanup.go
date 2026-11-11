package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	// mcpFindingEvidenceCleanupBatchSize bounds the rows one transaction deletes.
	mcpFindingEvidenceCleanupBatchSize int32 = 500

	// mcpFindingEvidenceCleanupMaxBatchesPerAttempt bounds the batches per
	// table in one activity attempt.
	mcpFindingEvidenceCleanupMaxBatchesPerAttempt int = 100
)

// mcpFindingEvidenceCleanup is the hourly sweep's schedule and retry policy.
var mcpFindingEvidenceCleanup = hourlyCleanup{
	name:               "mcp-finding-evidence-cleanup",
	activityTimeout:    10 * time.Minute,
	maxAttempts:        3,
	retryInterval:      time.Minute,
	backoffCoefficient: 2,
	schedulingMargin:   7 * time.Minute,
}

// MCPFindingEvidenceCleanupWorkflow removes evidence after its retention window.
// One global hourly sweep and one activity cost about 1,440 Temporal actions per
// month per namespace, fixed rather than scaling with tenants or findings. Three
// configured attempts cost at most about 2,880 actions per month. Each activity
// attempt deletes at most 50,000 rows per evidence table in bounded transactions.
func MCPFindingEvidenceCleanupWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, mcpFindingEvidenceCleanup.activityOptions())
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

// AddMCPFindingEvidenceCleanupSchedule installs the hourly sweep for this task
// queue.
func AddMCPFindingEvidenceCleanupSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	return mcpFindingEvidenceCleanup.ensureSchedule(ctx, temporalEnv, MCPFindingEvidenceCleanupWorkflow)
}
