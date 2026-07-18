package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var assistantsInfra *testenv.Environment

func newTestAuditLogger() *audit.Logger { return audit.NewLogger() }

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch assistants test infrastructure: %v", err)
	}
	assistantsInfra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup assistants test infrastructure: %v", err)
	}
	os.Exit(code)
}

func TestServiceCoreAdmitPendingThreadsUsesFlyBackend(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	admitted, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)
	require.Equal(t, projectID, admitted.ProjectID)

	runtime, err := assistantsrepo.New(conn).GetActiveAssistantRuntimeByThreadID(t.Context(), assistantsrepo.GetActiveAssistantRuntimeByThreadIDParams{
		AssistantThreadID: threadID,
		ProjectID:         projectID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeBackendFlyIO, runtime.Backend)
}

func TestServiceCoreAdmitPendingThreadsCapsFanOut(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_admit_cap")
	require.NoError(t, err)

	ctx := t.Context()
	assistantID, pending := seedAssistantWithPendingThreads(t, conn, "assistants-cap", 2, 3)
	preActivateV2Runtime(t, conn, assistantID, pending[0])

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	admitted, err := core.AdmitPendingThreads(ctx, assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{pending[0], pending[1]}, admitted.ThreadIDs, "admit must release threads up to MaxConcurrency (2)")
}

func TestServiceCoreAdmitPendingThreadsBlocksWhenActiveAtCap(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_admit_blocked")
	require.NoError(t, err)

	ctx := t.Context()
	assistantID, active, pending := seedAssistantWithActiveAndPending(t, conn, "assistants-blocked", 2, 2, 1)
	require.NotEmpty(t, pending)
	preActivateV2Runtime(t, conn, assistantID, active[0])

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	admitted, err := core.AdmitPendingThreads(ctx, assistantID)
	require.NoError(t, err)
	require.Empty(t, admitted.ThreadIDs, "admit must hold the pending thread when existing-active siblings saturate the cap")
	require.NotContains(t, admitted.ThreadIDs, pending[0], "the held-back pending thread must not slip through")
}

func TestServiceCoreAdmitPendingThreadsReleasesPartialHeadroom(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_admit_partial")
	require.NoError(t, err)

	ctx := t.Context()
	assistantID, active, _ := seedAssistantWithActiveAndPending(t, conn, "assistants-partial", 2, 1, 2)
	preActivateV2Runtime(t, conn, assistantID, active[0])

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	admitted, err := core.AdmitPendingThreads(ctx, assistantID)
	require.NoError(t, err)
	require.Len(t, admitted.ThreadIDs, 1, "cap=2 with 1 existing-active sibling leaves headroom for 1 pending admit")
}

func TestServiceCoreAdmitPendingThreadsBypassesCapForReservedStarter(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_admit_cold_cap_bypass")
	require.NoError(t, err)

	ctx := t.Context()
	// MaxConcurrency=1, one warm sibling already counts against the cap,
	// no live v2 runtime row → cold admit reserves a new one. The reserved
	// starter must be admitted or the runtime stays in starting forever.
	assistantID, _, pending := seedAssistantWithActiveAndPending(t, conn, "assistants-cold-bypass", 1, 1, 1)
	require.NotEmpty(t, pending)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	admitted, err := core.AdmitPendingThreads(ctx, assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{pending[0]}, admitted.ThreadIDs, "cold-admit must release the starter even when active count is at the cap")
}

func TestServiceCoreEnsureWarmupThreadBootsRuntimeViaTurnMachinery(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_warmup")
	require.NoError(t, err)

	ctx := t.Context()
	assistantID, projectID := seedAssistant(t, conn, "assistants-warmup", 2)

	metadata := []byte(`{"app_name":"gram-asst-warm"}`)
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, ensureResult: RuntimeBackendEnsureResult{ColdStart: true, BackendMetadataJSON: metadata}}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.EnsureWarmupThread(ctx, assistantID)
	require.NoError(t, err)
	require.True(t, result.ShouldSignal)
	require.Equal(t, projectID, result.ProjectID)

	row, err := assistantsrepo.New(conn).GetAssistantRuntimeV2(ctx, assistantsrepo.GetAssistantRuntimeV2Params{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStarting, row.State)
	require.Equal(t, result.ThreadID, row.AssistantThreadID, "the runtime row must be keyed to the warmup thread")

	// Signalling the thread workflow runs ProcessThreadEvents — the same
	// path a turn takes. With no events it boots the runtime and returns.
	processResult, err := core.ProcessThreadEvents(ctx, projectID, result.ThreadID)
	require.NoError(t, err)
	require.True(t, processResult.RuntimeActive)
	require.True(t, processResult.BootstrappedRuntime, "warmup boot must report bootstrap so siblings get admitted")
	require.False(t, processResult.ProcessedAnyEvent, "the warmup thread must never process events")

	row, err = assistantsrepo.New(conn).GetAssistantRuntimeV2(ctx, assistantsrepo.GetAssistantRuntimeV2Params{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeStateActive, row.State)
	require.JSONEq(t, string(metadata), string(row.BackendMetadataJson))
}

func TestServiceCoreEnsureWarmupThreadIsIdempotent(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_warmup_idem")
	require.NoError(t, err)

	ctx := t.Context()
	assistantID, projectID := seedAssistant(t, conn, "assistants-warmup-idem", 2)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	first, err := core.EnsureWarmupThread(ctx, assistantID)
	require.NoError(t, err)
	require.True(t, first.ShouldSignal)

	// A repeat (lost signal, double create) re-signals the same thread; the
	// reserved row is untouched.
	second, err := core.EnsureWarmupThread(ctx, assistantID)
	require.NoError(t, err)
	require.True(t, second.ShouldSignal)
	require.Equal(t, first.ThreadID, second.ThreadID)

	row, err := assistantsrepo.New(conn).GetAssistantRuntimeV2(ctx, assistantsrepo.GetAssistantRuntimeV2Params{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	require.NoError(t, err)
	require.Equal(t, first.ThreadID, row.AssistantThreadID)
}

func TestServiceCoreWarmupThreadDoesNotConsumeConcurrencySlot(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_warmup_cap")
	require.NoError(t, err)

	ctx := t.Context()
	assistantID, projectID := seedAssistant(t, conn, "assistants-warmup-cap", 1)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	warmup, err := core.EnsureWarmupThread(ctx, assistantID)
	require.NoError(t, err)
	require.True(t, warmup.ShouldSignal)
	_, err = core.ProcessThreadEvents(ctx, projectID, warmup.ThreadID)
	require.NoError(t, err)

	// With max_concurrency=1 and the warmup thread freshly stamped, the
	// first real turn must still be admitted against the active runtime.
	pending := seedThreadWithEvent(t, conn, assistantID, "assistants-warmup-cap", "first-turn", eventStatusPending)
	admitted, err := core.AdmitPendingThreads(ctx, assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{pending}, admitted.ThreadIDs, "the warmup thread must not occupy a concurrency slot")
}

func TestServiceCoreEnsureWarmupThreadSkipsWhenTrafficOwnsRuntime(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_warmup_noop")
	require.NoError(t, err)

	ctx := t.Context()
	assistantID, pending := seedAssistantWithPendingThreads(t, conn, "assistants-warmup-noop", 2, 1)
	preActivateV2Runtime(t, conn, assistantID, pending[0])

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.EnsureWarmupThread(ctx, assistantID)
	require.NoError(t, err)
	require.False(t, result.ShouldSignal, "a runtime row owned by organic traffic must be left alone")
}

func TestServiceCoreEnsureWarmupThreadSkipsInactiveAssistant(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_warmup_paused")
	require.NoError(t, err)

	ctx := t.Context()
	assistantID, projectID := seedAssistant(t, conn, "assistants-warmup-paused", 2)
	_, err = assistantsrepo.New(conn).UpdateAssistant(ctx, assistantsrepo.UpdateAssistantParams{
		AssistantID:    assistantID,
		ProjectID:      projectID,
		Name:           pgtype.Text{},
		Model:          pgtype.Text{},
		Instructions:   pgtype.Text{},
		WarmTtlSeconds: pgtype.Int8{},
		MaxConcurrency: pgtype.Int8{},
		Status:         pgtype.Text{String: StatusPaused, Valid: true},
	})
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.EnsureWarmupThread(ctx, assistantID)
	require.NoError(t, err)
	require.False(t, result.ShouldSignal)

	_, err = assistantsrepo.New(conn).GetAssistantRuntimeV2(ctx, assistantsrepo.GetAssistantRuntimeV2Params{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "paused assistants must not be warmed")
}

func seedAssistantWithPendingThreads(t *testing.T, conn *pgxpool.Pool, slug string, maxConcurrency int, pending int) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	assistantID, _, pendingIDs := seedAssistantWithActiveAndPending(t, conn, slug, maxConcurrency, 0, pending)
	return assistantID, pendingIDs
}

func seedAssistantWithActiveAndPending(t *testing.T, conn *pgxpool.Pool, slug string, maxConcurrency, active, pending int) (uuid.UUID, []uuid.UUID, []uuid.UUID) {
	t.Helper()
	assistantID, _ := seedAssistant(t, conn, slug, maxConcurrency)
	activeIDs := make([]uuid.UUID, 0, active)
	for i := range active {
		activeIDs = append(activeIDs, seedThreadWithEvent(t, conn, assistantID, slug, fmt.Sprintf("active-%d", i), eventStatusCompleted))
	}
	pendingIDs := make([]uuid.UUID, 0, pending)
	for i := range pending {
		pendingIDs = append(pendingIDs, seedThreadWithEvent(t, conn, assistantID, slug, fmt.Sprintf("pending-%d", i), eventStatusPending))
	}
	return assistantID, activeIDs, pendingIDs
}

func seedAssistant(t *testing.T, conn *pgxpool.Pool, slug string, maxConcurrency int) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: "org-test",
	})
	require.NoError(t, err)
	a, err := assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       proj.ID,
		OrganizationID:  "org-test",
		CreatedByUserID: pgtype.Text{},
		Name:            "Assistant",
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  int64(maxConcurrency),
		Status:          StatusActive,
	})
	require.NoError(t, err)
	return a.ID, proj.ID
}

func seedThreadWithEvent(t *testing.T, conn *pgxpool.Pool, assistantID uuid.UUID, slugBase, correlation, status string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	row, err := assistantsrepo.New(conn).GetAssistantForDispatch(ctx, assistantID)
	require.NoError(t, err)
	chatID := uuid.New()
	require.NoError(t, assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      row.ProjectID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	}))
	threadID, err := assistantsrepo.New(conn).UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     row.ProjectID,
		CorrelationID: correlation,
		ChatID:        chatID,
		SourceKind:    sourceKindSlack,
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)
	_, err = assistantsrepo.New(conn).InsertAssistantThreadEvent(ctx, assistantsrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     threadID,
		AssistantID:           assistantID,
		ProjectID:             row.ProjectID,
		TriggerInstanceID:     uuid.NullUUID{Valid: false},
		EventID:               "evt-" + correlation,
		CorrelationID:         correlation,
		Status:                status,
		NormalizedPayloadJson: []byte(`{"text":"hi"}`),
		SourcePayloadJson:     []byte("{}"),
	})
	require.NoError(t, err)
	return threadID
}

