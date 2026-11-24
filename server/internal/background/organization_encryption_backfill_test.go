package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// fakeOrganizationEncryptionMigrator records calls in order and fails the
// organizations named in errs. Every candidate in due is due, except those in
// notDue, and its due check fails for those in dueErrs.
type fakeOrganizationEncryptionMigrator struct {
	mu        sync.Mutex
	calls     []string
	due       []orgencryption.Transition
	notDue    map[string]bool
	dueErrs   map[string]error
	finalized []orgencryption.Transition
	errs      map[string]error
}

func (f *fakeOrganizationEncryptionMigrator) record(call, organizationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, call+" "+organizationID)

	return f.errs[organizationID]
}

func (f *fakeOrganizationEncryptionMigrator) NextBackfillCandidates(context.Context, orgencryption.BackfillCursor) (orgencryption.BackfillPage, error) {
	return orgencryption.BackfillPage{Candidates: f.due, More: false}, nil
}

func (f *fakeOrganizationEncryptionMigrator) BackfillDue(_ context.Context, transition orgencryption.Transition) (bool, error) {
	if err := f.dueErrs[transition.OrganizationID]; err != nil {
		return false, err
	}

	return !f.notDue[transition.OrganizationID], nil
}

func (f *fakeOrganizationEncryptionMigrator) Backfill(_ context.Context, organizationID string, _ orgencryption.UseCase) (orgencryption.RunResult, error) {
	return orgencryption.RunResult{}, f.record("backfill", organizationID)
}

func (f *fakeOrganizationEncryptionMigrator) ListFinalized(context.Context) ([]orgencryption.Transition, error) {
	return f.finalized, nil
}

func (f *fakeOrganizationEncryptionMigrator) ResumeClear(_ context.Context, organizationID string, _ orgencryption.UseCase) error {
	return f.record("resume", organizationID)
}

func (f *fakeOrganizationEncryptionMigrator) SweepStragglers(_ context.Context, organizationID string, _ orgencryption.UseCase) (int64, error) {
	if err := f.record("sweep", organizationID); err != nil {
		return 0, err
	}
	return 1, nil
}

func dualWrite(organizationID string) orgencryption.Transition {
	return orgencryption.Transition{OrganizationID: organizationID, UseCase: orgencryption.UseCaseOAuthSessionTokens, State: orgencryption.TransitionStateDualWrite}
}

func stalledClear(organizationID string) orgencryption.Transition {
	return orgencryption.Transition{OrganizationID: organizationID, UseCase: orgencryption.UseCaseOAuthSessionTokens, State: orgencryption.TransitionStateOrgKMS}
}

func clearedOrgKMS(organizationID string) orgencryption.Transition {
	return orgencryption.Transition{OrganizationID: organizationID, UseCase: orgencryption.UseCaseOAuthSessionTokens, State: orgencryption.TransitionStateOrgKMS, PlatformCopiesClearedAt: new(time.Now())}
}

func newBackfillActivities(t *testing.T, migrator OrganizationEncryptionMigrator) *OrganizationEncryptionBackfillActivities {
	t.Helper()

	activities := NewOrganizationEncryptionBackfillActivities(testenv.NewLogger(t), migrator)
	activities.heartbeat = func(context.Context, ...any) {}

	return activities
}

func TestOrganizationEncryptionBackfill_WithoutMigratorDoesNothing(t *testing.T) {
	t.Parallel()

	activities := newBackfillActivities(t, nil)

	result, err := activities.Backfill(t.Context())
	require.NoError(t, err)
	require.Equal(t, OrganizationEncryptionBackfillResult{Backfilled: 0, Skipped: 0, Failed: 0, ClearsResumed: 0, Swept: 0, StragglersCleared: 0}, result)

	_, err = activities.BackfillOrganization(t.Context(), OrganizationEncryptionBackfillOrganizationParams{OrganizationID: "org_a", UseCase: string(orgencryption.UseCaseOAuthClientSecrets)})
	requireApplicationError(t, err, OrganizationEncryptionBackfillRejectedErrorType)
}

