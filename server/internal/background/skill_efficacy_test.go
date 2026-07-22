package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
)

// exhaustedPage is the enqueue result of a project with nothing left to enqueue.
func exhaustedPage(cursor efficacy.EnqueueCursor) *activities.EnqueueSkillEfficacyPageResult {
	return &activities.EnqueueSkillEfficacyPageResult{
		Scanned:    0,
		Units:      0,
		Confirmed:  0,
		Stamped:    0,
		NextCursor: cursor,
		Exhausted:  true,
	}
}

// registerIdleEnqueue answers the enqueue walk with an exhausted page, which is
// what a test that is not about the walk itself wants.
func registerIdleEnqueue(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.EnqueueSkillEfficacyPageParams) (*activities.EnqueueSkillEfficacyPageResult, error) {
			return exhaustedPage(params.Cursor), nil
		},
		activity.RegisterOptions{Name: "EnqueueSkillEfficacyPage"},
	)
}

// registerEmptyClaim answers the crash-recovery claim with nothing, which is the
// normal case when no owner has died.
func registerEmptyClaim(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.LoadReservedSkillEfficacyEvaluationsParams) (*activities.SkillEfficacyBatch, error) {
			return &activities.SkillEfficacyBatch{IDs: nil}, nil
		},
		activity.RegisterOptions{Name: "LoadReservedSkillEfficacyEvaluations"},
	)
}

func TestSkillEfficacyCoordinatorWorkflowIDIsPerProject(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	require.Equal(t, "v1:skill-efficacy:"+projectID.String(), skillEfficacyCoordinatorWorkflowID(projectID))
	require.NotEqual(t, skillEfficacyCoordinatorWorkflowID(uuid.New()), skillEfficacyCoordinatorWorkflowID(projectID))
}

func TestSkillEfficacyCoordinatorWorkflowCompletesWhenThereIsNoWork(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	projectID := uuid.New()

	enqueueCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.EnqueueSkillEfficacyPageParams) (*activities.EnqueueSkillEfficacyPageResult, error) {
			enqueueCalls++
			require.Equal(t, projectID, params.ProjectID)
			require.Equal(t, efficacy.MaxEnqueuePageSize, params.PageSize)
			return exhaustedPage(params.Cursor), nil
		},
		activity.RegisterOptions{Name: "EnqueueSkillEfficacyPage"},
	)
	reserveCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			reserveCalls++
			require.Equal(t, efficacy.MaxReservedClaimBatch, params.BatchSize)
			return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: nil, NextCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil}}, nil
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	claimCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.LoadReservedSkillEfficacyEvaluationsParams) (*activities.SkillEfficacyBatch, error) {
			claimCalls++
			require.Equal(t, projectID, params.ProjectID)
			return &activities.SkillEfficacyBatch{IDs: nil}, nil
		},
		activity.RegisterOptions{Name: "LoadReservedSkillEfficacyEvaluations"},
	)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{
		ProjectID: projectID,
		Cursor:    efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, enqueueCalls, "an exhausted walk is one page")
	require.Equal(t, 1, reserveCalls)
	require.Equal(t, 1, claimCalls)
}

func TestSkillEfficacyCoordinatorWorkflowChainsTheEnqueueCursorAndResetsItWhenExhausted(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	first := efficacy.EnqueueCursor{SeenAt: time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), ID: uuid.New()}
	second := efficacy.EnqueueCursor{SeenAt: time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC), ID: uuid.New()}

	var seen []efficacy.EnqueueCursor
	pages := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.EnqueueSkillEfficacyPageParams) (*activities.EnqueueSkillEfficacyPageResult, error) {
			seen = append(seen, params.Cursor)
			pages++
			switch pages {
			case 1:
				return &activities.EnqueueSkillEfficacyPageResult{
					Scanned: int(efficacy.MaxEnqueuePageSize), Units: 1, Confirmed: 1, Stamped: 1,
					NextCursor: first, Exhausted: false,
				}, nil
			case 2:
				return &activities.EnqueueSkillEfficacyPageResult{
					Scanned: 1, Units: 1, Confirmed: 1, Stamped: 1,
					NextCursor: second, Exhausted: true,
				}, nil
			default:
				return exhaustedPage(params.Cursor), nil
			}
		},
		activity.RegisterOptions{Name: "EnqueueSkillEfficacyPage"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: nil, NextCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil}}, nil
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	registerEmptyClaim(env)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{
		ProjectID: uuid.New(),
		Cursor:    efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []efficacy.EnqueueCursor{
		{SeenAt: time.Time{}, ID: uuid.Nil},
		first,
	}, seen, "an unexhausted page hands its cursor to the next one")
	require.Equal(t, 2, pages, "the walk stops at the page that reached the end")
}