// preActivateV2Runtime reserves and then transitions the v2 runtime row to
// `active` so admitPendingThreadsV2 bypasses the cold-start firstThreadOnly
// guard and exercises the cap logic on its own. anchor must be a real
// assistant_threads row id under this assistant (the column carries a
// "who triggered cold admit" reference and is FK-checked).
func preActivateV2Runtime(t *testing.T, conn *pgxpool.Pool, assistantID, anchor uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	row, err := assistantsrepo.New(conn).GetAssistantForDispatch(ctx, assistantID)
	require.NoError(t, err)
	require.NoError(t, assistantsrepo.New(conn).ReserveAssistantRuntimeV2(ctx, assistantsrepo.ReserveAssistantRuntimeV2Params{
		AssistantThreadID: anchor,
		AssistantID:       assistantID,
		ProjectID:         row.ProjectID,
		Backend:           runtimeBackendFlyIO,
		State:             runtimeStateStarting,
	}))
	runtime, err := assistantsrepo.New(conn).LookupActiveAssistantRuntimeV2(ctx, assistantsrepo.LookupActiveAssistantRuntimeV2Params{
		ProjectID:   row.ProjectID,
		AssistantID: assistantID,
	})
	require.NoError(t, err)
	require.NoError(t, assistantsrepo.New(conn).SetAssistantRuntimeActive(ctx, assistantsrepo.SetAssistantRuntimeActiveParams{
		ActiveState: runtimeStateActive,
		WarmUntil:   pgtype.Timestamptz{Time: time.Now().UTC().Add(10 * time.Minute), Valid: true},
		RuntimeID:   runtime.ID,
		ProjectID:   row.ProjectID,
	}))
}

func TestWarmRemainingSecondsKeepsBusyRunnerAlive(t *testing.T) {
	t.Parallel()

	// Idle is derived from min(threads.idle_seconds) — &0 marks any thread
	// with a turn in flight, in which case ExpireThreadRuntime must revert
	// to active. A nil idle means the runner reported zero live threads
	// (fully idle VM) so Stop is correct.
	zero := uint64(0)
	require.Positive(t, warmRemainingSeconds(&zero, 300), "busy runner (idle=&0) must keep a positive warm window")
	require.Zero(t, warmRemainingSeconds(nil, 300), "no live threads must collapse to a Stop decision")
}

func TestServiceCoreExpireThreadRuntimeRevertsWhenTurnInFlight(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_expire_busy")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	runtimeID := uuid.New()
	warmUntil := time.Now().UTC().Add(-1 * time.Second)
	now := time.Now().UTC()
	err = assistantsrepo.New(conn).CreateAssistantRuntime(t.Context(), assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  runtimeID,
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{Time: warmUntil, Valid: true},
		LastHeartbeatAt:     pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: now, Valid: true},
		EndedAt:             pgtype.Timestamptz{},
		DeletedAt:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	var stopCalls atomic.Int64
	logger := testenv.NewLogger(t)
	busyIdle := uint64(0)
	backend := testRuntimeBackend{
		backend:      runtimeBackendFlyIO,
		statusResult: RuntimeBackendStatus{Configured: true, IdleSeconds: &busyIdle},
		stopCalls:    &stopCalls,
	}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())

	result, err := core.ExpireThreadRuntime(t.Context(), projectID, threadID, DefaultWarmTTLSeconds)
	require.NoError(t, err)
	require.False(t, result.Stopped, "runner reports turn in flight (idle=&0); expiry must revert, not tear down")
	require.Positive(t, result.RemainingSeconds, "revert path must hand back a positive warm window for the workflow to re-arm")
	require.Equal(t, int64(0), stopCalls.Load(), "Stop must not be invoked while a turn is executing")

	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{
		ID:        runtimeID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeStateActive, runtime.State, "runtime row must be reverted from expiring back to active")
	require.False(t, runtime.Deleted)
}

// TestServiceCoreExpireThreadRuntimeRetryAfterStopFailureIsIdempotent guards
// the Temporal-retry path: if the first ExpireThreadRuntime attempt CAS'd the
// row from active->expiring but then Stop() failed, the activity returns an
// error and Temporal retries. The retry must reuse the existing expiring row
// and complete the teardown rather than treating the CAS miss as "another
// actor handled it" (which would leak the backend VM and wedge the thread on
// the partial unique index).
func TestServiceCoreExpireThreadRuntimeRetryAfterStopFailureIsIdempotent(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_expire_retry")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	runtimeID := uuid.New()
	warmUntil := time.Now().UTC().Add(-1 * time.Second)
	now := time.Now().UTC()
	err = assistantsrepo.New(conn).CreateAssistantRuntime(t.Context(), assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  runtimeID,
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{Time: warmUntil, Valid: true},
		LastHeartbeatAt:     pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: now, Valid: true},
		EndedAt:             pgtype.Timestamptz{},
		DeletedAt:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	var stopCalls atomic.Int64
	logger := testenv.NewLogger(t)
	failingBackend := testRuntimeBackend{
		backend:      runtimeBackendFlyIO,
		statusResult: RuntimeBackendStatus{Configured: true, IdleSeconds: new(uint64(DefaultWarmTTLSeconds + 60))},
		stopErr:      errors.New("fly delete app blew up"),
		stopCalls:    &stopCalls,
	}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, failingBackend, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())

	_, err = core.ExpireThreadRuntime(t.Context(), projectID, threadID, DefaultWarmTTLSeconds)
	require.Error(t, err, "first attempt with failing Stop must surface the error so Temporal retries")
	require.Equal(t, int64(1), stopCalls.Load())

	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{
		ID:        runtimeID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeStateExpiring, runtime.State, "row must remain in expiring after Stop failure")

	healingBackend := testRuntimeBackend{
		backend:      runtimeBackendFlyIO,
		statusResult: RuntimeBackendStatus{Configured: true, IdleSeconds: new(uint64(DefaultWarmTTLSeconds + 60))},
		stopCalls:    &stopCalls,
	}
	core = NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, healingBackend, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())

	result, err := core.ExpireThreadRuntime(t.Context(), projectID, threadID, DefaultWarmTTLSeconds)
	require.NoError(t, err, "retry must drive the existing expiring row to a terminal state")
	require.True(t, result.Stopped, "retry must report stopped only after Stop actually succeeded")
	require.Equal(t, int64(2), stopCalls.Load(), "retry must invoke Stop a second time, not fall through ErrNoRows")

	runtime, err = assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{
		ID:        runtimeID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStopped, runtime.State)
	require.True(t, runtime.Deleted)
}

// TestServiceCoreReapStuckRuntimesCleansUpStuckExpiring covers the safety net
// for ExpireThreadRuntime activities that exhaust Temporal's retry budget
// after CAS active->expiring. Without reaper coverage the row stays in
// `expiring` indefinitely and blocks the partial unique index
// ReserveAssistantRuntime relies on.
func TestServiceCoreReapStuckRuntimesCleansUpStuckExpiring(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_reap_expiring")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	runtimeID := uuid.New()
	staleUpdatedAt := time.Now().UTC().Add(-(runtimeExpiringReapGrace + time.Minute))
	now := time.Now().UTC()
	err = assistantsrepo.New(conn).CreateAssistantRuntime(t.Context(), assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  runtimeID,
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateExpiring,
		WarmUntil:           pgtype.Timestamptz{},
		LastHeartbeatAt:     pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: staleUpdatedAt, Valid: true},
		EndedAt:             pgtype.Timestamptz{},
		DeletedAt:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapStuckRuntimes(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, result.StaleRuntimesStopped)

	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{
		ID:        runtimeID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStopped, runtime.State, "stuck expiring row must be reclaimed by the reaper")
	require.True(t, runtime.DeletedAt.Valid)
}

// TestServiceCoreReapStuckRuntimesLeavesFreshExpiring verifies the grace
// window: an expiring row that is still within the activity's retry budget
// must NOT be touched by the reaper, otherwise an in-flight retry would race
// the reaper for the same row.
func TestServiceCoreReapStuckRuntimesLeavesFreshExpiring(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_reap_expiring_fresh")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	runtimeID := uuid.New()
	now := time.Now().UTC()
	err = assistantsrepo.New(conn).CreateAssistantRuntime(t.Context(), assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  runtimeID,
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateExpiring,
		WarmUntil:           pgtype.Timestamptz{},
		LastHeartbeatAt:     pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: now, Valid: true},
		EndedAt:             pgtype.Timestamptz{},
		DeletedAt:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapStuckRuntimes(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 0, result.StaleRuntimesStopped, "fresh expiring row must remain so an in-flight retry can complete")

	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{
		ID:        runtimeID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeStateExpiring, runtime.State)
}

func TestServiceCoreReapStuckRuntimesSkipsLiveProcessingLease(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	ctx := t.Context()
	threadKey := assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID}
	runtimeKey := assistantsrepo.GetLatestAssistantRuntimeByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID}

	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(ctx, threadKey)
	require.NoError(t, err)

	err = assistantsrepo.New(conn).CreateAssistantRuntime(ctx, assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  uuid.New(),
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{Time: time.Now().UTC().Add(-2 * time.Minute), Valid: true},
		LastHeartbeatAt:     pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: time.Now().UTC().Add(-10 * time.Minute), Valid: true},
		EndedAt:             pgtype.Timestamptz{},
		DeletedAt:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	err = assistantsrepo.New(conn).SetAssistantThreadEventStatus(ctx, assistantsrepo.SetAssistantThreadEventStatusParams{
		Status:    eventStatusProcessing,
		UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		ID:        event.ID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapStuckRuntimes(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 0, result.StaleRuntimesStopped)
	require.EqualValues(t, 0, result.StaleEventsRequeued)

	runtime, err := assistantsrepo.New(conn).GetLatestAssistantRuntimeByThreadID(ctx, runtimeKey)
	require.NoError(t, err)
	require.False(t, runtime.DeletedAt.Valid)
	require.Equal(t, runtimeStateActive, runtime.State)

	event, err = assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(ctx, threadKey)
	require.NoError(t, err)
	require.Equal(t, eventStatusProcessing, event.Status)
}

func TestServiceCoreReapStuckRuntimesLeavesIdleActiveRuntime(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	ctx := t.Context()
	threadKey := assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID}
	runtimeKey := assistantsrepo.GetLatestAssistantRuntimeByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID}

	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(ctx, threadKey)
	require.NoError(t, err)

	// Long-idle active runtime: warm window long passed, no heartbeat in
	// ages. Idle runtimes keep their VM — only the stale processing lease
	// on the event gets requeued.
	idleSince := time.Now().UTC().Add(-30 * time.Minute)
	err = assistantsrepo.New(conn).CreateAssistantRuntime(ctx, assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  uuid.New(),
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{Time: idleSince, Valid: true},
		LastHeartbeatAt:     pgtype.Timestamptz{Time: idleSince, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: idleSince, Valid: true},
		EndedAt:             pgtype.Timestamptz{},
		DeletedAt:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	err = assistantsrepo.New(conn).SetAssistantThreadEventStatus(ctx, assistantsrepo.SetAssistantThreadEventStatusParams{
		Status:    eventStatusProcessing,
		UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(-(eventProcessingRequeueGrace + time.Minute)), Valid: true},
		ID:        event.ID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapStuckRuntimes(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 0, result.StaleRuntimesStopped, "idle active runtimes must never be reaped")
	require.EqualValues(t, 1, result.StaleEventsRequeued)

	runtime, err := assistantsrepo.New(conn).GetLatestAssistantRuntimeByThreadID(ctx, runtimeKey)
	require.NoError(t, err)
	require.False(t, runtime.DeletedAt.Valid)
	require.Equal(t, runtimeStateActive, runtime.State)

	event, err = assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(ctx, threadKey)
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, event.Status)
}

