package assistants

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

type switchBackend struct {
	*stubRuntimeBackend
	readiness    runtimeSwitchReadiness
	preparations int
}

func (b *switchBackend) PrepareRuntimeSwitch(context.Context, assistantRecord, []assistantRuntimeRecord, int16) (runtimeSwitchReadiness, error) {
	b.preparations++
	return b.readiness, nil
}

func TestRuntimeSelectionValidation(t *testing.T) {
	t.Parallel()
	for _, change := range []runtimeSelectionChange{
		{Version: new(3)}, {ExpectedVersion: new(2)}, {Version: new(4), ExpectedVersion: new(2)},
		{Version: new(3), ExpectedVersion: new(0)},
	} {
		require.ErrorIs(t, validateRuntimeSelection(change), errAssistantValidation)
	}
	require.NoError(t, validateRuntimeSelection(runtimeSelectionChange{}))
	require.NoError(t, validateRuntimeSelection(runtimeSelectionChange{Version: new(3), ExpectedVersion: new(2)}))
}

func TestRuntimeSwitchRejectsPendingAndProcessingEvents(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "runtime_switch_busy")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	backend := &switchBackend{stubRuntimeBackend: &stubRuntimeBackend{}, readiness: runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: func() {}}}
	core := &ServiceCore{db: conn, runtime: backend}
	for _, status := range []string{eventStatusPending, eventStatusProcessing} {
		if status == eventStatusProcessing {
			_, err := assistantrepo.New(conn).ClaimNextPendingEvent(t.Context(), assistantrepo.ClaimNextPendingEventParams{ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending, ProcessingStatus: eventStatusProcessing})
			require.NoError(t, err)
		}
		tx, err := conn.Begin(t.Context())
		require.NoError(t, err)
		queries := assistantrepo.New(tx)
		require.NoError(t, queries.AcquireAssistantAdvisoryLock(t.Context(), assistantID.String()))
		release, err := core.prepareRuntimeSelection(t.Context(), queries, projectID, assistantID, runtimeSelectionChange{Version: new(3), ExpectedVersion: new(2)})
		require.ErrorIs(t, err, errRuntimeSwitchBusy)
		require.Nil(t, release)
		require.NoError(t, tx.Rollback(t.Context()))
	}
	require.Zero(t, backend.preparations)
}

func TestRuntimeSwitchRequiresAuthoritativeBackgroundReadiness(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "runtime_switch_readiness")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	_, err = assistantrepo.New(conn).CancelPendingAssistantThreadEvents(t.Context(), assistantrepo.CancelPendingAssistantThreadEventsParams{ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending, CancelledStatus: eventStatusCancelled})
	require.NoError(t, err)
	for _, readiness := range []runtimeSwitchReadiness{
		{}, {DestinationReady: true, Settled: false, Release: func() {}},
	} {
		backend := &switchBackend{stubRuntimeBackend: &stubRuntimeBackend{}, readiness: readiness}
		core := &ServiceCore{db: conn, runtime: backend}
		tx, err := conn.Begin(t.Context())
		require.NoError(t, err)
		queries := assistantrepo.New(tx)
		require.NoError(t, queries.AcquireAssistantAdvisoryLock(t.Context(), assistantID.String()))
		release, err := core.prepareRuntimeSelection(t.Context(), queries, projectID, assistantID, runtimeSelectionChange{Version: new(3), ExpectedVersion: new(2)})
		require.Error(t, err)
		require.NoError(t, tx.Rollback(t.Context()))
		if release != nil {
			release()
		}
		row, err := assistantrepo.New(conn).GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: projectID, AssistantID: assistantID})
		require.NoError(t, err)
		require.Equal(t, int16(2), row.RuntimeVersion)
	}
}

func TestRuntimeSwitchRetiresImmutableRowsAndChecksExpectedVersion(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "runtime_switch_success")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	queries := assistantrepo.New(conn)
	_, err = queries.CancelPendingAssistantThreadEvents(t.Context(), assistantrepo.CancelPendingAssistantThreadEventsParams{ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending, CancelledStatus: eventStatusCancelled})
	require.NoError(t, err)
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{AssistantThreadID: threadID, AssistantID: assistantID, ProjectID: projectID, Backend: runtimeBackendLocal, State: runtimeStateActive}))
	old, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: projectID, AssistantID: assistantID})
	require.NoError(t, err)
	released := false
	backend := &switchBackend{stubRuntimeBackend: &stubRuntimeBackend{}, readiness: runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: func() { released = true }}}
	core := &ServiceCore{db: conn, runtime: backend}
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	qtx := assistantrepo.New(tx)
	require.NoError(t, qtx.AcquireAssistantAdvisoryLock(t.Context(), assistantID.String()))
	release, err := core.prepareRuntimeSelection(t.Context(), qtx, projectID, assistantID, runtimeSelectionChange{Version: new(3), ExpectedVersion: new(2)})
	require.NoError(t, err)
	require.False(t, released)
	require.NoError(t, tx.Commit(t.Context()))
	release()
	require.True(t, released)
	require.Equal(t, 1, backend.stopCount)
	retired, err := queries.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: old.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.True(t, retired.Ended)
	require.Equal(t, int16(2), retired.RuntimeVersion)
	current, err := queries.GetAssistant(t.Context(), assistantrepo.GetAssistantParams{AssistantID: assistantID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, int16(3), current.RuntimeVersion)
	tx, err = conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	qtx = assistantrepo.New(tx)
	require.NoError(t, qtx.AcquireAssistantAdvisoryLock(t.Context(), assistantID.String()))
	_, err = core.prepareRuntimeSelection(t.Context(), qtx, projectID, assistantID, runtimeSelectionChange{Version: new(3), ExpectedVersion: new(2)})
	require.ErrorIs(t, err, errRuntimeSwitchConflict)
}

