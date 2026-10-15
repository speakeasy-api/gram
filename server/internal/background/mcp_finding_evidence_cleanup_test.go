package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

func TestMCPFindingEvidenceCleanupWorkflow(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if fail {
					return errors.New("database unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: "CleanupMCPFindingEvidence"})
			env.ExecuteWorkflow(MCPFindingEvidenceCleanupWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			if fail {
				require.Error(t, env.GetWorkflowError())
				require.Equal(t, 3, calls)
			} else {
				require.NoError(t, env.GetWorkflowError())
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestCleanupMCPFindingEvidenceBatches(t *testing.T) {
	t.Parallel()
	t.Run("drains partial batch", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := cleanupMCPFindingEvidenceBatches(t.Context(), "finding matches", func(_ context.Context, limit int32) (int64, error) {
			require.Equal(t, int32(500), limit)
			calls++
			if calls == 1 {
				return int64(limit), nil
			}
			return 1, nil
		})
		require.NoError(t, err)
		require.Equal(t, 2, calls)
	})
	t.Run("per-attempt saturation", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := cleanupMCPFindingEvidenceBatches(t.Context(), "finding matches", func(context.Context, int32) (int64, error) {
			calls++
			return 500, nil
		})
		require.ErrorContains(t, err, "budget exhausted")
		require.Equal(t, 100, calls)
	})
	t.Run("database failure", func(t *testing.T) {
		t.Parallel()
		failure := errors.New("database unavailable")
		err := cleanupMCPFindingEvidenceBatches(t.Context(), "finding matches", func(context.Context, int32) (int64, error) {
			return 0, failure
		})
		require.ErrorIs(t, err, failure)
	})
}

func TestCleanupMCPEvidenceDrainsBothTables(t *testing.T) {
	t.Parallel()
	var findings, executions int
	err := cleanupMCPEvidence(t.Context(),
		func(context.Context, int32) (int64, error) { findings++; return 0, nil },
		func(context.Context, int32) (int64, error) { executions++; return 0, nil },
	)
	require.NoError(t, err)
	require.Equal(t, 1, findings)
	require.Equal(t, 1, executions)
}

func TestCleanupMCPEvidenceFindingFailureDoesNotStarvePayloads(t *testing.T) {
	t.Parallel()
	failure := errors.New("database unavailable")
	executions := 0
	err := cleanupMCPEvidence(t.Context(),
		func(context.Context, int32) (int64, error) { return 0, failure },
		func(context.Context, int32) (int64, error) { executions++; return 0, nil },
	)
	require.ErrorIs(t, err, failure)
	require.Equal(t, 1, executions)
}

func TestCleanupMCPEvidenceFindingSaturationDoesNotStarvePayloads(t *testing.T) {
	t.Parallel()
	executions := 0
	err := cleanupMCPEvidence(t.Context(),
		func(_ context.Context, limit int32) (int64, error) { return int64(limit), nil },
		func(context.Context, int32) (int64, error) { executions++; return 0, nil },
	)
	require.ErrorContains(t, err, "finding matches")
	require.ErrorContains(t, err, "budget exhausted")
	require.Equal(t, 1, executions)
}

func TestCleanupMCPEvidenceReportsPayloadFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("database unavailable")
	err := cleanupMCPEvidence(t.Context(),
		func(context.Context, int32) (int64, error) { return 0, nil },
		func(context.Context, int32) (int64, error) { return 0, failure },
	)
	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, "execution payloads")
}

func TestMCPFindingEvidenceCleanupRunTimeout(t *testing.T) {
	t.Parallel()
	require.Equal(t, 40*time.Minute, mcpFindingEvidenceCleanupRunTimeout())
}

func TestMCPFindingEvidenceCleanupSchedule(t *testing.T) {
	t.Parallel()
	c := &temporalmocks.Client{}
	sc := &temporalmocks.ScheduleClient{}
	env := tenv.NewEnvironment(c, "test", "cleanup-test")
	id := "v1:mcp-finding-evidence-cleanup:cleanup-test"
	c.On("ScheduleClient").Return(sc).Once()
	sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
		require.Equal(t, id, options.ID)
		require.Equal(t, time.Hour-time.Second, options.CatchupWindow)
		require.Equal(t, time.Hour, options.Spec.Intervals[0].Every)
		action, ok := options.Action.(*client.ScheduleWorkflowAction)
		require.True(t, ok)
		require.Equal(t, 40*time.Minute, action.WorkflowRunTimeout)
		require.Equal(t, "cleanup-test", action.TaskQueue)
		return true
	})).Return(nil, nil).Once()

	require.NoError(t, AddMCPFindingEvidenceCleanupSchedule(t.Context(), env))
	c.AssertExpectations(t)
	sc.AssertExpectations(t)
}