func TestServiceCoreReapStuckRuntimesStopsLocalRowsWithMissingContainers(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_missing_local_runtime")
	require.NoError(t, err)

	missingProjectID, missingAssistantID, missingThreadID := insertReapableProject(t, conn, "missing-local-runtime")
	missingRuntimeID := insertActiveV2RuntimeRow(
		t,
		conn,
		missingProjectID,
		missingAssistantID,
		missingThreadID,
		runtimeBackendLocal,
		runtimeStateActive,
		`{"container_id":"missing","container_name":"gram-asst-missing","host_port":18081}`,
	)

	presentProjectID, presentAssistantID, presentThreadID := insertReapableProject(t, conn, "present-local-runtime")
	presentRuntimeID := insertActiveV2RuntimeRow(
		t,
		conn,
		presentProjectID,
		presentAssistantID,
		presentThreadID,
		runtimeBackendLocal,
		runtimeStateActive,
		`{"container_id":"present","container_name":"gram-asst-present","host_port":18082}`,
	)

	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, 18082)
	backend := newTestLocalBackend(t, engine, nil)
	presentRecord := assistantRuntimeRecord{
		ID:                  presentRuntimeID,
		AssistantThreadID:   uuid.Nil,
		AssistantID:         presentAssistantID,
		ProjectID:           presentProjectID,
		Backend:             runtimeBackendLocal,
		BackendMetadataJSON: []byte(`{"container_id":"present"}`),
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{},
	}
	presentName := localContainerName(presentRecord)
	engine.containers[presentName] = &fakeContainer{
		id:      "present",
		imageID: testLocalImageID,
		running: false,
		spec:    backend.containerSpec(presentRecord, presentName),
		starts:  0,
	}

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())
	result, err := core.ReapStuckRuntimes(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, result.StaleRuntimesStopped)
	require.Contains(t, result.AffectedAssistantIDs, missingAssistantID)
	require.NotContains(t, result.AffectedAssistantIDs, presentAssistantID)

	missingRuntime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: missingRuntimeID, ProjectID: missingProjectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStopped, missingRuntime.State)
	require.True(t, missingRuntime.DeletedAt.Valid)

	presentRuntime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: presentRuntimeID, ProjectID: presentProjectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateActive, presentRuntime.State)
	require.False(t, presentRuntime.DeletedAt.Valid)
}

func insertAssistantFixture(t *testing.T, conn *pgxpool.Pool) (projectID, assistantID, chatID, threadID uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "project",
		OrganizationID: "org-test",
	})
	require.NoError(t, err)

	assistant, err := assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       proj.ID,
		OrganizationID:  "org-test",
		CreatedByUserID: pgtype.Text{},
		Name:            "Assistant",
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          StatusActive,
	})
	require.NoError(t, err)

	chatID = uuid.New()
	err = assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      proj.ID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)

	threadID, err = assistantsrepo.New(conn).UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     proj.ID,
		CorrelationID: "corr-1",
		ChatID:        chatID,
		SourceKind:    sourceKindSlack,
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)

	_, err = assistantsrepo.New(conn).InsertAssistantThreadEvent(ctx, assistantsrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     threadID,
		AssistantID:           assistant.ID,
		ProjectID:             proj.ID,
		TriggerInstanceID:     uuid.NullUUID{Valid: false},
		EventID:               "evt-1",
		CorrelationID:         "corr-1",
		Status:                eventStatusPending,
		NormalizedPayloadJson: []byte(`{"text":"hello"}`),
		SourcePayloadJson:     []byte("{}"),
	})
	require.NoError(t, err)

	return proj.ID, assistant.ID, chatID, threadID
}

func TestServiceCoreLoadChatHistoryReplaysToolTurns(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_load_history")
	require.NoError(t, err)

	ctx := t.Context()
	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "project",
		OrganizationID: "org-test",
	})
	require.NoError(t, err)
	projectID := proj.ID

	chatID := uuid.New()
	err = assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      projectID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)

	toolCallsJSON := `[{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"oslo\"}"}}]`
	rows := []struct {
		role       string
		content    string
		toolCalls  []byte
		toolCallID pgtype.Text
	}{
		{role: "system", content: "You are Gram."},
		{role: "user", content: "what's the weather in oslo?"},
		{role: "assistant", content: "", toolCalls: []byte(toolCallsJSON)},
		{role: "tool", content: `{"temp":"cold"}`, toolCallID: pgtype.Text{String: "call_abc", Valid: true}},
		{role: "assistant", content: "It's cold."},
		{role: "user", content: "thanks"},
	}
	for _, r := range rows {
		err = chatrepo.New(conn).CreateChatMessageWithToolCalls(ctx, chatrepo.CreateChatMessageWithToolCallsParams{
			ChatID:     chatID,
			ProjectID:  uuid.NullUUID{UUID: projectID, Valid: true},
			Role:       r.role,
			Content:    r.content,
			ToolCalls:  r.toolCalls,
			ToolCallID: r.toolCallID,
		})
		require.NoError(t, err)
	}

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	history, err := core.loadChatHistory(t.Context(), chatID, projectID)
	require.NoError(t, err)

	require.Len(t, history, 5, "system row should be dropped; user/assistant/tool rows replayed in order")

	require.Equal(t, "user", history[0].Role)
	require.Equal(t, "what's the weather in oslo?", history[0].Content)
	require.Empty(t, history[0].ToolCalls)
	require.Empty(t, history[0].ToolCallID)

	require.Equal(t, "assistant", history[1].Role)
	require.Empty(t, history[1].Content)
	require.Len(t, history[1].ToolCalls, 1)
	require.Equal(t, "call_abc", history[1].ToolCalls[0].ID)
	require.Equal(t, "get_weather", history[1].ToolCalls[0].Name)
	require.JSONEq(t, `{"city":"oslo"}`, history[1].ToolCalls[0].Arguments)

	require.Equal(t, "tool", history[2].Role)
	require.JSONEq(t, `{"temp":"cold"}`, history[2].Content)
	require.Equal(t, "call_abc", history[2].ToolCallID)
	require.Empty(t, history[2].ToolCalls)

	require.Equal(t, "assistant", history[3].Role)
	require.Equal(t, "It's cold.", history[3].Content)
	require.Empty(t, history[3].ToolCalls)

	require.Equal(t, "user", history[4].Role)
	require.Equal(t, "thanks", history[4].Content)
}

func TestServiceCoreLoadChatHistoryReturnsOnlyLatestGeneration(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_load_history_gens")
	require.NoError(t, err)

	ctx := t.Context()
	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "project",
		OrganizationID: "org-test",
	})
	require.NoError(t, err)
	projectID := proj.ID

	chatID := uuid.New()
	err = assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      projectID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)

	rows := []struct {
		gen     int32
		role    string
		content string
	}{
		{gen: 0, role: "user", content: "gen-0-user"},
		{gen: 0, role: "assistant", content: "gen-0-asst"},
		{gen: 1, role: "user", content: "gen-1-user"},
		{gen: 1, role: "assistant", content: "gen-1-asst"},
		{gen: 2, role: "user", content: "gen-2-user-a"},
		{gen: 2, role: "assistant", content: "gen-2-asst"},
		{gen: 2, role: "user", content: "gen-2-user-b"},
	}
	for _, r := range rows {
		err = chatrepo.New(conn).CreateChatMessageWithToolCalls(ctx, chatrepo.CreateChatMessageWithToolCallsParams{
			ChatID:     chatID,
			ProjectID:  uuid.NullUUID{UUID: projectID, Valid: true},
			Role:       r.role,
			Content:    r.content,
			ToolCalls:  nil,
			ToolCallID: pgtype.Text{},
			Generation: r.gen,
		})
		require.NoError(t, err)
	}

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	history, err := core.loadChatHistory(t.Context(), chatID, projectID)
	require.NoError(t, err)

	require.Len(t, history, 3, "only gen 2 rows make it into the replay")
	require.Equal(t, "gen-2-user-a", history[0].Content)
	require.Equal(t, "gen-2-asst", history[1].Content)
	require.Equal(t, "gen-2-user-b", history[2].Content)
}

func TestServiceCoreLoadChatHistoryFailsWhenToolRowMissingCallID(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_load_history_bad")
	require.NoError(t, err)

	ctx := t.Context()
	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "project",
		OrganizationID: "org-test",
	})
	require.NoError(t, err)
	projectID := proj.ID

	chatID := uuid.New()
	err = assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      projectID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)

	err = chatrepo.New(conn).CreateChatMessageWithToolCalls(ctx, chatrepo.CreateChatMessageWithToolCallsParams{
		ChatID:     chatID,
		ProjectID:  uuid.NullUUID{UUID: projectID, Valid: true},
		Role:       "tool",
		Content:    "orphan result",
		ToolCalls:  nil,
		ToolCallID: pgtype.Text{},
	})
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	_, err = core.loadChatHistory(ctx, chatID, projectID)
	require.ErrorContains(t, err, "tool chat row missing tool_call_id")
}

