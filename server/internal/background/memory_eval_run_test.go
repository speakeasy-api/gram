package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

// The workflow must outlive a maximally slow run plus the compensation that
// resolves the row, or a timeout strands the row in running.
func TestMemoryEvalRunWorkflow_RunTimeoutOutlivesItsActivities(t *testing.T) {
	t.Parallel()

	require.Greater(t, memoryEvalRunScheduleToCloseTimeout, memoryEvalRunActivityTimeout)

	// 5 attempts × 30s, plus backoff of 5s, 10s, 20s, 40s between them.
	policy := memoryEvalCompensationRetryPolicy()
	require.EqualValues(t, 5, policy.MaximumAttempts)
	require.GreaterOrEqual(t, memoryEvalCompensationScheduleToClose, 5*memoryEvalCompensationAttemptTimeout+75*time.Second)

	require.Greater(t, memoryEvalRunWorkflowRunTimeout, memoryEvalRunScheduleToCloseTimeout+memoryEvalCompensationScheduleToClose)
}

type fakeMemoryEvalRunner struct {
	mu          sync.Mutex
	runErr      error
	ran         []uuid.UUID
	interrupted []uuid.UUID
}

func (f *fakeMemoryEvalRunner) Run(_ context.Context, runID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ran = append(f.ran, runID)
	return f.runErr
}

func (f *fakeMemoryEvalRunner) MarkInterrupted(_ context.Context, runID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interrupted = append(f.interrupted, runID)
	return nil
}

func runMemoryEvalWorkflow(t *testing.T, runner MemoryEvalRunner, runID uuid.UUID) error {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	a := &Activities{memoryEvalRunner: runner}
	env.RegisterActivity(a.RunMemoryEval)
	env.RegisterActivity(a.MarkMemoryEvalInterrupted)
	env.ExecuteWorkflow(MemoryEvalRunWorkflow, MemoryEvalRunInput{RunID: runID})
	require.True(t, env.IsWorkflowCompleted())
	if err := env.GetWorkflowError(); err != nil {
		return fmt.Errorf("memory eval run workflow: %w", err)
	}
	return nil
}

func TestMemoryEvalRunWorkflow_SucceedsWithoutCompensation(t *testing.T) {
	t.Parallel()

	runner := &fakeMemoryEvalRunner{}
	runID := uuid.New()
	require.NoError(t, runMemoryEvalWorkflow(t, runner, runID))
	require.Equal(t, []uuid.UUID{runID}, runner.ran)
	require.Empty(t, runner.interrupted)
}

// A run activity that fails once is not retried (a run spends credits), and
// the row is resolved by the compensation.
func TestMemoryEvalRunWorkflow_FailureRunsOnceAndMarksInterrupted(t *testing.T) {
	t.Parallel()

	runner := &fakeMemoryEvalRunner{runErr: errors.New("row update failed")}
	runID := uuid.New()
	require.Error(t, runMemoryEvalWorkflow(t, runner, runID))
	require.Equal(t, []uuid.UUID{runID}, runner.ran)
	require.Equal(t, []uuid.UUID{runID}, runner.interrupted)
}

// A worker without a runner fails the run loudly instead of skipping it.
func TestMemoryEvalRunWorkflow_UnconfiguredWorkerFails(t *testing.T) {
	t.Parallel()

	require.Error(t, runMemoryEvalWorkflow(t, nil, uuid.New()))
}
