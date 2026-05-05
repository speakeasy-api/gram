package assistants

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var assistantsInfra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
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

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

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

func TestWarmRemainingSecondsKeepsBusyRunnerAlive(t *testing.T) {
	t.Parallel()

	// The runner sends idle_seconds=0 while a turn is in flight (see
	// agents/runner/src/wire.rs::RunnerStateResponse) and omits the field
	// entirely only when never /configured. ExpireThreadRuntime must treat
	// both shapes as "do not stop": the &0 case covers the production race,
	// and the nil case is a defensive guard against an unconfigured backend
	// row sneaking past the CAS.
	zero := uint64(0)
	require.Positive(t, warmRemainingSeconds(&zero, 300), "busy runner (idle=&0) must keep a positive warm window")
	require.Positive(t, warmRemainingSeconds(nil, 300), "missing idle (never configured) must not collapse to a Stop decision")
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
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(logger))

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
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), conn, failingBackend, nil, nil, nil, telemetry.NewStub(logger))

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
	core = NewServiceCore(logger, testenv.NewTracerProvider(t), conn, healingBackend, nil, nil, nil, telemetry.NewStub(logger))

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

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

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

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

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

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

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

func TestServiceCoreReapStuckRuntimesReclaimsStaleProcessingLease(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	ctx := t.Context()
	threadKey := assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID}
	runtimeKey := assistantsrepo.GetLatestAssistantRuntimeByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID}

	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(ctx, threadKey)
	require.NoError(t, err)

	staleHeartbeat := time.Now().UTC().Add(-(runtimeProcessingLeaseGrace + time.Minute))
	staleWarmUntil := time.Now().UTC().Add(-(runtimeWarmExpiryReapGrace + time.Minute))
	err = assistantsrepo.New(conn).CreateAssistantRuntime(ctx, assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  uuid.New(),
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{Time: staleWarmUntil, Valid: true},
		LastHeartbeatAt:     pgtype.Timestamptz{Time: staleHeartbeat, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: staleHeartbeat, Valid: true},
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

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	result, err := core.ReapStuckRuntimes(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.StaleRuntimesStopped)
	require.EqualValues(t, 1, result.StaleEventsRequeued)

	runtime, err := assistantsrepo.New(conn).GetLatestAssistantRuntimeByThreadID(ctx, runtimeKey)
	require.NoError(t, err)
	require.True(t, runtime.DeletedAt.Valid)
	require.Equal(t, runtimeStateStopped, runtime.State)

	event, err = assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(ctx, threadKey)
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, event.Status)
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

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

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

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

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

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

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
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, tokens, nil, telemetry.NewStub(logger))

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
}

func TestServiceCoreProcessThreadEventsRequeuesOnTurnFailure(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_process_fail")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	logger := testenv.NewLogger(t)
	tokens := assistanttokens.New("test-jwt-secret", conn, nil)
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: errors.New("runtime RunTurn blew up")}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), conn, backend, nil, tokens, nil, telemetry.NewStub(logger))

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
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), conn, backend, nil, tokens, mustParseURLForServiceTest(t, "https://gram.example.com"), telemetry.NewStub(logger))

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

