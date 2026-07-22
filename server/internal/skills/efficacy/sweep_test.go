package efficacy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPendingWorkProjectsRejectsPageLimitOutsideBounds(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_sweep_page_limit")

	for _, pageLimit := range []int32{0, -1, MaxSweepProjectPage + 1} {
		_, err := PendingWorkProjects(t.Context(), fixture.db, uuid.Nil, StaleReservationAfter, pageLimit)
		require.ErrorContains(t, err, "page limit must be between")
	}
}

func TestPendingWorkProjectsFindsAProjectWithNothingEnqueuedYet(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_sweep_unenqueued")

	require.Empty(t, fixture.pendingWork(t), "an empty project holds no work")

	sessionID := "sweep-unenqueued"
	fixture.seedChat(t, sessionID, 1, 90*time.Minute)
	fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-3*time.Hour))

	require.Equal(t, []PendingWorkProject{{ProjectID: fixture.projectID, HasStale: false}}, fixture.pendingWork(t),
		"a reconciled activation no walk has reached is work, and holds no reservation to recover")
}

func TestPendingWorkProjectsFindsAProjectWhoseQueueIsStillPending(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_sweep_pending")

	skill := fixture.captureSkill(t, "efficacy-sweep-pending")
	fixture.seedUnits(t, skill, "sweep-pending", 1, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, 1)

	// Every activation is stamped now, so the only thing naming the project is
	// the evaluation the walk produced.
	require.Equal(t, []PendingWorkProject{{ProjectID: fixture.projectID, HasStale: false}}, fixture.pendingWork(t),
		"a pending-only project is swept, and the sweep has nothing to reset for it")
}

func TestPendingWorkProjectsFindsAProjectWhoseOnlyWorkIsAnAbandonedReservation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_sweep_stale")

	skill := fixture.captureSkill(t, "efficacy-sweep-stale")
	fixture.seedUnits(t, skill, "sweep-stale", 1, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, 1)

	reserved, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, 1)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	claimed := fixture.reservedEvaluations(t, 1)
	require.Len(t, claimed, 1)
	reservedAt := claimed[0].UpdatedAt.Time

	// Nothing is left to enqueue and nothing is pending, so a project holding
	// only this reservation is invisible to every other source.
	require.Empty(t, fixture.pendingWork(t),
		"a reservation whose owner is still inside the staleness window is not work")

	// The cutoff is now - staleAfter. Waiting until the reservation is a clear
	// gap behind it is what makes the boundary the assertion rather than the
	// scheduler.
	const gap = time.Second
	require.Eventually(t, func() bool {
		return time.Since(reservedAt) >= 2*gap
	}, 30*time.Second, 100*time.Millisecond)

	stale, err := PendingWorkProjects(ctx, fixture.db, uuid.Nil, time.Since(reservedAt)-gap, MaxSweepProjectPage)
	require.NoError(t, err)
	require.Equal(t, []PendingWorkProject{{ProjectID: fixture.projectID, HasStale: true}}, stale,
		"an abandoned reservation is the sweeper's own work, and the row that names it says so")

	// Recovering it puts the row back in the queue, which is what the coordinator
	// the sweep then signals goes on to reserve again.
	reset, err := ResetStaleReservations(ctx, fixture.db, fixture.projectID, time.Since(reservedAt)-gap)
	require.NoError(t, err)
	require.Equal(t, int64(1), reset)

	pending := fixture.pendingEvaluations(t)
	require.Len(t, pending, 1)
	require.Equal(t, StatePending, pending[0].State)
	require.Equal(t, []PendingWorkProject{{ProjectID: fixture.projectID, HasStale: false}}, fixture.pendingWork(t),
		"the recovered row keeps the project in the sweep until it is scored, with nothing left to reset")
}

func TestPendingWorkProjectsPagesPastTheProjectItIsGiven(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_sweep_paging")

	sessionID := "sweep-paging"
	fixture.seedChat(t, sessionID, 1, 90*time.Minute)
	fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-3*time.Hour))
	require.Equal(t, []PendingWorkProject{{ProjectID: fixture.projectID, HasStale: false}}, fixture.pendingWork(t))

	past, err := PendingWorkProjects(t.Context(), fixture.db, fixture.projectID, StaleReservationAfter, MaxSweepProjectPage)
	require.NoError(t, err)
	require.Empty(t, past, "a page starts strictly after the project it resumes from")
}

// pendingWork is the whole estate this fixture's database can see, which is only
// ever the fixture's own project because each one clones its own database.
func (f efficacyFixture) pendingWork(t *testing.T) []PendingWorkProject {
	t.Helper()

	projects, err := PendingWorkProjects(t.Context(), f.db, uuid.Nil, StaleReservationAfter, MaxSweepProjectPage)
	require.NoError(t, err)

	return projects
}
