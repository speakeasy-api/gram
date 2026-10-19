package assistants

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestReviewLocalReadinessFailureStopsStartedIncarnation(t *testing.T) {
	t.Parallel()
	k, engine, r, _ := kitLocalFixture(t)
	failure := errors.New("readiness failed")
	k.config.Ready = func(context.Context, assistantRuntimeRecord, *url.URL) error { return failure }
	_, err := k.Ensure(t.Context(), r)
	require.ErrorIs(t, err, failure)
	require.False(t, engine.info.Running)
	require.Contains(t, engine.calls, "stop")
	require.NoError(t, k.ReapStoppedMachine(t.Context(), r))
	require.Empty(t, engine.info.ID)
	require.Nil(t, engine.volume)
}

func TestReviewKitCallbackRejectsFailedAuthority(t *testing.T) {
	t.Parallel()
	for _, state := range []string{runtimeStateFailed, runtimeStateExpiring, runtimeStateStopped} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			core, db, key, _ := kitSessionFixture(t)
			_, err := core.StoreKitRuntimeSession(t.Context(), key, "test-session")
			require.NoError(t, err)
			err = assistantrepo.New(db).SetAssistantRuntimeStateForTest(t.Context(), assistantrepo.SetAssistantRuntimeStateForTestParams{ProjectID: key.ProjectID, RuntimeID: key.RuntimeID, State: state, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
			require.NoError(t, err)
			tx := testenv.BeginTx(t, t.Context(), db)
			defer func() { _ = tx.Rollback(t.Context()) }()
			_, err = core.ValidateKitRuntimeSessionInTx(t.Context(), tx, key, "test-session")
			require.ErrorIs(t, err, ErrKitRuntimeBinding)
		})
	}
}

func TestReviewKitSwitchTokenUsesLockHoldingTransaction(t *testing.T) {
	t.Parallel()
	core, db, key, _ := kitSessionFixture(t)
	require.NoError(t, core.StoreKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID, "test-token"))
	tx := testenv.BeginTx(t, t.Context(), db)
	defer func() { _ = tx.Rollback(t.Context()) }()
	q := assistantrepo.New(tx)
	require.NoError(t, q.AcquireAssistantAdvisoryLock(t.Context(), key.AssistantID.String()))
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), runtimeSwitchQueriesKey{}, q), time.Second)
	defer cancel()
	token, err := core.LoadKitGatewayToken(ctx, key.ProjectID, key.AssistantID, key.RuntimeID)
	require.NoError(t, err)
	require.Equal(t, "test-token", token)
	require.Same(t, q, runtimeSwitchQueries(ctx, db))
	// The real local composition callback uses this control-only reader.
	// Holding the runtime row lock ensures a pool-based authority read cannot
	// accidentally pass the regression test by using another connection.
	_, err = q.GetAssistantRuntimeForAdmission(ctx, assistantrepo.GetAssistantRuntimeForAdmissionParams{ID: key.RuntimeID, ProjectID: key.ProjectID})
	require.NoError(t, err)
	token, err = core.loadKitGatewayTokenAlreadyLocked(ctx, assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID})
	require.NoError(t, err)
	require.Equal(t, "test-token", token)
}

func TestReviewKitInterruptLostAuthorityIsNotAcknowledged(t *testing.T) {
	t.Parallel()
	core, db, key, _ := kitSessionFixture(t)
	_, err := core.StoreKitRuntimeSession(t.Context(), key, "test-session")
	require.NoError(t, err)
	q := assistantrepo.New(db)
	event, err := q.ClaimNextPendingEvent(t.Context(), assistantrepo.ClaimNextPendingEventParams{ProjectID: key.ProjectID, ThreadID: key.ThreadID, PendingStatus: eventStatusPending, ProcessingStatus: eventStatusProcessing})
	require.NoError(t, err)
	_, err = q.RequestKitInterrupt(t.Context(), assistantrepo.RequestKitInterruptParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeID: key.RuntimeID, ThreadID: key.ThreadID})
	require.NoError(t, err)
	require.NoError(t, q.RetireAssistantRuntimesForSwitch(t.Context(), assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID}))
	p := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Core: core}}
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID}
	req := runTurnRequest{EventID: event.ID, EventAttempt: int(event.Attempts), ThreadID: key.ThreadID}
	require.ErrorIs(t, p.acknowledgeKitInterrupt(t.Context(), r, req, "cancelled"), ErrKitInterruptPending)
	req.EventAttempt++
	require.NoError(t, p.acknowledgeKitInterrupt(t.Context(), r, req, "cancelled"))
}