func TestServiceCoreProcessThreadEventsCompletesEvent(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_process_ok")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	logger := testenv.NewLogger(t)
	tokens := assistanttokens.New("test-jwt-secret", conn, nil)
	runTurnMCP := &atomic.Pointer[[]runtimeMCPServer]{}
	runTurnPrompt := &atomic.Pointer[string]{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil, runTurnMCPServers: runTurnMCP, runTurnPrompt: runTurnPrompt}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, tokens, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())

	admitted, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)
	require.Equal(t, projectID, admitted.ProjectID)

	result, err := core.ProcessThreadEvents(t.Context(), projectID, threadID)
	require.NoError(t, err)
	require.True(t, result.ProcessedAnyEvent)
	require.True(t, result.RuntimeActive)
	require.False(t, result.RetryAdmission)

	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, eventStatusCompleted, event.Status)

	runtime, err := assistantsrepo.New(conn).GetActiveAssistantRuntimeByThreadID(t.Context(), assistantsrepo.GetActiveAssistantRuntimeByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateActive, runtime.State)

	// Resolved MCP set must flow through processEventTurn → RunTurn each
	// turn — that is the channel through which assistant toolset edits
	// reach a live runner without recycling the VM. The bare fixture has
	// no user toolsets, so we assert on the always-present implicit
	// platform entry.
	captured := runTurnMCP.Load()
	require.NotNil(t, captured, "RunTurn must receive mcp_servers")
	require.NotEmpty(t, *captured)
	require.Equal(t, "_p-"+platformtools.AssistantsPlatformToolsetSlug, (*captured)[0].ID)
	require.NotContains(t, *runTurnPrompt.Load(), "<assistant-environment-change>", "a NULL baseline establishes itself without a notice")

	persisted, err := assistantsrepo.New(conn).InitThreadSkillSnapshot(t.Context(), assistantsrepo.InitThreadSkillSnapshotParams{
		Candidate: []byte(`{"version":1,"skills":[{"skill_id":"00000000-0000-0000-0000-000000000001","name":"unexpected","description":"","resolved_version_id":"00000000-0000-0000-0000-000000000002"}]}`),
		ThreadID:  threadID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"version":1,"skills":[]}`, string(persisted))
}

func TestAssistantEventSkillSnapshotClaimCompletionAndCASMiss(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_skill_snapshot_cas")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	queries := assistantsrepo.New(conn)

	baseline, err := marshalAssistantSkillSetSnapshot(newAssistantSkillSetSnapshot([]assistantSkillRow{{SkillID: uuid.New(), Name: "baseline", ResolvedVersionID: uuid.New(), Description: "old"}}))
	require.NoError(t, err)
	current, err := marshalAssistantSkillSetSnapshot(newAssistantSkillSetSnapshot([]assistantSkillRow{{SkillID: uuid.New(), Name: "current", ResolvedVersionID: uuid.New(), Description: "current"}}))
	require.NoError(t, err)
	newer, err := marshalAssistantSkillSetSnapshot(newAssistantSkillSetSnapshot([]assistantSkillRow{{SkillID: uuid.New(), Name: "newer", ResolvedVersionID: uuid.New(), Description: "newer"}}))
	require.NoError(t, err)
	_, err = queries.InitThreadSkillSnapshot(t.Context(), assistantsrepo.InitThreadSkillSnapshotParams{Candidate: baseline, ThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)

	first, err := queries.ClaimNextPendingEvent(t.Context(), assistantsrepo.ClaimNextPendingEventParams{
		ProcessingStatus: eventStatusProcessing, ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending,
	})
	require.NoError(t, err)
	require.JSONEq(t, string(baseline), string(first.SkillSetSnapshot))

	secondID := uuid.New()
	_, err = queries.InsertAssistantThreadEvent(t.Context(), assistantsrepo.InsertAssistantThreadEventParams{
		AssistantThreadID: threadID, AssistantID: assistantID, ProjectID: projectID, TriggerInstanceID: uuid.NullUUID{},
		EventID: secondID.String(), CorrelationID: "corr-1", Status: eventStatusPending,
		NormalizedPayloadJson: []byte(`{"text":"second"}`), SourcePayloadJson: []byte(`{}`),
	})
	require.NoError(t, err)
	second, err := queries.ClaimNextPendingEvent(t.Context(), assistantsrepo.ClaimNextPendingEventParams{
		ProcessingStatus: eventStatusProcessing, ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending,
	})
	require.NoError(t, err)
	require.JSONEq(t, string(baseline), string(second.SkillSetSnapshot))

	err = queries.CompleteAssistantThreadEventAndAdvanceSkillSnapshot(t.Context(), assistantsrepo.CompleteAssistantThreadEventAndAdvanceSkillSnapshotParams{
		CurrentSnapshot: current, ProjectID: projectID, ClaimedSnapshot: first.SkillSetSnapshot, AllowAdvance: true, CompletedStatus: eventStatusCompleted, EventID: first.ID,
	})
	require.NoError(t, err)
	err = queries.CompleteAssistantThreadEventAndAdvanceSkillSnapshot(t.Context(), assistantsrepo.CompleteAssistantThreadEventAndAdvanceSkillSnapshotParams{
		CurrentSnapshot: newer, ProjectID: projectID, ClaimedSnapshot: second.SkillSetSnapshot, AllowAdvance: true, CompletedStatus: eventStatusCompleted, EventID: second.ID,
	})
	require.NoError(t, err)

	secondEvent, err := queries.GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, eventStatusCompleted, secondEvent.Status, "CAS miss must still complete the event")
	persisted, err := queries.InitThreadSkillSnapshot(t.Context(), assistantsrepo.InitThreadSkillSnapshotParams{Candidate: baseline, ThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.JSONEq(t, string(current), string(persisted), "CAS miss must preserve the newer snapshot")
}

func TestAssistantEventSkillSnapshotRetryCompletesWithoutAdvancingBaseline(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_skill_snapshot_retry")
	require.NoError(t, err)
	projectID, _, _, threadID := insertAssistantFixture(t, conn)
	queries := assistantsrepo.New(conn)
	baseline, err := marshalAssistantSkillSetSnapshot(newAssistantSkillSetSnapshot([]assistantSkillRow{{SkillID: uuid.New(), Name: "baseline", ResolvedVersionID: uuid.New(), Description: "old"}}))
	require.NoError(t, err)
	current, err := marshalAssistantSkillSetSnapshot(newAssistantSkillSetSnapshot([]assistantSkillRow{{SkillID: uuid.New(), Name: "changed", ResolvedVersionID: uuid.New(), Description: "new"}}))
	require.NoError(t, err)
	_, err = queries.InitThreadSkillSnapshot(t.Context(), assistantsrepo.InitThreadSkillSnapshotParams{Candidate: baseline, ThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)

	first, err := queries.ClaimNextPendingEvent(t.Context(), assistantsrepo.ClaimNextPendingEventParams{
		ProcessingStatus: eventStatusProcessing, ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, first.Attempts)
	_, err = queries.RequeueStaleAssistantEvents(t.Context(), assistantsrepo.RequeueStaleAssistantEventsParams{
		PendingStatus: eventStatusPending, ProcessingStatus: eventStatusProcessing,
		UpdatedBefore: pgtype.Timestamptz{Time: time.Now().UTC().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)
	retry, err := queries.ClaimNextPendingEvent(t.Context(), assistantsrepo.ClaimNextPendingEventParams{
		ProcessingStatus: eventStatusProcessing, ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, retry.Attempts)
	require.JSONEq(t, string(baseline), string(retry.SkillSetSnapshot))

	err = queries.CompleteAssistantThreadEventAndAdvanceSkillSnapshot(t.Context(), assistantsrepo.CompleteAssistantThreadEventAndAdvanceSkillSnapshotParams{
		CurrentSnapshot: current, ProjectID: projectID, ClaimedSnapshot: retry.SkillSetSnapshot, AllowAdvance: false,
		CompletedStatus: eventStatusCompleted, EventID: retry.ID,
	})
	require.NoError(t, err)
	event, err := queries.GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, eventStatusCompleted, event.Status)
	persisted, err := queries.InitThreadSkillSnapshot(t.Context(), assistantsrepo.InitThreadSkillSnapshotParams{Candidate: current, ThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.JSONEq(t, string(baseline), string(persisted))
}

func TestAssistantEventSkillSnapshotNullBaselineInitializesWhenAdvanceDisallowed(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_skill_snapshot_null_retry")
	require.NoError(t, err)
	projectID, _, _, threadID := insertAssistantFixture(t, conn)
	queries := assistantsrepo.New(conn)
	current, err := marshalAssistantSkillSetSnapshot(newAssistantSkillSetSnapshot(nil))
	require.NoError(t, err)
	claimed, err := queries.ClaimNextPendingEvent(t.Context(), assistantsrepo.ClaimNextPendingEventParams{
		ProcessingStatus: eventStatusProcessing, ProjectID: projectID, ThreadID: threadID, PendingStatus: eventStatusPending,
	})
	require.NoError(t, err)
	require.Nil(t, claimed.SkillSetSnapshot)
	err = queries.CompleteAssistantThreadEventAndAdvanceSkillSnapshot(t.Context(), assistantsrepo.CompleteAssistantThreadEventAndAdvanceSkillSnapshotParams{
		CurrentSnapshot: current, ProjectID: projectID, ClaimedSnapshot: nil, AllowAdvance: false,
		CompletedStatus: eventStatusCompleted, EventID: claimed.ID,
	})
	require.NoError(t, err)
	persisted, err := queries.InitThreadSkillSnapshot(t.Context(), assistantsrepo.InitThreadSkillSnapshotParams{Candidate: []byte(`{"version":1,"skills":[]}`), ThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.JSONEq(t, string(current), string(persisted))
}

func TestProcessEventTurnNestsSkillNoticeForRegularAndMCPAuthPrompts(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_skill_snapshot_prompts")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	logger := testenv.NewLogger(t)
	prompt := &atomic.Pointer[string]{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnPrompt: prompt}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, assistanttokens.New("test-jwt-secret", conn, nil), nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	assistant, err := core.GetAssistant(t.Context(), projectID, assistantID)
	require.NoError(t, err)
	thread := assistantThreadRecord{ID: threadID, AssistantID: assistantID, ProjectID: projectID, CorrelationID: "corr-1", SourceKind: sourceKindSlack}
	runtime := assistantRuntimeRecord{ID: uuid.New(), AssistantID: assistantID, ProjectID: projectID, Backend: runtimeBackendFlyIO}
	baseline, err := marshalAssistantSkillSetSnapshot(newAssistantSkillSetSnapshot([]assistantSkillRow{{SkillID: uuid.New(), Name: "removed", ResolvedVersionID: uuid.New(), Description: "old"}}))
	require.NoError(t, err)

	regular := assistantThreadEventRecord{ID: uuid.New(), EventID: "regular", NormalizedPayloadJSON: []byte(`{"event_type":"message","text":"hello"}`), SkillSetSnapshot: baseline}
	current, err := core.processEventTurn(t.Context(), thread, assistant, runtime, regular)
	require.NoError(t, err)
	require.NotNil(t, current)
	require.Contains(t, *prompt.Load(), "<assistant-environment-change>")
	require.Less(t, strings.Index(*prompt.Load(), "<assistant-environment-change>"), strings.Index(*prompt.Load(), "</message-context>"))

	mcpAuth := assistantThreadEventRecord{ID: uuid.New(), EventID: "mcp-auth", NormalizedPayloadJSON: []byte(`{"gram_event_kind":"assistant_mcp_auth","status":"success"}`), SkillSetSnapshot: baseline}
	_, err = core.processEventTurn(t.Context(), thread, assistant, runtime, mcpAuth)
	require.NoError(t, err)
	require.Contains(t, *prompt.Load(), "<assistant-environment-change>")
	require.Less(t, strings.Index(*prompt.Load(), "<assistant-environment-change>"), strings.Index(*prompt.Load(), "</message-context>"))

	regular.SkillSetSnapshot = current
	want, err := slackAdapter{}.DecodeTurn(regular)
	require.NoError(t, err)
	_, err = core.processEventTurn(t.Context(), thread, assistant, runtime, regular)
	require.NoError(t, err)
	require.Equal(t, want, *prompt.Load(), "no delta must leave the adapter prompt byte-identical")

	regular.SkillSetSnapshot = nil
	_, err = core.processEventTurn(t.Context(), thread, assistant, runtime, regular)
	require.NoError(t, err)
	require.Equal(t, want, *prompt.Load(), "a NULL baseline must not emit a notice")
}

func TestServiceCoreProcessThreadEventsRequeuesOnTurnFailure(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_process_fail")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	baseline, err := marshalAssistantSkillSetSnapshot(newAssistantSkillSetSnapshot([]assistantSkillRow{{SkillID: uuid.New(), Name: "baseline", ResolvedVersionID: uuid.New(), Description: "old"}}))
	require.NoError(t, err)
	_, err = assistantsrepo.New(conn).InitThreadSkillSnapshot(t.Context(), assistantsrepo.InitThreadSkillSnapshotParams{Candidate: baseline, ThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tokens := assistanttokens.New("test-jwt-secret", conn, nil)
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: errors.New("runtime RunTurn blew up")}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, tokens, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())

	admitted, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)
	require.Equal(t, projectID, admitted.ProjectID)

	result, err := core.ProcessThreadEvents(t.Context(), projectID, threadID)
	require.NoError(t, err)
	require.False(t, result.ProcessedAnyEvent)
	require.True(t, result.RetryAdmission)

	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, event.Status)
	require.EqualValues(t, 1, event.Attempts)
	require.True(t, event.LastError.Valid)
	require.Contains(t, event.LastError.String, "runtime RunTurn blew up")
	persisted, err := assistantsrepo.New(conn).InitThreadSkillSnapshot(t.Context(), assistantsrepo.InitThreadSkillSnapshotParams{
		Candidate: []byte(`{"version":1,"skills":[]}`), ThreadID: threadID, ProjectID: projectID,
	})
	require.NoError(t, err)
	require.JSONEq(t, string(baseline), string(persisted), "reset events must not advance the snapshot")
}

func TestServiceCoreProcessThreadEventsMarksRuntimeFailedOnUnhealthyTurn(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_unhealthy_turn")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	var stopCalls atomic.Int64
	logger := testenv.NewLogger(t)
	tokens := assistanttokens.New("test-jwt-secret", conn, nil)
	backend := testRuntimeBackend{
		backend:    runtimeBackendFlyIO,
		runTurnErr: ErrRuntimeUnhealthy,
		stopCalls:  &stopCalls,
	}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, tokens, mustParseURLForServiceTest(t, "https://gram.example.com"), telemetry.NewStub(logger), nil, newTestAuditLogger())

	admitted, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)

	result, err := core.ProcessThreadEvents(t.Context(), projectID, threadID)
	require.NoError(t, err)
	require.True(t, result.RetryAdmission)
	require.False(t, result.RuntimeActive)
	require.False(t, result.ProcessedAnyEvent)
	require.Equal(t, int64(1), stopCalls.Load(), "unhealthy turn must tear the VM down")

	runtime, err := assistantsrepo.New(conn).GetLatestAssistantRuntimeByThreadID(t.Context(), assistantsrepo.GetLatestAssistantRuntimeByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateFailed, runtime.State, "unhealthy turn must mark runtime failed so the admission backoff applies")
	require.True(t, runtime.Deleted)

	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, eventStatusProcessing, event.Status, "unhealthy turn leaves the event in processing for the reaper")
}

// An event that has already torn down its runtime maxRuntimeTeardowns times is
// failed terminally instead of being re-admitted forever — the bound that stops
// a deterministic error misclassified as unhealthy from looping.
func TestServiceCoreProcessThreadEventsCapsRuntimeTeardowns(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_teardown_cap")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	var stopCalls atomic.Int64
	logger := testenv.NewLogger(t)
	tokens := assistanttokens.New("test-jwt-secret", conn, nil)
	backend := testRuntimeBackend{
		backend:    runtimeBackendFlyIO,
		runTurnErr: ErrRuntimeUnhealthy,
		stopCalls:  &stopCalls,
	}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, tokens, mustParseURLForServiceTest(t, "https://gram.example.com"), telemetry.NewStub(logger), nil, newTestAuditLogger())

	admitted, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)

	// Drive attempts up to maxRuntimeTeardowns-1 the way the real teardown
	// cycle does — claim (which increments attempts) then requeue back to
	// pending — so the next claim inside ProcessThreadEvents lands on the
	// ceiling.
	repo := assistantsrepo.New(conn)
	requeueBefore := pgtype.Timestamptz{Time: time.Now().UTC().Add(time.Hour), Valid: true}
	for range maxRuntimeTeardowns - 1 {
		_, err = repo.ClaimNextPendingEvent(t.Context(), assistantsrepo.ClaimNextPendingEventParams{
			ProcessingStatus: eventStatusProcessing,
			ProjectID:        projectID,
			ThreadID:         threadID,
			PendingStatus:    eventStatusPending,
		})
		require.NoError(t, err)
		_, err = repo.RequeueStaleAssistantEvents(t.Context(), assistantsrepo.RequeueStaleAssistantEventsParams{
			PendingStatus:    eventStatusPending,
			ProcessingStatus: eventStatusProcessing,
			UpdatedBefore:    requeueBefore,
		})
		require.NoError(t, err)
	}

	result, err := core.ProcessThreadEvents(t.Context(), projectID, threadID)
	require.NoError(t, err)
	require.True(t, result.RetryAdmission, "thread is re-admitted so any other pending events run on a fresh runtime")
	require.False(t, result.RuntimeActive)
	require.Equal(t, int64(1), stopCalls.Load(), "the runtime is still torn down on the final attempt")

	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, eventStatusFailed, event.Status, "the poisoned event itself is failed terminally, not re-admitted")
	require.True(t, event.LastError.Valid)
	require.Contains(t, event.LastError.String, "exceeded 10 runtime teardowns")
}

// insertReapableProject inserts an isolated project + assistant + thread so a
// single test can host several independent fixtures without colliding on the
// project slug or the per-thread-active runtime unique index.
func insertReapableProject(t *testing.T, conn *pgxpool.Pool, slug string) (projectID, assistantID, threadID uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: "org-test",
	})
	require.NoError(t, err)

	assistant, err := assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       proj.ID,
		OrganizationID:  "org-test",
		CreatedByUserID: pgtype.Text{},
		Name:            "Assistant",
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          StatusActive,
	})
	require.NoError(t, err)

	chatID := uuid.New()
	err = assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      proj.ID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)

	threadID, err = assistantsrepo.New(conn).UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     proj.ID,
		CorrelationID: "corr-" + slug,
		ChatID:        chatID,
		SourceKind:    sourceKindSlack,
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)

	return proj.ID, assistant.ID, threadID
}

// insertReapableRuntimeRow seeds an assistant_runtimes row with non-empty
// backend metadata so the reap queries can find it. ended_at is set so
// multiple rows can coexist on the same thread without colliding on the
// active-runtime unique index.
func insertReapableRuntimeRow(
	t *testing.T,
	conn *pgxpool.Pool,
	projectID, assistantID, threadID uuid.UUID,
	state string,
	appName string,
	updatedAt time.Time,
) uuid.UUID {
	t.Helper()
	return insertReapableRuntimeRowWithMetadata(t, conn, projectID, assistantID, threadID, state, fmt.Sprintf(`{"app_name":%q}`, appName), updatedAt)
}

// insertReapableRuntimeRowWithMetadata variant lets a test seed a full
// flyRuntimeMetadata blob so per-thread reap assertions can verify which
// fields survive.
func insertReapableRuntimeRowWithMetadata(
	t *testing.T,
	conn *pgxpool.Pool,
	projectID, assistantID, threadID uuid.UUID,
	state string,
	metadataJSON string,
	updatedAt time.Time,
) uuid.UUID {
	t.Helper()

	runtimeID := uuid.New()
	err := assistantsrepo.New(conn).CreateAssistantRuntime(t.Context(), assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  runtimeID,
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(metadataJSON),
		State:               state,
		WarmUntil:           pgtype.Timestamptz{},
		LastHeartbeatAt:     pgtype.Timestamptz{},
		UpdatedAt:           pgtype.Timestamptz{Time: updatedAt, Valid: true},
		EndedAt:             endedAtFor(state, updatedAt),
		DeletedAt:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	return runtimeID
}

func endedAtFor(state string, updatedAt time.Time) pgtype.Timestamptz {
	switch state {
	case runtimeStateActive, runtimeStateStarting:
		return pgtype.Timestamptz{}
	default:
		return pgtype.Timestamptz{Time: updatedAt, Valid: true}
	}
}

func TestServiceCoreDeleteAssistantReapsRuntimes(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "delete_assistant_reaps")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	runtimeID := insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-delete-target", time.Now().UTC().Add(-time.Hour))

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	require.NoError(t, core.DeleteAssistant(t.Context(), projectID, assistantID, urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"), nil))
	require.EqualValues(t, 1, reapCalls.Load())

	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{
		ID:        runtimeID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeStateReaped, runtime.State)
	require.JSONEq(t, `{}`, string(runtime.BackendMetadataJson))

	assistant, err := assistantsrepo.New(conn).GetAssistantIgnoringDeleted(t.Context(), assistantsrepo.GetAssistantIgnoringDeletedParams{
		AssistantID: assistantID,
		ProjectID:   projectID,
	})
	require.NoError(t, err)
	require.True(t, assistant.DeletedAt.Valid)
}

func TestServiceCoreDeleteAssistantSucceedsEvenWhenReapErrors(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "delete_assistant_reap_error")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-flaky", time.Now().UTC().Add(-time.Hour))

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{
		backend:   runtimeBackendFlyIO,
		reapCalls: reapCalls,
		reapErr:   errors.New("fly api 503"),
	}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	require.NoError(t, core.DeleteAssistant(t.Context(), projectID, assistantID, urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"), nil))
	require.EqualValues(t, 1, reapCalls.Load())

	// Soft-delete still landed; the janitor will retry the orphan later.
	assistant, err := assistantsrepo.New(conn).GetAssistantIgnoringDeleted(t.Context(), assistantsrepo.GetAssistantIgnoringDeletedParams{
		AssistantID: assistantID,
		ProjectID:   projectID,
	})
	require.NoError(t, err)
	require.True(t, assistant.DeletedAt.Valid)
}

func TestServiceCoreReapAssistantRuntimesCallsBackendAndClearsMetadata(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_assistant_runtimes")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	runtimeID := insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-orphan", time.Now().UTC().Add(-time.Hour))

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapAssistantRuntimes(t.Context(), projectID, assistantID)
	require.NoError(t, err)
	require.Equal(t, 1, result.Reaped)
	require.Equal(t, 0, result.Errors)
	require.EqualValues(t, 1, reapCalls.Load())

	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{
		ID:        runtimeID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeStateReaped, runtime.State)
	require.JSONEq(t, `{}`, string(runtime.BackendMetadataJson))
}

func TestServiceCoreReapAssistantRuntimesSkipsRowsWithoutMetadata(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_assistant_runtimes_skip")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	err = assistantsrepo.New(conn).CreateAssistantRuntime(t.Context(), assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  uuid.New(),
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateStopped,
		WarmUntil:           pgtype.Timestamptz{},
		LastHeartbeatAt:     pgtype.Timestamptz{},
		UpdatedAt:           pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		EndedAt:             pgtype.Timestamptz{},
		DeletedAt:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapAssistantRuntimes(t.Context(), projectID, assistantID)
	require.NoError(t, err)
	require.Equal(t, 0, result.Reaped)
	require.EqualValues(t, 0, reapCalls.Load())
}

// insertActiveV2RuntimeRow seeds an active v2 runtime row carrying backend
// metadata, mirroring what a completed admit leaves behind, and returns the
// row id.
func insertActiveV2RuntimeRow(
	t *testing.T,
	conn *pgxpool.Pool,
	projectID, assistantID, threadID uuid.UUID,
	backend string,
	state string,
	metadata string,
) uuid.UUID {
	t.Helper()

	require.NoError(t, assistantsrepo.New(conn).ReserveAssistantRuntimeV2(t.Context(), assistantsrepo.ReserveAssistantRuntimeV2Params{
		AssistantThreadID: threadID,
		AssistantID:       assistantID,
		ProjectID:         projectID,
		Backend:           backend,
		State:             state,
	}))
	row, err := assistantsrepo.New(conn).GetAssistantRuntimeV2(t.Context(), assistantsrepo.GetAssistantRuntimeV2Params{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	require.NoError(t, err)
	require.NoError(t, assistantsrepo.New(conn).UpdateAssistantRuntimeMetadata(t.Context(), assistantsrepo.UpdateAssistantRuntimeMetadataParams{
		BackendMetadataJson: []byte(metadata),
		RuntimeID:           row.ID,
		ProjectID:           projectID,
	}))
	return row.ID
}

func TestServiceCoreRecycleActiveRuntimeImagesRecyclesAndPersists(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "recycle_runtime_images")
	require.NoError(t, err)

	// insertReapableProject seeds no thread events, so the runtime reads as
	// fully idle to the sweep's in-flight guard.
	projectID, assistantID, threadID := insertReapableProject(t, conn, "recycle-persist")
	runtimeID := insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendFlyIO, runtimeStateActive, `{"app_name":"gram-asst-stale","machine_id":"m-1"}`)

	recycledMetadata := `{"app_name":"gram-asst-stale","machine_id":"m-1","last_boot_id":"boot-2"}`
	recycleCalls := &atomic.Int64{}
	backend := testRuntimeBackend{
		backend:      runtimeBackendFlyIO,
		recycleCalls: recycleCalls,
		recycleResult: RuntimeBackendRecycleResult{
			Recycled:            true,
			BackendMetadataJSON: []byte(recycledMetadata),
		},
	}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.RecycleActiveRuntimeImages(t.Context(), RecycleAssistantRuntimeImagesParams{OnRowProcessed: nil})
	require.NoError(t, err)
	require.Equal(t, 1, result.Recycled)
	require.Equal(t, 0, result.Skipped)
	require.Equal(t, 0, result.Errors)
	require.EqualValues(t, 1, recycleCalls.Load())

	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
	require.NoError(t, err)
	require.JSONEq(t, recycledMetadata, string(runtime.BackendMetadataJson))
}

func TestServiceCoreRecycleActiveRuntimeImagesSweepsOnlyActiveV2Rows(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "recycle_runtime_images_filter")
	require.NoError(t, err)

	// Candidate: active v2 row with metadata. The fake reports no recycle
	// (image already current), so it must count as skipped.
	activeProj, activeAssistant, activeThread := insertReapableProject(t, conn, "recycle-active")
	insertActiveV2RuntimeRow(t, conn, activeProj, activeAssistant, activeThread, runtimeBackendFlyIO, runtimeStateActive, `{"app_name":"gram-asst-current","machine_id":"m-1"}`)

	// Non-candidates: a starting v2 row (mid-boot, already on the new
	// image) and a v1 row (per-thread runtimes recycle on admission).
	startingProj, startingAssistant, startingThread := insertReapableProject(t, conn, "recycle-starting")
	insertActiveV2RuntimeRow(t, conn, startingProj, startingAssistant, startingThread, runtimeBackendFlyIO, runtimeStateStarting, `{"app_name":"gram-asst-booting","machine_id":"m-2"}`)
	v1Proj, v1Assistant, v1Thread := insertReapableProject(t, conn, "recycle-v1")
	insertReapableRuntimeRow(t, conn, v1Proj, v1Assistant, v1Thread, runtimeStateActive, "gram-asst-v1", time.Now().UTC())

	recycleCalls := &atomic.Int64{}
	backend := testRuntimeBackend{
		backend:       runtimeBackendFlyIO,
		recycleCalls:  recycleCalls,
		recycleResult: RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil},
	}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.RecycleActiveRuntimeImages(t.Context(), RecycleAssistantRuntimeImagesParams{OnRowProcessed: nil})
	require.NoError(t, err)
	require.Equal(t, 0, result.Recycled)
	require.Equal(t, 1, result.Skipped)
	require.Equal(t, 0, result.Errors)
	require.EqualValues(t, 1, recycleCalls.Load())
}

func TestServiceCoreRecycleActiveRuntimeImagesSkipsAssistantsWithInFlightEvents(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "recycle_runtime_images_in_flight")
	require.NoError(t, err)

	// insertAssistantFixture seeds a pending thread event, which is exactly
	// the in-flight signal the sweep must respect.
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendFlyIO, runtimeStateActive, `{"app_name":"gram-asst-busy","machine_id":"m-1"}`)

	recycleCalls := &atomic.Int64{}
	backend := testRuntimeBackend{
		backend:       runtimeBackendFlyIO,
		recycleCalls:  recycleCalls,
		recycleResult: RuntimeBackendRecycleResult{Recycled: true, BackendMetadataJSON: []byte(`{}`)},
	}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.RecycleActiveRuntimeImages(t.Context(), RecycleAssistantRuntimeImagesParams{OnRowProcessed: nil})
	require.NoError(t, err)
	require.Equal(t, 0, result.Recycled)
	require.Equal(t, 1, result.Skipped)
	require.Equal(t, 0, result.Errors)
	require.EqualValues(t, 0, recycleCalls.Load())
}

func TestServiceCoreRecycleActiveRuntimeImagesIgnoresDeletedAssistants(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "recycle_runtime_images_deleted_assistant")
	require.NoError(t, err)

	// An active row orphaned by a failed best-effort reap on assistant
	// delete belongs to the janitor; recycling it would bump updated_at and
	// postpone the inactivity-based collection.
	projectID, assistantID, threadID := insertReapableProject(t, conn, "recycle-deleted-assistant")
	insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendFlyIO, runtimeStateActive, `{"app_name":"gram-asst-orphan","machine_id":"m-1"}`)
	require.NoError(t, assistantsrepo.New(conn).DeleteAssistant(t.Context(), assistantsrepo.DeleteAssistantParams{
		AssistantID: assistantID,
		ProjectID:   projectID,
	}))

	recycleCalls := &atomic.Int64{}
	backend := testRuntimeBackend{
		backend:       runtimeBackendFlyIO,
		recycleCalls:  recycleCalls,
		recycleResult: RuntimeBackendRecycleResult{Recycled: true, BackendMetadataJSON: []byte(`{}`)},
	}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.RecycleActiveRuntimeImages(t.Context(), RecycleAssistantRuntimeImagesParams{OnRowProcessed: nil})
	require.NoError(t, err)
	require.Equal(t, 0, result.Recycled)
	require.Equal(t, 0, result.Skipped)
	require.Equal(t, 0, result.Errors)
	require.EqualValues(t, 0, recycleCalls.Load())
}

func TestServiceCoreRecycleActiveRuntimeImagesUndoesRecycleWhenRowExpiresMidSweep(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "recycle_runtime_images_raced")
	require.NoError(t, err)

	projectID, assistantID, threadID := insertReapableProject(t, conn, "recycle-raced")
	originalMetadata := `{"app_name":"gram-asst-raced","machine_id":"m-1"}`
	runtimeID := insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendFlyIO, runtimeStateActive, originalMetadata)

	// The fake recycles "successfully" but expires the row first, simulating
	// the warm timer winning the race while the machine update was in flight.
	stopCalls := &atomic.Int64{}
	backend := testRuntimeBackend{
		backend:   runtimeBackendFlyIO,
		stopCalls: stopCalls,
		recycleFn: func(ctx context.Context, record assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
			require.NoError(t, assistantsrepo.New(conn).StopAssistantRuntime(ctx, assistantsrepo.StopAssistantRuntimeParams{
				State:         runtimeStateStopped,
				ProjectID:     record.ProjectID,
				RuntimeID:     record.ID,
				StartingState: runtimeStateStarting,
				ActiveState:   runtimeStateActive,
				ExpiringState: runtimeStateExpiring,
			}))
			return RuntimeBackendRecycleResult{
				Recycled:            true,
				BackendMetadataJSON: []byte(`{"app_name":"gram-asst-raced","machine_id":"m-1","last_boot_id":"boot-2"}`),
			}, nil
		},
	}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.RecycleActiveRuntimeImages(t.Context(), RecycleAssistantRuntimeImagesParams{OnRowProcessed: nil})
	require.NoError(t, err)
	require.Equal(t, 0, result.Recycled)
	require.Equal(t, 1, result.Skipped)
	require.Equal(t, 0, result.Errors)
	// The restart was undone and the dead row's metadata left alone.
	require.EqualValues(t, 1, stopCalls.Load())
	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
	require.NoError(t, err)
	require.JSONEq(t, originalMetadata, string(runtime.BackendMetadataJson))
	require.Equal(t, runtimeStateStopped, runtime.State)
}

func TestServiceCoreRecycleActiveRuntimeImagesCountsBackendErrors(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "recycle_runtime_images_errors")
	require.NoError(t, err)

	projectID, assistantID, threadID := insertReapableProject(t, conn, "recycle-errors")
	runtimeID := insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendFlyIO, runtimeStateActive, `{"app_name":"gram-asst-flaky","machine_id":"m-1"}`)

	backend := testRuntimeBackend{
		backend:    runtimeBackendFlyIO,
		recycleErr: errors.New("fly api 503"),
	}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.RecycleActiveRuntimeImages(t.Context(), RecycleAssistantRuntimeImagesParams{OnRowProcessed: nil})
	require.NoError(t, err)
	require.Equal(t, 0, result.Recycled)
	require.Equal(t, 1, result.Errors)

	// Metadata is untouched on failure.
	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
	require.NoError(t, err)
	require.JSONEq(t, `{"app_name":"gram-asst-flaky","machine_id":"m-1"}`, string(runtime.BackendMetadataJson))
}

// A non-reuse backend (GKE) has no in-place image swap: it rolls onto a new
// image by terminating idle runtimes (warm-TTL expiry), so the in-place recycle
// sweep is a no-op for it — it touches no rows and tears nothing down.
func TestServiceCoreRecycleActiveRuntimeImagesNoOpsForNonReuseBackend(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "recycle_runtime_images_gke_noop")
	require.NoError(t, err)

	projectID, assistantID, threadID := insertReapableProject(t, conn, "recycle-gke-noop")
	runtimeID := insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendGKE, runtimeStateActive, `{"claim_name":"gram-asst-idle"}`)

	stopCalls := &atomic.Int64{}
	recycleCalls := &atomic.Int64{}
	backend := testRuntimeBackend{
		backend:      runtimeBackendGKE,
		statusResult: RuntimeBackendStatus{Configured: true, IdleSeconds: nil},
		stopCalls:    stopCalls,
		recycleCalls: recycleCalls,
	}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.RecycleActiveRuntimeImages(t.Context(), RecycleAssistantRuntimeImagesParams{OnRowProcessed: nil})
	require.NoError(t, err)
	require.Equal(t, 0, result.Recycled)
	require.Equal(t, 0, result.Skipped)
	require.Equal(t, 0, result.Errors)
	require.EqualValues(t, 0, stopCalls.Load(), "the in-place sweep must not tear down GKE runtimes")
	require.EqualValues(t, 0, recycleCalls.Load(), "GKE has no in-place recycle")

	runtime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateActive, runtime.State, "GKE rows roll via terminate-on-idle, not the sweep")
}

func TestServiceCoreReapInactiveAssistantRuntimesCollectsOnlyInactive(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_inactive_runtimes")
	require.NoError(t, err)

	stale, staleAssistantID, staleThreadID := insertReapableProject(t, conn, "stale")
	staleRuntimeID := insertReapableRuntimeRow(t, conn, stale, staleAssistantID, staleThreadID, runtimeStateStopped, "gram-asst-stale", time.Now().UTC().Add(-30*24*time.Hour))

	fresh, freshAssistantID, freshThreadID := insertReapableProject(t, conn, "fresh")
	freshRuntimeID := insertReapableRuntimeRow(t, conn, fresh, freshAssistantID, freshThreadID, runtimeStateStopped, "gram-asst-fresh", time.Now().UTC().Add(-time.Hour))

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           10,
		OnRowProcessed:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Reaped)
	require.Equal(t, 0, result.Errors)
	require.EqualValues(t, 1, reapCalls.Load())

	staleRuntime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: staleRuntimeID, ProjectID: stale})
	require.NoError(t, err)
	freshRuntime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: freshRuntimeID, ProjectID: fresh})
	require.NoError(t, err)
	require.Equal(t, runtimeStateReaped, staleRuntime.State)
	require.Equal(t, runtimeStateStopped, freshRuntime.State)
}

func TestServiceCoreReapInactiveAssistantRuntimesSkipsAssistantWithRecentActivity(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_inactive_recent_activity")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	// Old runtime row + a fresh row on the same assistant. The recent
	// activity must keep the entire assistant out of the candidate set.
	oldRuntimeID := insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-old", time.Now().UTC().Add(-30*24*time.Hour))
	insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-recent", time.Now().UTC().Add(-time.Hour))

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           10,
		OnRowProcessed:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.Reaped)
	require.EqualValues(t, 0, reapCalls.Load())

	oldRuntime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: oldRuntimeID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStopped, oldRuntime.State)
}

func TestServiceCoreReapInactiveAssistantRuntimesReapsSiblingsAcrossSweeps(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_inactive_siblings")
	require.NoError(t, err)

	// Two stale runtime rows on the same assistant (different threads so the
	// active-runtime unique index doesn't collide). The first sweep reaps one
	// and bumps its updated_at — without the metadata-cleared filter on the
	// NOT EXISTS guard, that bump would block the sibling for another full
	// inactivity window.
	projectID, assistantID, threadA := insertReapableProject(t, conn, "siblings")
	chatB := uuid.New()
	err = assistantsrepo.New(conn).UpsertAssistantChat(t.Context(), assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatB,
		ProjectID:      projectID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)
	threadB, err := assistantsrepo.New(conn).UpsertAssistantThread(t.Context(), assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     projectID,
		CorrelationID: "corr-siblings-b",
		ChatID:        chatB,
		SourceKind:    sourceKindSlack,
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)

	insertReapableRuntimeRow(t, conn, projectID, assistantID, threadA, runtimeStateStopped, "gram-asst-sibling-a", time.Now().UTC().Add(-30*24*time.Hour))
	insertReapableRuntimeRow(t, conn, projectID, assistantID, threadB, runtimeStateStopped, "gram-asst-sibling-b", time.Now().UTC().Add(-30*24*time.Hour))

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	first, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           1,
		OnRowProcessed:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, first.Reaped)

	second, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           10,
		OnRowProcessed:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, second.Reaped, "sibling row must remain a candidate after the first reap")
	require.EqualValues(t, 2, reapCalls.Load())
}

func TestServiceCoreReapStoppedAssistantRuntimesCollectsOnlyAged(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_stopped_runtimes")
	require.NoError(t, err)

	projectID, assistantID, threadID := insertReapableProject(t, conn, "stopped-aged")
	staleID := insertReapableRuntimeRowWithMetadata(t, conn, projectID, assistantID, threadID, runtimeStateStopped,
		`{"app_name":"gram-asst-stale","app_id":"app-1","app_url":"https://gram-asst-stale.fly.dev","app_ip":"1.2.3.4","machine_id":"machine-stale","last_boot_id":"boot-1"}`,
		time.Now().UTC().Add(-30*24*time.Hour),
	)

	freshProject, freshAssistantID, freshThreadID := insertReapableProject(t, conn, "stopped-fresh")
	freshID := insertReapableRuntimeRow(t, conn, freshProject, freshAssistantID, freshThreadID, runtimeStateStopped, "gram-asst-fresh", time.Now().UTC().Add(-time.Hour))

	reapMachineCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapMachineCalls: reapMachineCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapStoppedAssistantRuntimes(t.Context(), ReapStoppedAssistantRuntimesParams{
		StoppedTTL: 14 * 24 * time.Hour,
		BatchSize:  10,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Reaped)
	require.Equal(t, 0, result.Errors)
	require.EqualValues(t, 1, reapMachineCalls.Load())

	stale, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: staleID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateReaped, stale.State)

	var staleMeta map[string]any
	require.NoError(t, json.Unmarshal(stale.BackendMetadataJson, &staleMeta))
	require.Equal(t, "gram-asst-stale", staleMeta["app_name"], "app fields must survive per-thread reap")
	require.Equal(t, "app-1", staleMeta["app_id"])
	require.Equal(t, "1.2.3.4", staleMeta["app_ip"])
	require.NotContains(t, staleMeta, "machine_id", "machine slot must be cleared")
	require.NotContains(t, staleMeta, "last_boot_id", "boot id must be cleared")

	fresh, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: freshID, ProjectID: freshProject})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStopped, fresh.State, "rows inside the TTL window must stay put")
}

func TestServiceCoreReapStoppedAssistantRuntimesIgnoresSiblingActivity(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_stopped_sibling_active")
	require.NoError(t, err)

	// Same assistant: one fully-active row, one long-stopped row. The
	// per-thread sweep must still collect the stopped row — that is the whole
	// reason this path exists.
	projectID, assistantID, activeThread := insertReapableProject(t, conn, "stopped-sibling")
	insertReapableRuntimeRow(t, conn, projectID, assistantID, activeThread, runtimeStateActive, "gram-asst-live", time.Now().UTC().Add(-time.Minute))

	chatB := uuid.New()
	require.NoError(t, assistantsrepo.New(conn).UpsertAssistantChat(t.Context(), assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatB,
		ProjectID:      projectID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	}))
	stoppedThread, err := assistantsrepo.New(conn).UpsertAssistantThread(t.Context(), assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     projectID,
		CorrelationID: "corr-stopped-sibling",
		ChatID:        chatB,
		SourceKind:    sourceKindSlack,
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)
	stoppedID := insertReapableRuntimeRow(t, conn, projectID, assistantID, stoppedThread, runtimeStateStopped, "gram-asst-stuck", time.Now().UTC().Add(-30*24*time.Hour))

	reapMachineCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapMachineCalls: reapMachineCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapStoppedAssistantRuntimes(t.Context(), ReapStoppedAssistantRuntimesParams{
		StoppedTTL: 14 * 24 * time.Hour,
		BatchSize:  10,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Reaped)
	require.EqualValues(t, 1, reapMachineCalls.Load())

	stopped, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: stoppedID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateReaped, stopped.State)
}

func TestServiceCoreReapStoppedAssistantRuntimesSkipsHistoricalRowWithFresherStoppedSibling(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_stopped_historical")
	require.NoError(t, err)

	// `ReserveAssistantRuntime` carries the prior row's `backend_metadata_json`
	// forward, so an old stopped row can share `machine_id` with a much
	// newer stopped row. Reaping the old one would destroy the machine the
	// fresh sibling still references — well inside its own TTL.
	projectID, assistantID, threadID := insertReapableProject(t, conn, "historical-shared-machine")
	oldID := insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-shared", time.Now().UTC().Add(-30*24*time.Hour))
	freshID := insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-shared", time.Now().UTC().Add(-time.Hour))

	reapMachineCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapMachineCalls: reapMachineCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapStoppedAssistantRuntimes(t.Context(), ReapStoppedAssistantRuntimesParams{
		StoppedTTL: 14 * 24 * time.Hour,
		BatchSize:  10,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.Reaped)
	require.EqualValues(t, 0, reapMachineCalls.Load(), "historical rows must not trigger destroys while a fresher stopped sibling owns the machine")

	old, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: oldID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStopped, old.State)
	fresh, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: freshID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStopped, fresh.State)
}

func TestServiceCoreReapStoppedAssistantRuntimesSkipsThreadWithFresherStartingRow(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_stopped_race_skip")
	require.NoError(t, err)

	// Stopped row whose thread has since picked up a fresh starting row —
	// the warm-resume admit path. The per-thread sweep must skip so it does
	// not destroy a machine an active admit is restarting.
	projectID, assistantID, threadID := insertReapableProject(t, conn, "race-skip")
	stoppedID := insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-race", time.Now().UTC().Add(-30*24*time.Hour))
	startingID := uuid.New()
	require.NoError(t, assistantsrepo.New(conn).CreateAssistantRuntime(t.Context(), assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  startingID,
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{"app_name":"gram-asst-race","machine_id":"machine-race"}`),
		State:               runtimeStateStarting,
		WarmUntil:           pgtype.Timestamptz{},
		LastHeartbeatAt:     pgtype.Timestamptz{},
		UpdatedAt:           pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		EndedAt:             pgtype.Timestamptz{},
		DeletedAt:           pgtype.Timestamptz{},
	}))

	reapMachineCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapMachineCalls: reapMachineCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapStoppedAssistantRuntimes(t.Context(), ReapStoppedAssistantRuntimesParams{
		StoppedTTL: 14 * 24 * time.Hour,
		BatchSize:  10,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.Reaped)
	require.EqualValues(t, 0, reapMachineCalls.Load(), "race-skip must not call the backend")

	stopped, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: stoppedID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStopped, stopped.State, "stopped row must remain untouched")
}

