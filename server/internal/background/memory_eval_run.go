package background

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// MemoryEvalRunner executes stored team memory eval runs. It is satisfied by
// evalrun.Runner, which this package cannot import: the eval bench reaches
// the platform MCP, which reaches this package.
type MemoryEvalRunner interface {
	Run(ctx context.Context, runID uuid.UUID) error
	MarkInterrupted(ctx context.Context, runID uuid.UUID) error
}

// MemoryEvalRunInput names the memory_eval_runs row to execute. The row
// already exists in status queued; the workflow's job is to fill it in.
type MemoryEvalRunInput struct {
	RunID uuid.UUID
}

// The run timeout must outlive a full-length run plus the compensation that
// resolves the row, or a timeout kills the compensation too and leaves the
// row in running. Asserted in TestMemoryEvalRunWorkflow_RunTimeoutOutlivesItsActivities.
const (
	// One run: summaries and digests for the corpus, then every answer model
	// in every arm, each answer judged. Bounded by evalrun.MaxRuns and
	// evalrun.MaxAnswerModels.
	memoryEvalRunActivityTimeout = 3 * time.Hour

	// Queue time plus run time.
	memoryEvalRunScheduleToCloseTimeout = 3*time.Hour + 10*time.Minute

	memoryEvalCompensationAttemptTimeout   = 30 * time.Second
	memoryEvalCompensationScheduleToClose  = 4 * time.Minute
	memoryEvalRunWorkflowRunTimeout        = 3*time.Hour + 20*time.Minute
	memoryEvalRunActivityHeartbeatInterval = 30 * time.Second
)

func memoryEvalCompensationRetryPolicy() *temporal.RetryPolicy {
	return &temporal.RetryPolicy{
		MaximumAttempts:        5,
		InitialInterval:        5 * time.Second,
		BackoffCoefficient:     2,
		MaximumInterval:        1 * time.Minute,
		NonRetryableErrorTypes: nil,
	}
}

// ExecuteMemoryEvalRunWorkflow starts one stored eval run. The workflow id is
// keyed by run, and USE_EXISTING makes a duplicate start attach to the
// in-flight run instead of racing it.
func ExecuteMemoryEvalRunWorkflow(ctx context.Context, temporalEnv *tenv.Environment, input MemoryEvalRunInput) (client.WorkflowRun, error) {
	run, err := temporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       fmt.Sprintf("v1:memory-eval-run/%s", input.RunID),
		TaskQueue:                string(temporalEnv.Queue()),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowRunTimeout:       memoryEvalRunWorkflowRunTimeout,
	}, MemoryEvalRunWorkflow, input)
	if err != nil {
		return nil, fmt.Errorf("execute memory eval run workflow: %w", err)
	}
	return run, nil
}

func MemoryEvalRunWorkflow(ctx workflow.Context, input MemoryEvalRunInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    memoryEvalRunActivityTimeout,
		ScheduleToCloseTimeout: memoryEvalRunScheduleToCloseTimeout,
		HeartbeatTimeout:       2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			// One attempt: a run spends real model credits and records its
			// own failure on the row. A staff member re-runs deliberately.
			MaximumAttempts:    1,
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    1 * time.Minute,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.RunMemoryEval, input).Get(ctx, nil); err != nil {
		// A crashed or heartbeat-timed-out worker never records its failure,
		// so resolve the row here. Disconnected so cancellation or
		// termination still runs it; a no-op for rows already resolved.
		disconnected, cancelDisconnected := workflow.NewDisconnectedContext(ctx)
		defer cancelDisconnected()
		markCtx := workflow.WithActivityOptions(disconnected, workflow.ActivityOptions{
			StartToCloseTimeout:    memoryEvalCompensationAttemptTimeout,
			ScheduleToCloseTimeout: memoryEvalCompensationScheduleToClose,
			RetryPolicy:            memoryEvalCompensationRetryPolicy(),
		})
		if markErr := workflow.ExecuteActivity(markCtx, a.MarkMemoryEvalInterrupted, input).Get(markCtx, nil); markErr != nil {
			workflow.GetLogger(ctx).Error("mark interrupted memory eval run failed", "error", markErr)
		}
		return fmt.Errorf("run memory eval: %w", err)
	}
	return nil
}

// RunMemoryEval executes one stored eval run and lands the outcome on its row.
func (a *Activities) RunMemoryEval(ctx context.Context, input MemoryEvalRunInput) error {
	if a.memoryEvalRunner == nil {
		return fmt.Errorf("run memory eval: memory eval runner not configured on this worker")
	}
	stop := heartbeatEvery(ctx, memoryEvalRunActivityHeartbeatInterval)
	defer stop()
	if err := a.memoryEvalRunner.Run(ctx, input.RunID); err != nil {
		return fmt.Errorf("run memory eval: %w", err)
	}
	return nil
}

// MarkMemoryEvalInterrupted fails a run whose activity died before it could
// record the outcome.
func (a *Activities) MarkMemoryEvalInterrupted(ctx context.Context, input MemoryEvalRunInput) error {
	if a.memoryEvalRunner == nil {
		return fmt.Errorf("mark memory eval interrupted: memory eval runner not configured on this worker")
	}
	if err := a.memoryEvalRunner.MarkInterrupted(ctx, input.RunID); err != nil {
		return fmt.Errorf("mark memory eval interrupted: %w", err)
	}
	return nil
}

// heartbeatEvery records an activity heartbeat on an interval until the
// returned stop function runs or ctx ends.
func heartbeatEvery(ctx context.Context, interval time.Duration) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx)
			}
		}
	}()
	return func() { close(done) }
}

// TemporalMemoryEvalRunStarter starts MemoryEvalRunWorkflow for queued runs.
type TemporalMemoryEvalRunStarter struct {
	TemporalEnv *tenv.Environment
}

func (s *TemporalMemoryEvalRunStarter) StartMemoryEvalRun(ctx context.Context, runID uuid.UUID) error {
	if _, err := ExecuteMemoryEvalRunWorkflow(ctx, s.TemporalEnv, MemoryEvalRunInput{RunID: runID}); err != nil {
		return err
	}
	return nil
}