func TestReviewEventAdmissionLocksLifecycleRow(t *testing.T) {
	t.Parallel()
	_, db, key, _ := kitSessionFixture(t)
	tx := testenv.BeginTx(t, t.Context(), db)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err := assistantrepo.New(tx).GetAssistantRuntimeForAdmission(t.Context(), assistantrepo.GetAssistantRuntimeForAdmissionParams{ID: key.RuntimeID, ProjectID: key.ProjectID})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	err = assistantrepo.New(db).SetAssistantRuntimeStateForTest(ctx, assistantrepo.SetAssistantRuntimeStateForTestParams{ProjectID: key.ProjectID, RuntimeID: key.RuntimeID, State: runtimeStateExpiring, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.NoError(t, tx.Commit(t.Context()))
	err = assistantrepo.New(db).SetAssistantRuntimeStateForTest(t.Context(), assistantrepo.SetAssistantRuntimeStateForTestParams{ProjectID: key.ProjectID, RuntimeID: key.RuntimeID, State: runtimeStateExpiring, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
	require.NoError(t, err)
}

func TestReviewRuntimeSwitchRetirementSurvivesTransactionOutcome(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"rollback", "commit", "crash"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			core, db, key, _ := kitSessionFixture(t)
			q := assistantrepo.New(db)
			_, err := q.CancelPendingAssistantThreadEvents(t.Context(), assistantrepo.CancelPendingAssistantThreadEventsParams{ProjectID: key.ProjectID, ThreadID: key.ThreadID, PendingStatus: eventStatusPending, CancelledStatus: eventStatusCancelled})
			require.NoError(t, err)
			backend := &switchBackend{stubRuntimeBackend: &stubRuntimeBackend{}, readiness: runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: func() {}}}
			core.runtime = backend
			tx := testenv.BeginTx(t, t.Context(), db)
			qt := assistantrepo.New(tx)
			require.NoError(t, qt.AcquireAssistantAdvisoryLock(t.Context(), key.AssistantID.String()))
			release, err := core.prepareRuntimeSelection(t.Context(), qt, key.ProjectID, key.AssistantID, runtimeSelectionChange{Version: new(2), ExpectedVersion: new(3)})
			require.NoError(t, err)
			require.Zero(t, backend.stopCount, "must not stop before commit")
			if outcome == "rollback" {
				require.NoError(t, tx.Rollback(t.Context()))
				release()
				require.Zero(t, backend.stopCount)
				row, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ProjectID: key.ProjectID, ID: key.RuntimeID})
				require.NoError(t, err)
				require.Equal(t, runtimeStateActive, row.State)
				require.False(t, row.Ended)
				return
			}
			require.NoError(t, tx.Commit(t.Context()))
			if outcome == "commit" {
				release()
			} else {
				// Simulate process death before the post-commit callback. The existing
				// janitor must consume the persisted intent even with empty metadata.
				pending, err := q.HasPendingRuntimeSwitchRetirement(t.Context(), assistantrepo.HasPendingRuntimeSwitchRetirementParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID})
				require.NoError(t, err)
				require.True(t, pending)
				_, err = core.ReapStoppedAssistantRuntimes(t.Context(), ReapStoppedAssistantRuntimesParams{StoppedTTL: time.Hour, BatchSize: 10})
				require.NoError(t, err)
			}
			require.Equal(t, 1, backend.stopCount)
			row, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ProjectID: key.ProjectID, ID: key.RuntimeID})
			require.NoError(t, err)
			require.True(t, row.Ended)
			require.Equal(t, runtimeStateStopped, row.State)
		})
	}
}

type reviewFailingStopBackend struct {
	*switchBackend
	stopErr error
}

func (b *reviewFailingStopBackend) Stop(ctx context.Context, r assistantRuntimeRecord) error {
	if b.stopErr != nil {
		return b.stopErr
	}
	return b.switchBackend.Stop(ctx, r)
}

func TestReviewRuntimeSwitchFailedStopRetriesDurably(t *testing.T) {
	t.Parallel()
	core, db, key, _ := kitSessionFixture(t)
	q := assistantrepo.New(db)
	_, err := q.CancelPendingAssistantThreadEvents(t.Context(), assistantrepo.CancelPendingAssistantThreadEventsParams{ProjectID: key.ProjectID, ThreadID: key.ThreadID, PendingStatus: eventStatusPending, CancelledStatus: eventStatusCancelled})
	require.NoError(t, err)
	backend := &reviewFailingStopBackend{switchBackend: &switchBackend{stubRuntimeBackend: &stubRuntimeBackend{}, readiness: runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: func() {}}}, stopErr: errors.New("stop temporarily unavailable")}
	core.runtime = backend
	tx := testenv.BeginTx(t, t.Context(), db)
	qt := assistantrepo.New(tx)
	require.NoError(t, qt.AcquireAssistantAdvisoryLock(t.Context(), key.AssistantID.String()))
	release, err := core.prepareRuntimeSelection(t.Context(), qt, key.ProjectID, key.AssistantID, runtimeSelectionChange{Version: new(2), ExpectedVersion: new(3)})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	release()
	pending, err := q.HasPendingRuntimeSwitchRetirement(t.Context(), assistantrepo.HasPendingRuntimeSwitchRetirementParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID})
	require.NoError(t, err)
	require.True(t, pending)
	tx = testenv.BeginTx(t, t.Context(), db)
	qt = assistantrepo.New(tx)
	require.NoError(t, qt.AcquireAssistantAdvisoryLock(t.Context(), key.AssistantID.String()))
	_, err = core.prepareRuntimeSelection(t.Context(), qt, key.ProjectID, key.AssistantID, runtimeSelectionChange{Version: new(3), ExpectedVersion: new(2)})
	require.ErrorIs(t, err, errRuntimeSwitchBusy, "cannot reuse a resource with a pending stop")
	require.NoError(t, tx.Rollback(t.Context()))
	backend.stopErr = nil
	_, err = core.ReapStoppedAssistantRuntimes(t.Context(), ReapStoppedAssistantRuntimesParams{StoppedTTL: time.Hour, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, 1, backend.stopCount)
	pending, err = q.HasPendingRuntimeSwitchRetirement(t.Context(), assistantrepo.HasPendingRuntimeSwitchRetirementParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID})
	require.NoError(t, err)
	require.False(t, pending)
}