func TestServiceCoreReapStoppedAssistantRuntimesIgnoresActiveAndStarting(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_stopped_state_filter")
	require.NoError(t, err)

	projectID, assistantID, threadID := insertReapableProject(t, conn, "stopped-state-active")
	insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateActive, "gram-asst-active", time.Now().UTC().Add(-30*24*time.Hour))

	startingProject, startingAssistantID, startingThreadID := insertReapableProject(t, conn, "stopped-state-starting")
	insertReapableRuntimeRow(t, conn, startingProject, startingAssistantID, startingThreadID, runtimeStateStarting, "gram-asst-starting", time.Now().UTC().Add(-30*24*time.Hour))

	reapMachineCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapMachineCalls: reapMachineCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	result, err := core.ReapStoppedAssistantRuntimes(t.Context(), ReapStoppedAssistantRuntimesParams{
		StoppedTTL: 14 * 24 * time.Hour,
		BatchSize:  10,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.Reaped)
	require.EqualValues(t, 0, reapMachineCalls.Load())
}

func TestServiceCoreReapInactiveAssistantRuntimesSkipsLiveRowsCollectsFinalized(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_inactive_state_agnostic")
	require.NoError(t, err)

	stale := time.Now().UTC().Add(-30 * 24 * time.Hour)

	// Live row: no matter how long idle, the VM stays until the assistant
	// is deleted.
	liveProject, liveAssistantID, liveThreadID := insertReapableProject(t, conn, "live-idle")
	liveRuntimeID := insertReapableRuntimeRow(t, conn, liveProject, liveAssistantID, liveThreadID, runtimeStateActive, "gram-asst-live", stale)

	// Finalized row that kept a live-looking state (a turn racing the
	// teardown can re-stamp `active` post-delete): still collected.
	tombProject, tombAssistantID, tombThreadID := insertReapableProject(t, conn, "tombstone")
	tombRuntimeID := insertReapableRuntimeRow(t, conn, tombProject, tombAssistantID, tombThreadID, runtimeStateActive, "gram-asst-tomb", stale)
	err = assistantsrepo.New(conn).StopAssistantRuntime(t.Context(), assistantsrepo.StopAssistantRuntimeParams{
		State:         runtimeStateActive,
		ProjectID:     tombProject,
		RuntimeID:     tombRuntimeID,
		StartingState: runtimeStateStarting,
		ActiveState:   runtimeStateActive,
		ExpiringState: runtimeStateExpiring,
	})
	require.NoError(t, err)
	err = assistantsrepo.New(conn).BackdateAssistantRuntimeUpdatedAt(t.Context(), assistantsrepo.BackdateAssistantRuntimeUpdatedAtParams{
		UpdatedAt:         pgtype.Timestamptz{Time: stale, Valid: true},
		AssistantThreadID: tombThreadID,
		State:             runtimeStateActive,
	})
	require.NoError(t, err)

	// Live row under a soft-deleted assistant: the delete path's inline reap
	// is best-effort, so the janitor stays the safety net here.
	orphanProject, orphanAssistantID, orphanThreadID := insertReapableProject(t, conn, "deleted-assistant")
	orphanRuntimeID := insertReapableRuntimeRow(t, conn, orphanProject, orphanAssistantID, orphanThreadID, runtimeStateActive, "gram-asst-orphan", stale)
	err = assistantsrepo.New(conn).DeleteAssistant(t.Context(), assistantsrepo.DeleteAssistantParams{
		AssistantID: orphanAssistantID,
		ProjectID:   orphanProject,
	})
	require.NoError(t, err)

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger())

	rowProcessed := &atomic.Int64{}
	result, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           10,
		OnRowProcessed:      func() { rowProcessed.Add(1) },
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Reaped)
	require.EqualValues(t, 2, reapCalls.Load())
	require.EqualValues(t, 2, rowProcessed.Load(), "OnRowProcessed must fire once per row so the activity can heartbeat")

	liveRuntime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: liveRuntimeID, ProjectID: liveProject})
	require.NoError(t, err)
	require.Equal(t, runtimeStateActive, liveRuntime.State, "live idle runtime must not be collected")
	require.False(t, liveRuntime.DeletedAt.Valid)

	tombRuntime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: tombRuntimeID, ProjectID: tombProject})
	require.NoError(t, err)
	require.Equal(t, runtimeStateReaped, tombRuntime.State)

	orphanRuntime, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ID: orphanRuntimeID, ProjectID: orphanProject})
	require.NoError(t, err)
	require.Equal(t, runtimeStateReaped, orphanRuntime.State, "live row under deleted assistant must be collected")
}