func TestSkillEfficacyCoordinatorWorkflowPublishesReservedBatchesUntilThereIsNoWorkLeft(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	projectID := uuid.New()
	batch := []uuid.UUID{uuid.New(), uuid.New()}

	registerIdleEnqueue(env)
	reserveCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			reserveCalls++
			if reserveCalls == 1 {
				return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: batch, NextCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil}}, nil
			}
			return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: nil, NextCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil}}, nil
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	registerEmptyClaim(env)
	var published [][]uuid.UUID
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.PublishSkillEfficacyBatchParams) (*activities.PublishSkillEfficacyBatchResult, error) {
			require.Equal(t, projectID, params.ProjectID)
			published = append(published, params.IDs)
			return &activities.PublishSkillEfficacyBatchResult{
				Loaded: len(params.IDs), AlreadyPublished: 0, Scored: len(params.IDs),
				ModelFailures: 0, Failed: 0, Retryable: 0,
			}, nil
		},
		activity.RegisterOptions{Name: "PublishSkillEfficacyBatch"},
	)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{
		ProjectID: projectID,
		Cursor:    efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, [][]uuid.UUID{batch}, published)
	require.Equal(t, 2, reserveCalls, "the pass after an empty reservation is the one that stops")
}

// A run that spends every pass still finding batches to publish ran out of
// history budget, not out of work: the queue behind it is unknown, and nothing
// but this run's own next attempt is going to look at it again. It must
// continue as new rather than complete, or the backlog sits until the next
// sweep tick notices it.
func TestSkillEfficacyCoordinatorWorkflowContinuesAsNewWhenThePassCapStopsIt(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	projectID := uuid.New()

	registerIdleEnqueue(env)
	reserveCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			reserveCalls++
			// Every pass finds a batch: the queue never runs dry inside this run.
			return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: []uuid.UUID{uuid.New()}, NextCursor: efficacy.PendingCursor{}}, nil
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	registerEmptyClaim(env)
	publishCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.PublishSkillEfficacyBatchParams) (*activities.PublishSkillEfficacyBatchResult, error) {
			publishCalls++
			return &activities.PublishSkillEfficacyBatchResult{
				Loaded: len(params.IDs), AlreadyPublished: 0, Scored: len(params.IDs),
				ModelFailures: 0, Failed: 0, Retryable: 0,
			}, nil
		},
		activity.RegisterOptions{Name: "PublishSkillEfficacyBatch"},
	)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{
		ProjectID: projectID,
		Cursor:    efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
	})

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr, "the pass cap is not a completion, it is a handoff")
	require.Equal(t, skillEfficacyMaxPasses, reserveCalls)
	require.Equal(t, skillEfficacyMaxPasses, publishCalls, "every pass this run spent published a batch")
}

func TestSkillEfficacyCoordinatorWorkflowChainsThePendingCursorAcrossPasses(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	stopped := efficacy.PendingCursor{ObservedAt: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), ID: uuid.New()}

	registerIdleEnqueue(env)
	var seen []efficacy.PendingCursor
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			seen = append(seen, params.Cursor)
			// The first pass walks its page bound without emptying the queue and
			// reports where it stopped; the second reaches the end and reports the
			// head.
			if len(seen) == 1 {
				return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: []uuid.UUID{uuid.New()}, NextCursor: stopped}, nil
			}
			return &activities.ReserveSkillEfficacyEvaluationsResult{
				IDs:        nil,
				NextCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil},
			}, nil
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	registerEmptyClaim(env)
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.PublishSkillEfficacyBatchParams) (*activities.PublishSkillEfficacyBatchResult, error) {
			return &activities.PublishSkillEfficacyBatchResult{
				Loaded: len(params.IDs), AlreadyPublished: 0, Scored: len(params.IDs),
				ModelFailures: 0, Failed: 0, Retryable: 0,
			}, nil
		},
		activity.RegisterOptions{Name: "PublishSkillEfficacyBatch"},
	)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{
		ProjectID:     uuid.New(),
		Cursor:        efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
		PendingCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []efficacy.PendingCursor{
		{ObservedAt: time.Time{}, ID: uuid.Nil},
		stopped,
	}, seen, "a bounded walk hands where it stopped to the next pass")
}