func TestOrganizationEncryptionBackfill_SweepsFinalizedOrganizationsBeforeBackfilling(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		due:       []orgencryption.Transition{dualWrite("org_a"), dualWrite("org_b")},
		finalized: []orgencryption.Transition{stalledClear("org_stalled"), clearedOrgKMS("org_cleared")},
	}

	result, err := newBackfillActivities(t, migrator).Backfill(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"resume org_stalled", "sweep org_cleared", "backfill org_a", "backfill org_b"}, migrator.calls)
	require.Equal(t, OrganizationEncryptionBackfillResult{Backfilled: 2, Skipped: 0, Failed: 0, ClearsResumed: 1, Swept: 1, StragglersCleared: 1}, result)
}

func TestOrganizationEncryptionBackfill_AProblemInOneOrganizationDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		due:       []orgencryption.Transition{dualWrite("org_a"), dualWrite("org_b")},
		finalized: []orgencryption.Transition{clearedOrgKMS("org_cleared")},
		errs:      map[string]error{"org_a": orgencryption.ErrKeyUnavailable, "org_cleared": errors.New("database unavailable")},
	}

	result, err := newBackfillActivities(t, migrator).Backfill(t.Context())
	require.NoError(t, err, "a run that made progress is not retried")
	require.Equal(t, []string{"sweep org_cleared", "backfill org_a", "backfill org_b"}, migrator.calls)
	require.Equal(t, 1, result.Backfilled)
	require.Equal(t, 2, result.Failed)
}

func TestOrganizationEncryptionBackfill_FailsWhenNothingSucceeded(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		due:  []orgencryption.Transition{dualWrite("org_a")},
		errs: map[string]error{"org_a": orgencryption.ErrKeyUnavailable},
	}

	_, err := newBackfillActivities(t, migrator).Backfill(t.Context())
	require.ErrorIs(t, err, orgencryption.ErrKeyUnavailable)
}

func TestOrganizationEncryptionBackfill_SkipsOrganizationsAnotherRunHolds(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		due: []orgencryption.Transition{dualWrite("org_a"), dualWrite("org_b")},
		errs: map[string]error{
			"org_a": orgencryption.ErrBackfillRunning,
			"org_b": fmt.Errorf("%w: run in org_kms", orgencryption.ErrTransitionNotAllowed),
		},
	}

	result, err := newBackfillActivities(t, migrator).Backfill(t.Context())
	require.NoError(t, err, "skipping is not a failure")
	require.Equal(t, 2, result.Skipped)
	require.Zero(t, result.Failed)
}

func TestOrganizationEncryptionBackfill_ResumesAtMostTwoStalledClears(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		finalized: []orgencryption.Transition{stalledClear("org_1"), stalledClear("org_2"), stalledClear("org_3"), clearedOrgKMS("org_cleared")},
	}

	result, err := newBackfillActivities(t, migrator).Backfill(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"resume org_1", "resume org_2", "sweep org_cleared"}, migrator.calls)
	require.Equal(t, 2, result.ClearsResumed)
}

func TestOrganizationEncryptionBackfill_StopsStartingWorkAtTheBudget(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		due: []orgencryption.Transition{dualWrite("org_a"), dualWrite("org_b"), dualWrite("org_c")},
	}
	activities := newBackfillActivities(t, migrator)

	start := time.Now()
	calls := 0
	// The deadline, the page and the first candidate read the clock before
	// it passes the budget.
	activities.now = func() time.Time {
		calls++
		if calls <= 3 {
			return start
		}
		return start.Add(organizationEncryptionBackfillBudget)
	}

	result, err := activities.Backfill(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"backfill org_a"}, migrator.calls, "the rest wait for the next run")
	require.Equal(t, 1, result.Backfilled)
}