type testRuntimeBackend struct {
	backend           string
	ensureResult      RuntimeBackendEnsureResult
	ensureErr         error
	runTurnErr        error
	runTurnMCPServers *atomic.Pointer[[]runtimeMCPServer]
	runTurnPrompt     *atomic.Pointer[string]
	statusResult      RuntimeBackendStatus
	statusErr         error
	stopErr           error
	stopCalls         *atomic.Int64
	reapCalls         *atomic.Int64
	reapErr           error
	reapMachineCalls  *atomic.Int64
	reapMachineErr    error
	imageRef          string
	recycleResult     RuntimeBackendRecycleResult
	recycleErr        error
	recycleCalls      *atomic.Int64
	// recycleFn, when set, replaces the canned recycleResult/recycleErr so a
	// test can mutate DB state mid-sweep (e.g. expire the row) before the
	// service persists the outcome.
	recycleFn func(context.Context, assistantRuntimeRecord) (RuntimeBackendRecycleResult, error)
}

func (t testRuntimeBackend) Backend() string {
	return t.backend
}

func (t testRuntimeBackend) SupportsBackend(backend string) bool {
	return backend == t.backend
}

func (t testRuntimeBackend) ServerURL() *url.URL {
	return &url.URL{Scheme: "https", Host: "gram.example.com"}
}