func TestRuntimeIngestionWaitsForSwitchFence(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "runtime_switch_ingestion")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, assistantrepo.New(tx).AcquireAssistantAdvisoryLock(t.Context(), assistantID.String()))
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = assistantrepo.New(conn).InsertAssistantThreadEvent(ctx, assistantrepo.InsertAssistantThreadEventParams{
		ProjectID: projectID, AssistantID: assistantID, AssistantThreadID: threadID,
		EventID: uuid.NewString(), CorrelationID: "corr-1", Status: eventStatusPending,
		NormalizedPayloadJson: []byte(`{}`), SourcePayloadJson: []byte(`{}`),
	})
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.NoError(t, tx.Rollback(t.Context()))
	require.ErrorIs(t, tx.Rollback(t.Context()), pgx.ErrTxClosed)
}

func TestRuntimeSwitchAPIRequiresAssistantWriteAndIgnoresUIFlags(t *testing.T) {
	t.Parallel()
	svc, ctx, projectID := newRBACService(t)
	writeCtx := authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID))
	assistant, err := svc.CreateAssistant(writeCtx, &gen.CreateAssistantPayload{Name: "Runtime switch", Model: "openai/gpt-4o-mini", Instructions: "", Status: new(StatusPaused)})
	require.NoError(t, err)
	require.Equal(t, 2, assistant.RuntimeVersion)
	payload := &gen.UpdateAssistantPayload{ID: assistant.ID, RuntimeVersion: new(3), ExpectedRuntimeVersion: new(2)}
	deniedCtx := authztest.WithExactGrants(t, ctx)
	_, err = svc.UpdateAssistant(deniedCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
	svc.core.runtime = &switchBackend{stubRuntimeBackend: &stubRuntimeBackend{}, readiness: runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: func() {}}}
	// No frontend feature flag is installed in the context or the service.
	updated, err := svc.UpdateAssistant(writeCtx, payload)
	require.NoError(t, err)
	require.Equal(t, 3, updated.RuntimeVersion)
	// An unrelated settings update preserves the selected runtime.
	updated, err = svc.UpdateAssistant(writeCtx, &gen.UpdateAssistantPayload{ID: assistant.ID, Name: new("Renamed assistant")})
	require.NoError(t, err)
	require.Equal(t, 3, updated.RuntimeVersion)
}

func TestRuntimeSwitchRejectsPendingAdmission(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "runtime_switch_admission")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	queries := assistantrepo.New(conn)
	_, err = queries.CancelPendingAssistantThreadEvents(t.Context(), assistantrepo.CancelPendingAssistantThreadEventsParams{ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending, CancelledStatus: eventStatusCancelled})
	require.NoError(t, err)
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: projectID, AssistantID: assistantID, AssistantThreadID: threadID, Backend: runtimeBackendLocal, State: runtimeStateStarting}))
	core := &ServiceCore{db: conn}
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	qtx := assistantrepo.New(tx)
	require.NoError(t, qtx.AcquireAssistantAdvisoryLock(t.Context(), assistantID.String()))
	_, err = core.prepareRuntimeSelection(t.Context(), qtx, projectID, assistantID, runtimeSelectionChange{Version: new(3), ExpectedVersion: new(2)})
	require.ErrorIs(t, err, errRuntimeSwitchBusy)
}

func TestRuntimeSwitchWaitsForPendingIngestionCommit(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "runtime_switch_ingest_commit")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	_, err = assistantrepo.New(conn).CancelPendingAssistantThreadEvents(t.Context(), assistantrepo.CancelPendingAssistantThreadEventsParams{ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending, CancelledStatus: eventStatusCancelled})
	require.NoError(t, err)
	ingest, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = ingest.Rollback(t.Context()) }()
	_, err = assistantrepo.New(ingest).InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{
		ProjectID: projectID, AssistantID: assistantID, AssistantThreadID: threadID,
		EventID: uuid.NewString(), CorrelationID: "corr-1", Status: eventStatusPending,
		NormalizedPayloadJson: []byte(`{}`), SourcePayloadJson: []byte(`{}`),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	contender, err := conn.Begin(t.Context())
	require.NoError(t, err)
	err = assistantrepo.New(contender).AcquireAssistantAdvisoryLock(ctx, assistantID.String())
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	_ = contender.Rollback(t.Context())
	require.NoError(t, ingest.Commit(t.Context()))
	contender, err = conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = contender.Rollback(t.Context()) }()
	queries := assistantrepo.New(contender)
	require.NoError(t, queries.AcquireAssistantAdvisoryLock(t.Context(), assistantID.String()))
	core := &ServiceCore{db: conn}
	_, err = core.prepareRuntimeSelection(t.Context(), queries, projectID, assistantID, runtimeSelectionChange{Version: new(3), ExpectedVersion: new(2)})
	require.ErrorIs(t, err, errRuntimeSwitchBusy)
}