func TestServiceCoreProcessThreadEventsDoesNotStopRuntimeOnConfigureFailure(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_config_fail")
	require.NoError(t, err)

	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	var stopCalls atomic.Int64
	logger := testenv.NewLogger(t)
	tokens := assistanttokens.New("test-jwt-secret", conn, nil)
	backend := testRuntimeBackend{
		backend: runtimeBackendFlyIO,
		ensureResult: RuntimeBackendEnsureResult{
			ColdStart:      true,
			NeedsConfigure: true,
			BackendMetadataJSON: []byte(`{
				"app_name": "gram-asst-test",
				"app_url": "https://gram-asst-test.fly.dev",
				"machine_id": "machine-1",
				"last_boot_id": "boot-1"
			}`),
		},
		configureErr: errors.New("runtime Configure blew up"),
		stopCalls:    &stopCalls,
	}
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), conn, backend, nil, tokens, mustParseURLForServiceTest(t, "https://gram.example.com"), telemetry.NewStub(logger))

	admitted, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)

	result, err := core.ProcessThreadEvents(t.Context(), projectID, threadID)
	require.NoError(t, err)
	require.True(t, result.RetryAdmission)
	require.False(t, result.RuntimeActive)
	require.Equal(t, int64(0), stopCalls.Load(), "configure failure should preserve the Fly app for reuse/recovery")

	failedRuntime, err := assistantsrepo.New(conn).GetLatestAssistantRuntimeByThreadID(t.Context(), assistantsrepo.GetLatestAssistantRuntimeByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, runtimeStateFailed, failedRuntime.State)
	require.True(t, failedRuntime.Deleted)
	require.JSONEq(t, string(backend.ensureResult.BackendMetadataJSON), string(failedRuntime.BackendMetadataJson))

	hotAdmit, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Empty(t, hotAdmit.ThreadIDs, "admission backoff must block re-admit immediately after a setup failure")

	// Simulate the backoff window elapsing so the next admit is eligible.
	err = assistantsrepo.New(conn).BackdateAssistantRuntimeUpdatedAt(t.Context(), assistantsrepo.BackdateAssistantRuntimeUpdatedAtParams{
		UpdatedAt:         pgtype.Timestamptz{Time: time.Now().UTC().Add(-time.Hour), Valid: true},
		AssistantThreadID: threadID,
		State:             runtimeStateFailed,
	})
	require.NoError(t, err)

	admittedAgain, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admittedAgain.ThreadIDs)

	nextRuntime, err := assistantsrepo.New(conn).GetActiveAssistantRuntimeByThreadID(t.Context(), assistantsrepo.GetActiveAssistantRuntimeByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.JSONEq(t, string(backend.ensureResult.BackendMetadataJSON), string(nextRuntime.BackendMetadataJson))
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

	runtimeID := uuid.New()
	metadata := fmt.Sprintf(`{"app_name":%q}`, appName)
	err := assistantsrepo.New(conn).CreateAssistantRuntime(t.Context(), assistantsrepo.CreateAssistantRuntimeParams{
		ID:                  runtimeID,
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(metadata),
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
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	require.NoError(t, core.DeleteAssistant(t.Context(), projectID, assistantID))
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
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	require.NoError(t, core.DeleteAssistant(t.Context(), projectID, assistantID))
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
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

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
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	result, err := core.ReapAssistantRuntimes(t.Context(), projectID, assistantID)
	require.NoError(t, err)
	require.Equal(t, 0, result.Reaped)
	require.EqualValues(t, 0, reapCalls.Load())
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
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	result, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           10,
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
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	result, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           10,
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
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	first, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           1,
	})
	require.NoError(t, err)
	require.Equal(t, 1, first.Reaped)

	second, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           10,
	})
	require.NoError(t, err)
	require.Equal(t, 1, second.Reaped, "sibling row must remain a candidate after the first reap")
	require.EqualValues(t, 2, reapCalls.Load())
}

func TestServiceCoreReapInactiveAssistantRuntimesIgnoresActiveAndStarting(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_inactive_state_filter")
	require.NoError(t, err)

	projectID, assistantID, threadID := insertReapableProject(t, conn, "active-state")
	insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateActive, "gram-asst-active", time.Now().UTC().Add(-30*24*time.Hour))

	startingProject, startingAssistantID, startingThreadID := insertReapableProject(t, conn, "starting-state")
	insertReapableRuntimeRow(t, conn, startingProject, startingAssistantID, startingThreadID, runtimeStateStarting, "gram-asst-starting", time.Now().UTC().Add(-30*24*time.Hour))

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	result, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour,
		BatchSize:           10,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.Reaped)
	require.EqualValues(t, 0, reapCalls.Load())
}

type testRuntimeBackend struct {
	backend      string
	ensureResult RuntimeBackendEnsureResult
	ensureErr    error
	configureErr error
	runTurnErr   error
	statusResult RuntimeBackendStatus
	statusErr    error
	stopErr      error
	stopCalls    *atomic.Int64
	reapCalls    *atomic.Int64
	reapErr      error
}

func (t testRuntimeBackend) Backend() string {
	return t.backend
}

func (t testRuntimeBackend) SupportsBackend(backend string) bool {
	return backend == t.backend
}

func (t testRuntimeBackend) Ensure(context.Context, assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	if t.ensureErr != nil {
		return RuntimeBackendEnsureResult{}, t.ensureErr
	}
	return t.ensureResult, nil
}

func (t testRuntimeBackend) Configure(context.Context, assistantRuntimeRecord, runtimeStartupConfig) error {
	return t.configureErr
}

func (t testRuntimeBackend) RunTurn(context.Context, assistantRuntimeRecord, string, string, string) error {
	return t.runTurnErr
}

func (t testRuntimeBackend) ServerURL(context.Context, assistantRuntimeRecord, *url.URL) (*url.URL, error) {
	parsed, err := url.Parse("https://gram.example.com")
	if err != nil {
		return nil, fmt.Errorf("parse test server url: %w", err)
	}
	return parsed, nil
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

func mustParseURLForServiceTest(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}