func (t testRuntimeBackend) Ensure(context.Context, assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	if t.ensureErr != nil {
		return RuntimeBackendEnsureResult{}, t.ensureErr
	}
	return t.ensureResult, nil
}

func (t testRuntimeBackend) ImageRef() string {
	return t.imageRef
}

func (t testRuntimeBackend) ReusesIdleRuntimes() bool {
	// Mirror production: GKE tears idle runtimes down (no warm reuse), every
	// other backend preserves them for warm restart.
	return t.backend != runtimeBackendGKE
}

func (t testRuntimeBackend) RecycleImage(ctx context.Context, record assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	if t.recycleCalls != nil {
		t.recycleCalls.Add(1)
	}
	if t.recycleFn != nil {
		return t.recycleFn(ctx, record)
	}
	if t.recycleErr != nil {
		return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, t.recycleErr
	}
	return t.recycleResult, nil
}

func (t testRuntimeBackend) RunTurn(_ context.Context, _ assistantRuntimeRecord, _ uuid.UUID, _ string, _ string, prompt string, mcpServers []runtimeMCPServer) error {
	if t.runTurnMCPServers != nil {
		captured := append([]runtimeMCPServer(nil), mcpServers...)
		t.runTurnMCPServers.Store(&captured)
	}
	if t.runTurnPrompt != nil {
		t.runTurnPrompt.Store(&prompt)
	}
	return t.runTurnErr
}