func TestSkillEfficacyCoordinatorWorkflowContinuesAfterAnEmptyProgressingReservation(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	advanced := efficacy.PendingCursor{ObservedAt: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), ID: uuid.New()}

	registerIdleEnqueue(env)
	var seen []efficacy.PendingCursor
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			seen = append(seen, params.Cursor)
			switch len(seen) {
			case 1:
				return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: nil, NextCursor: advanced}, nil
			case 2:
				return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: []uuid.UUID{uuid.New()}, NextCursor: efficacy.PendingCursor{}}, nil
			default:
				return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: nil, NextCursor: efficacy.PendingCursor{}}, nil
			}
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	registerEmptyClaim(env)
	published := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.PublishSkillEfficacyBatchParams) (*activities.PublishSkillEfficacyBatchResult, error) {
			published += len(params.IDs)
			return &activities.PublishSkillEfficacyBatchResult{
				Loaded: len(params.IDs), AlreadyPublished: 0, Scored: len(params.IDs),
				ModelFailures: 0, Failed: 0, Retryable: 0,
			}, nil
		},
		activity.RegisterOptions{Name: "PublishSkillEfficacyBatch"},
	)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{ProjectID: uuid.New()})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []efficacy.PendingCursor{{}, advanced, {}}, seen)
	require.Equal(t, 1, published)
}

func TestSkillEfficacyCoordinatorWorkflowPublishesReservationsAnOwnerAbandoned(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	abandoned := []uuid.UUID{uuid.New()}

	registerIdleEnqueue(env)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: nil, NextCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil}}, nil
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	claims := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.LoadReservedSkillEfficacyEvaluationsParams) (*activities.SkillEfficacyBatch, error) {
			claims++
			if claims == 1 {
				return &activities.SkillEfficacyBatch{IDs: abandoned}, nil
			}
			return &activities.SkillEfficacyBatch{IDs: nil}, nil
		},
		activity.RegisterOptions{Name: "LoadReservedSkillEfficacyEvaluations"},
	)
	var published [][]uuid.UUID
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.PublishSkillEfficacyBatchParams) (*activities.PublishSkillEfficacyBatchResult, error) {
			published = append(published, params.IDs)
			return &activities.PublishSkillEfficacyBatchResult{
				Loaded: 1, AlreadyPublished: 1, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0,
			}, nil
		},
		activity.RegisterOptions{Name: "PublishSkillEfficacyBatch"},
	)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{
		ProjectID: uuid.New(),
		Cursor:    efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, [][]uuid.UUID{abandoned}, published, "a batch no reservation produced is still published")
}

func TestSkillEfficacyCoordinatorWorkflowCoalescesSignalsRaisedWhileItWorks(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	registerIdleEnqueue(env)
	// Three signals land while the run is working. They describe one thing —
	// that the project has work — so they cost one continuation, not three.
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			env.SignalWorkflow(SignalSkillEfficacyRequested, struct{}{})
			env.SignalWorkflow(SignalSkillEfficacyRequested, struct{}{})
			env.SignalWorkflow(SignalSkillEfficacyRequested, struct{}{})
			return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: nil, NextCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil}}, nil
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	registerEmptyClaim(env)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{
		ProjectID: uuid.New(),
		Cursor:    efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
	})

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
}

func TestSkillEfficacyCoordinatorWorkflowDrainsTheSignalThatStartedIt(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	registerIdleEnqueue(env)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			return &activities.ReserveSkillEfficacyEvaluationsResult{IDs: nil, NextCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil}}, nil
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	registerEmptyClaim(env)

	// SignalWithStart delivers the starting signal before the first task, which
	// the run must not read as work it has not looked at yet.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalSkillEfficacyRequested, struct{}{})
	}, 0)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{
		ProjectID: uuid.New(),
		Cursor:    efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(), "the starting signal is the reason for this run, not the next one")
}

func TestSkillEfficacyCoordinatorWorkflowFailsWhenPublicationCannotRecover(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	registerIdleEnqueue(env)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReserveSkillEfficacyEvaluationsParams) (*activities.ReserveSkillEfficacyEvaluationsResult, error) {
			return &activities.ReserveSkillEfficacyEvaluationsResult{
				IDs:        []uuid.UUID{uuid.New()},
				NextCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil},
			}, nil
		},
		activity.RegisterOptions{Name: "ReserveSkillEfficacyEvaluations"},
	)
	registerEmptyClaim(env)
	attempts := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.PublishSkillEfficacyBatchParams) (*activities.PublishSkillEfficacyBatchResult, error) {
			attempts++
			return nil, errors.New("clickhouse unavailable")
		},
		activity.RegisterOptions{Name: "PublishSkillEfficacyBatch"},
	)

	env.ExecuteWorkflow(SkillEfficacyCoordinatorWorkflow, SkillEfficacyCoordinatorParams{
		ProjectID: uuid.New(),
		Cursor:    efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "publish skill efficacy batch")
	require.Equal(t, 3, attempts, "the publication retry policy is what bounds an infrastructure outage")
}

