package background

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"testing"
	"time"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

func TestTrustedDelegationCleanupWorkflow(t *testing.T) {
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
			// Match other workflow tests: race instrumentation can delay the first yield.
			env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if fail {
					return errors.New("database unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: "CleanupTrustedDelegationCredentials"})
			env.ExecuteWorkflow(TrustedDelegationCleanupWorkflow)
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

func TestCleanupTrustedDelegationBatches(t *testing.T) {
	t.Parallel()
	t.Run("drains partial batch", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := cleanupTrustedDelegationBatches(t.Context(), func(_ context.Context, limit int32) (int64, error) {
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
		err := cleanupTrustedDelegationBatches(t.Context(), func(_ context.Context, limit int32) (int64, error) {
			calls++
			return int64(limit), nil
		})
		require.ErrorContains(t, err, "budget exhausted")
		require.Equal(t, 100, calls)
	})
	t.Run("database failure", func(t *testing.T) {
		t.Parallel()
		failure := errors.New("database unavailable")
		calls := 0
		err := cleanupTrustedDelegationBatches(t.Context(), func(context.Context, int32) (int64, error) {
			calls++
			return 0, failure
		})
		require.ErrorIs(t, err, failure)
		require.Equal(t, 1, calls)
	})
}

func TestTrustedDelegationCleanupRunTimeout(t *testing.T) {
	t.Parallel()
	// Three ten-minute attempts, one- and two-minute backoffs, seven minutes
	// of headroom for queueing and workflow tasks.
	require.Equal(t, 40*time.Minute, trustedDelegationCleanupRunTimeout())
}

// Exercise the actual workflow retry policy and batch loop together: saturation
// is retryable, and every activity attempt gets all 100 batches again.
func TestTrustedDelegationCleanupBudgetResetsPerAttempt(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
	var rowsPerAttempt []int64
	var attempts []int32
	env.RegisterActivityWithOptions(func(ctx context.Context) error {
		attempts = append(attempts, activity.GetInfo(ctx).Attempt)
		var rows int64
		err := cleanupTrustedDelegationBatches(ctx, func(_ context.Context, limit int32) (int64, error) {
			rows += int64(limit)
			return int64(limit), nil
		})
		rowsPerAttempt = append(rowsPerAttempt, rows)
		return err
	}, activity.RegisterOptions{Name: "CleanupTrustedDelegationCredentials"})
	env.ExecuteWorkflow(TrustedDelegationCleanupWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "per-attempt batch budget exhausted")
	require.Equal(t, []int32{1, 2, 3}, attempts)
	require.Equal(t, []int64{50_000, 50_000, 50_000}, rowsPerAttempt)
}

func TestTrustedDelegationCleanupSchedule(t *testing.T) {
	t.Parallel()
	failure := errors.New("unavailable")
	for _, tc := range []struct {
		name                 string
		createErr, updateErr error
	}{
		{name: "create"},
		{name: "existing", createErr: temporal.ErrScheduleAlreadyRunning},
		{name: "create failure", createErr: failure},
		{name: "update failure", createErr: temporal.ErrScheduleAlreadyRunning, updateErr: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &temporalmocks.Client{}
			sc := &temporalmocks.ScheduleClient{}
			handle := &temporalmocks.ScheduleHandle{}
			env := tenv.NewEnvironment(c, "test", "cleanup-test")
			id := "v1:trusted-delegation-cleanup:cleanup-test"
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
			})).Return(nil, tc.createErr).Once()
			if errors.Is(tc.createErr, temporal.ErrScheduleAlreadyRunning) {
				sc.On("GetHandle", mock.Anything, id).Return(handle).Once()
				handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
					state := &client.ScheduleState{Paused: true, Note: "operator pause"}
					spec := &client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: time.Hour}}}
					update, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
						Action: &client.ScheduleWorkflowAction{TaskQueue: "cleanup-test", WorkflowRunTimeout: 35 * time.Minute},
						Policy: &client.SchedulePolicies{CatchupWindow: 24 * time.Hour}, State: state, Spec: spec,
					}}})
					require.NoError(t, err)
					action, ok := update.Schedule.Action.(*client.ScheduleWorkflowAction)
					require.True(t, ok)
					require.Equal(t, 40*time.Minute, action.WorkflowRunTimeout)
					require.Equal(t, time.Hour-time.Second, update.Schedule.Policy.CatchupWindow)
					require.Equal(t, state, update.Schedule.State)
					require.Equal(t, spec, update.Schedule.Spec)
					// A subsequent startup must not write an already reconciled schedule.
					unchanged := *update.Schedule
					update, err = options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: unchanged}})
					require.Nil(t, update)
					require.ErrorIs(t, err, temporal.ErrSkipScheduleUpdate)
					require.Equal(t, state, unchanged.State)

					update, err = options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
						Action: &client.ScheduleWorkflowAction{TaskQueue: "other-queue"},
					}}})
					require.Nil(t, update)
					require.ErrorContains(t, err, "owned by another task queue")
					return true
				})).Return(tc.updateErr).Once()
			}
			err := AddTrustedDelegationCleanupSchedule(t.Context(), env)
			if errors.Is(tc.createErr, failure) || errors.Is(tc.updateErr, failure) {
				require.ErrorIs(t, err, failure)
			} else {
				require.NoError(t, err)
			}
			c.AssertExpectations(t)
			sc.AssertExpectations(t)
			handle.AssertExpectations(t)
		})
	}
}

func TestCleanupTrustedDelegationOrganizations(t *testing.T) {
	t.Parallel()
	organizations := []pgtype.Text{{}, {String: "org_first", Valid: true}, {String: "org_second", Valid: true}}
	var calls []repo.CleanupTrustedDelegationCredentialsBatchParams
	err := cleanupTrustedDelegationOrganizations(t.Context(), organizations, func(_ context.Context, p repo.CleanupTrustedDelegationCredentialsBatchParams) (int64, error) {
		calls = append(calls, p)
		switch len(calls) {
		case 1:
			return 2, nil
		case 2:
			return 498, nil
		default:
			return 0, nil
		}
	})
	require.NoError(t, err)
	require.Equal(t, []repo.CleanupTrustedDelegationCredentialsBatchParams{
		{OrganizationID: organizations[0], BatchSize: 500},
		{OrganizationID: organizations[1], BatchSize: 498},
		{OrganizationID: organizations[1], BatchSize: 500},
		{OrganizationID: organizations[2], BatchSize: 500},
	}, calls)
}