func (t testRuntimeBackend) Status(context.Context, assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	if t.statusErr != nil {
		return RuntimeBackendStatus{}, t.statusErr
	}
	return t.statusResult, nil
}

func (t testRuntimeBackend) Stop(context.Context, assistantRuntimeRecord) error {
	if t.stopCalls != nil {
		t.stopCalls.Add(1)
	}
	return t.stopErr
}

func (t testRuntimeBackend) Reap(context.Context, assistantRuntimeRecord) error {
	if t.reapCalls != nil {
		t.reapCalls.Add(1)
	}
	return t.reapErr
}

func (t testRuntimeBackend) ReapStoppedMachine(context.Context, assistantRuntimeRecord) error {
	if t.reapMachineCalls != nil {
		t.reapMachineCalls.Add(1)
	}
	return t.reapMachineErr
}

func mustParseURLForServiceTest(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

func TestServiceCoreEnqueueTriggerTaskSkipsMissingAssistant(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_enqueue_missing")
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tokens := assistanttokens.New("test-jwt-secret", conn, nil)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, tokens, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())

	missing := uuid.New()
	result, err := core.EnqueueTriggerTask(t.Context(), bgtriggers.Task{
		TriggerInstanceID: uuid.New().String(),
		DefinitionSlug:    sourceKindSlack,
		TargetKind:        bgtriggers.TargetKindAssistant,
		TargetRef:         missing.String(),
		EventID:           "evt-missing",
		CorrelationID:     "corr-missing",
		EventJSON:         []byte(`{}`),
	})
	require.NoError(t, err)
	require.False(t, result.ShouldSignal)
	require.Equal(t, uuid.Nil, result.AssistantID)
}
