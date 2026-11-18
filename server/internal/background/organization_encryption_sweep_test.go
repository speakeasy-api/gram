package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stubOrganizationEncryptionSweeper struct {
	checked  int
	checkErr error
	disabled int
	sweepErr error
}

func (s stubOrganizationEncryptionSweeper) CheckDue(context.Context, time.Duration, int32, time.Duration) (int, error) {
	return s.checked, s.checkErr
}

func (s stubOrganizationEncryptionSweeper) SweepManagedKeys(context.Context, int32) (int, error) {
	return s.disabled, s.sweepErr
}

func TestOrganizationEncryptionSweep_WithoutSweeperDoesNothing(t *testing.T) {
	t.Parallel()

	result, err := NewOrganizationEncryptionSweepActivities(testenv.NewLogger(t), nil).Sweep(t.Context())
	require.NoError(t, err)
	require.Equal(t, OrganizationEncryptionSweepResult{Checked: 0, Disabled: 0}, result)
}

func TestOrganizationEncryptionSweep_ReportsBothHalves(t *testing.T) {
	t.Parallel()

	sweeper := stubOrganizationEncryptionSweeper{checked: 3, checkErr: nil, disabled: 1, sweepErr: nil}
	result, err := NewOrganizationEncryptionSweepActivities(testenv.NewLogger(t), sweeper).Sweep(t.Context())
	require.NoError(t, err)
	require.Equal(t, OrganizationEncryptionSweepResult{Checked: 3, Disabled: 1}, result)
}

func TestOrganizationEncryptionSweep_FailsOnlyWithoutProgress(t *testing.T) {
	t.Parallel()

	failure := errors.New("kms unreachable")

	_, err := NewOrganizationEncryptionSweepActivities(testenv.NewLogger(t), stubOrganizationEncryptionSweeper{checked: 0, checkErr: failure, disabled: 0, sweepErr: failure}).Sweep(t.Context())
	require.ErrorIs(t, err, failure, "a run with no progress fails, so Temporal retries it")

	result, err := NewOrganizationEncryptionSweepActivities(testenv.NewLogger(t), stubOrganizationEncryptionSweeper{checked: 2, checkErr: nil, disabled: 0, sweepErr: failure}).Sweep(t.Context())
	require.NoError(t, err, "a partial run is not retried")
	require.Equal(t, 2, result.Checked)
}

func TestOrganizationEncryptionSweepWorkflow_RunsTheActivityOnce(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(func(_ context.Context) (OrganizationEncryptionSweepResult, error) {
		calls++
		return OrganizationEncryptionSweepResult{Checked: 1, Disabled: 0}, nil
	}, activity.RegisterOptions{Name: OrganizationEncryptionSweepActivityName})

	env.ExecuteWorkflow(OrganizationEncryptionSweepWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, calls)

	var result OrganizationEncryptionSweepResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 1, result.Checked)
}

func TestOrganizationEncryptionSweepWorkflow_RetriesARunWithNoProgress(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	failure := errors.New("kms unreachable")
	activities := NewOrganizationEncryptionSweepActivities(testenv.NewLogger(t), stubOrganizationEncryptionSweeper{checked: 0, checkErr: failure, disabled: 0, sweepErr: failure})
	calls := 0
	env.RegisterActivityWithOptions(func(ctx context.Context) (OrganizationEncryptionSweepResult, error) {
		calls++
		return activities.Sweep(ctx)
	}, activity.RegisterOptions{Name: OrganizationEncryptionSweepActivityName})

	env.ExecuteWorkflow(OrganizationEncryptionSweepWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, 2, calls, "the activity is attempted twice before the run fails")
}