func TestOrganizationEncryptionBackfill_OnDemandReportsWhyItCannotRun(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		errs: map[string]error{
			"org_running":  orgencryption.ErrBackfillRunning,
			"org_finished": fmt.Errorf("%w: run in org_kms", orgencryption.ErrTransitionNotAllowed),
			"org_down":     orgencryption.ErrKeyUnavailable,
		},
	}
	activities := newBackfillActivities(t, migrator)
	params := func(organizationID string) OrganizationEncryptionBackfillOrganizationParams {
		return OrganizationEncryptionBackfillOrganizationParams{OrganizationID: organizationID, UseCase: string(orgencryption.UseCaseOAuthSessionTokens)}
	}

	_, err := activities.BackfillOrganization(t.Context(), params("org_running"))
	requireApplicationError(t, err, OrganizationEncryptionBackfillRunningErrorType)

	_, err = activities.BackfillOrganization(t.Context(), params("org_finished"))
	requireApplicationError(t, err, OrganizationEncryptionBackfillRejectedErrorType)

	_, err = activities.BackfillOrganization(t.Context(), OrganizationEncryptionBackfillOrganizationParams{OrganizationID: "org_a", UseCase: "not-a-use-case"})
	requireApplicationError(t, err, OrganizationEncryptionBackfillRejectedErrorType)

	_, err = activities.BackfillOrganization(t.Context(), params("org_down"))
	require.ErrorIs(t, err, orgencryption.ErrKeyUnavailable, "a key outage is retried")
	var applicationErr *temporal.ApplicationError
	require.NotErrorAs(t, err, &applicationErr)

	_, err = activities.BackfillOrganization(t.Context(), params("org_ok"))
	require.NoError(t, err)
}

func TestOrganizationEncryptionBackfillOrganizationWorkflow_DoesNotRetryARunAnotherRunHolds(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	activities := newBackfillActivities(t, &fakeOrganizationEncryptionMigrator{errs: map[string]error{"org_a": orgencryption.ErrBackfillRunning}})
	calls := 0
	env.RegisterActivityWithOptions(func(ctx context.Context, params OrganizationEncryptionBackfillOrganizationParams) (orgencryption.RunResult, error) {
		calls++
		return activities.BackfillOrganization(ctx, params)
	}, activity.RegisterOptions{Name: OrganizationEncryptionBackfillOrganizationActivityName})

	env.ExecuteWorkflow(OrganizationEncryptionBackfillOrganizationWorkflow, OrganizationEncryptionBackfillOrganizationParams{OrganizationID: "org_a", UseCase: string(orgencryption.UseCaseOAuthSessionTokens)})
	require.True(t, env.IsWorkflowCompleted())
	requireApplicationError(t, env.GetWorkflowError(), OrganizationEncryptionBackfillRunningErrorType)
	require.Equal(t, 1, calls)
}

func TestOrganizationEncryptionBackfillWorkflow_RunsTheActivityOnce(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(func(_ context.Context) (OrganizationEncryptionBackfillResult, error) {
		calls++
		return OrganizationEncryptionBackfillResult{Backfilled: 1, Skipped: 0, Failed: 0, ClearsResumed: 0, Swept: 0, StragglersCleared: 0}, nil
	}, activity.RegisterOptions{Name: OrganizationEncryptionBackfillActivityName})

	env.ExecuteWorkflow(OrganizationEncryptionBackfillWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, calls)
}

func TestOrganizationEncryptionBackfill_TimingsFitTheActivity(t *testing.T) {
	t.Parallel()

	require.Greater(t, organizationEncryptionBackfillItemTimeout, orgencryption.MaxRunDuration, "an item fits a whole Runner run")
	require.Greater(t, organizationEncryptionBackfillHeartbeatTimeout, organizationEncryptionBackfillItemTimeout, "an item never misses a heartbeat")
	require.Equal(t, organizationEncryptionBackfillStartToClose, organizationEncryptionBackfillBudget+organizationEncryptionBackfillItemTimeout+time.Minute, "the last item started inside the budget ends inside StartToClose")
	require.Less(t, organizationEncryptionBackfillStartToClose*organizationEncryptionBackfillMaxAttempts, organizationEncryptionBackfillRunTimeout, "every attempt fits the run")
}