func TestSkillEfficacySweepWorkflowResetsStaleReservationsBeforeSignalling(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	// One project the discovery read found a stale reservation on, one it did
	// not: the reset is the first's alone, the wake is both projects'.
	stale := efficacy.PendingWorkProject{ProjectID: uuid.New(), HasStale: true}
	pendingOnly := efficacy.PendingWorkProject{ProjectID: uuid.New(), HasStale: false}
	projects := []efficacy.PendingWorkProject{stale, pendingOnly}

	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ListSkillEfficacyProjectsParams) ([]efficacy.PendingWorkProject, error) {
			require.Equal(t, uuid.Nil, params.AfterProjectID)
			require.Equal(t, efficacy.MaxSweepProjectPage, params.PageLimit)
			return projects, nil
		},
		activity.RegisterOptions{Name: "ListSkillEfficacyProjects"},
	)
	var order []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ResetStaleSkillEfficacyReservationsParams) (*activities.ResetStaleSkillEfficacyReservationsResult, error) {
			order = append(order, "reset:"+params.ProjectID.String())
			return &activities.ResetStaleSkillEfficacyReservationsResult{Reset: 1}, nil
		},
		activity.RegisterOptions{Name: "ResetStaleSkillEfficacyReservations"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.SignalSkillEfficacyCoordinatorParams) error {
			order = append(order, "signal:"+params.ProjectID.String())
			return nil
		},
		activity.RegisterOptions{Name: "SignalSkillEfficacyCoordinator"},
	)

	env.ExecuteWorkflow(SkillEfficacySweepWorkflow, SkillEfficacySweepParams{AfterProjectID: uuid.Nil})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []string{
		"reset:" + stale.ProjectID.String(),
		"signal:" + stale.ProjectID.String(),
		"signal:" + pendingOnly.ProjectID.String(),
	}, order, "a project's recovered rows are pending before its coordinator is woken, and a project with none is only woken")
}

func TestSkillEfficacySweepWorkflowCarriesOnPastAProjectItCannotRecover(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	failing := uuid.New()
	healthy := uuid.New()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ListSkillEfficacyProjectsParams) ([]efficacy.PendingWorkProject, error) {
			return []efficacy.PendingWorkProject{
				{ProjectID: failing, HasStale: true},
				{ProjectID: healthy, HasStale: true},
			}, nil
		},
		activity.RegisterOptions{Name: "ListSkillEfficacyProjects"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ResetStaleSkillEfficacyReservationsParams) (*activities.ResetStaleSkillEfficacyReservationsResult, error) {
			if params.ProjectID == failing {
				return nil, errors.New("database unavailable")
			}
			return &activities.ResetStaleSkillEfficacyReservationsResult{Reset: 0}, nil
		},
		activity.RegisterOptions{Name: "ResetStaleSkillEfficacyReservations"},
	)
	var signalled []uuid.UUID
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.SignalSkillEfficacyCoordinatorParams) error {
			signalled = append(signalled, params.ProjectID)
			return nil
		},
		activity.RegisterOptions{Name: "SignalSkillEfficacyCoordinator"},
	)

	env.ExecuteWorkflow(SkillEfficacySweepWorkflow, SkillEfficacySweepParams{AfterProjectID: uuid.Nil})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []uuid.UUID{healthy}, signalled, "one project's outage does not cost the projects behind it")
}

func TestSkillEfficacySweepWorkflowContinuesAsNewPastAFullPage(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	full := make([]efficacy.PendingWorkProject, efficacy.MaxSweepProjectPage)
	for i := range full {
		full[i] = efficacy.PendingWorkProject{ProjectID: uuid.New(), HasStale: false}
	}
	var cursors []uuid.UUID
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ListSkillEfficacyProjectsParams) ([]efficacy.PendingWorkProject, error) {
			cursors = append(cursors, params.AfterProjectID)
			return full, nil
		},
		activity.RegisterOptions{Name: "ListSkillEfficacyProjects"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ResetStaleSkillEfficacyReservationsParams) (*activities.ResetStaleSkillEfficacyReservationsResult, error) {
			return &activities.ResetStaleSkillEfficacyReservationsResult{Reset: 0}, nil
		},
		activity.RegisterOptions{Name: "ResetStaleSkillEfficacyReservations"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalSkillEfficacyCoordinatorParams) error { return nil },
		activity.RegisterOptions{Name: "SignalSkillEfficacyCoordinator"},
	)

	env.ExecuteWorkflow(SkillEfficacySweepWorkflow, SkillEfficacySweepParams{AfterProjectID: uuid.Nil})

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.Len(t, cursors, skillEfficacyMaxSweepPages)
	require.Equal(t, full[len(full)-1].ProjectID, cursors[len(cursors)-1], "each page resumes past the last project of the one before it")
}