func TestOrganizationEncryptionBackfill_RetriesStartAfterADeadWorkersClaimExpires(t *testing.T) {
	t.Parallel()

	// The hourly activity heartbeats right before each run, which claims
	// within ClaimTimeout. Temporal notices a dead worker a heartbeat timeout
	// after that heartbeat at the earliest, and retries after the retry
	// interval.
	require.GreaterOrEqual(t, organizationEncryptionBackfillHeartbeatTimeout+organizationEncryptionBackfillRetryInterval, orgencryption.ClaimTimeout+orgencryption.RunLease)

	// An on-demand attempt starts its run at once, and the run claims within
	// ClaimTimeout. runStartSlack covers the activity's setup before the run.
	// The retry starts StartToClose plus the retry interval after the attempt
	// started.
	const runStartSlack = 30 * time.Second
	require.GreaterOrEqual(t, organizationEncryptionBackfillOrganizationStartToClose+organizationEncryptionBackfillOrganizationRetryInterval, runStartSlack+orgencryption.ClaimTimeout+orgencryption.RunLease)
	require.Less(t, organizationEncryptionBackfillOrganizationStartToClose*organizationEncryptionBackfillOrganizationMaxAttempts+organizationEncryptionBackfillOrganizationRetryInterval*3, organizationEncryptionBackfillOrganizationRunTimeout, "every attempt and its backoff fits the run")
}

func requireApplicationError(t *testing.T, err error, errorType string) {
	t.Helper()

	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.Equal(t, errorType, applicationErr.Type())
	require.True(t, applicationErr.NonRetryable())
}

func TestOrganizationEncryptionBackfill_SkipsASweepWhoseStateMoved(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		finalized: []orgencryption.Transition{clearedOrgKMS("org_cleared")},
		errs:      map[string]error{"org_cleared": fmt.Errorf("%w: sweep platform-key copies in dual_write", orgencryption.ErrTransitionNotAllowed)},
	}

	result, err := newBackfillActivities(t, migrator).Backfill(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, result.Skipped)
	require.Zero(t, result.Failed)
}

func TestOrganizationEncryptionBackfill_StopsWhenCancelled(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		due:       []orgencryption.Transition{dualWrite("org_a")},
		finalized: []orgencryption.Transition{clearedOrgKMS("org_cleared")},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := newBackfillActivities(t, migrator).Backfill(ctx)
	require.NoError(t, err)
	require.Empty(t, migrator.calls, "a cancelled activity starts no more work")
}

func TestOrganizationEncryptionBackfillOrganizationWorkflowID_IsScopedByQueue(t *testing.T) {
	t.Parallel()

	require.NotEqual(t,
		OrganizationEncryptionBackfillOrganizationWorkflowID("main", "org_a", orgencryption.UseCaseOAuthSessionTokens),
		OrganizationEncryptionBackfillOrganizationWorkflowID("preview-123", "org_a", orgencryption.UseCaseOAuthSessionTokens),
	)
}

func TestOrganizationEncryptionBackfill_ChecksEachCandidateBeforeBackfillingIt(t *testing.T) {
	t.Parallel()

	migrator := &fakeOrganizationEncryptionMigrator{
		due:     []orgencryption.Transition{dualWrite("org_failing"), dualWrite("org_idle"), dualWrite("org_due")},
		notDue:  map[string]bool{"org_idle": true},
		dueErrs: map[string]error{"org_failing": errors.New("count failed")},
	}

	result, err := newBackfillActivities(t, migrator).Backfill(t.Context())
	require.NoError(t, err, "a run that backfilled one organization is not retried")
	require.Equal(t, []string{"backfill org_due"}, migrator.calls, "only due candidates are backfilled, and a failed check moves on")
	require.Equal(t, 1, result.Backfilled)
	require.Equal(t, 1, result.Failed)
}
