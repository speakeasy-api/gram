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
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{Time: time.Now().UTC().Add(-2 * time.Minute), Valid: true},
		LastHeartbeatAt:     pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: time.Now().UTC().Add(-10 * time.Minute), Valid: true},
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
		AssistantThreadID:   threadID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJson: []byte(`{}`),
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{Time: staleWarmUntil, Valid: true},
		LastHeartbeatAt:     pgtype.Timestamptz{Time: staleHeartbeat, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: staleHeartbeat, Valid: true},
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

	projectID := uuid.New()
	chatID := uuid.New()

	_, err = conn.Exec(t.Context(), `
INSERT INTO projects (id, name, slug, organization_id)
VALUES ($1, 'Project', 'project', 'org-test')
`, projectID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO chats (id, project_id, organization_id)
VALUES ($1, $2, 'org-test')
`, chatID, projectID)
	require.NoError(t, err)

	rows := []struct {
		gen     int
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
		_, err = conn.Exec(t.Context(), `
INSERT INTO chat_messages (chat_id, project_id, role, content, generation)
VALUES ($1, $2, $3, $4, $5)
`, chatID, projectID, r.role, r.content, r.gen)
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

	var state string
	var deleted bool
	err = conn.QueryRow(t.Context(), `
SELECT state, deleted
FROM assistant_runtimes
WHERE assistant_thread_id = $1
`, threadID).Scan(&state, &deleted)
	require.NoError(t, err)
	require.Equal(t, runtimeStateFailed, state, "unhealthy turn must mark runtime failed so the admission backoff applies")
	require.True(t, deleted)

	var eventStatus string
	err = conn.QueryRow(t.Context(), `
SELECT status
FROM assistant_thread_events
WHERE assistant_thread_id = $1
`, threadID).Scan(&eventStatus)
	require.NoError(t, err)
	require.Equal(t, eventStatusProcessing, eventStatus, "unhealthy turn leaves the event in processing for the reaper")
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
	_, err = conn.Exec(t.Context(), `
UPDATE assistant_runtimes
SET updated_at = clock_timestamp() - INTERVAL '1 hour'
WHERE assistant_thread_id = $1
  AND state = $2
`, threadID, runtimeStateFailed)
	require.NoError(t, err)

	admittedAgain, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admittedAgain.ThreadIDs)

	nextRuntime, err := assistantsrepo.New(conn).GetActiveAssistantRuntimeByThreadID(t.Context(), assistantsrepo.GetActiveAssistantRuntimeByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.JSONEq(t, string(backend.ensureResult.BackendMetadataJSON), string(nextRuntime.BackendMetadataJson))
}

type testRuntimeBackend struct {
	backend      string
	ensureResult RuntimeBackendEnsureResult
	ensureErr    error
	configureErr error
	runTurnErr   error
	stopCalls    *atomic.Int64
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

func (t testRuntimeBackend) Stop(context.Context, assistantRuntimeRecord) error {
	if t.stopCalls != nil {
		t.stopCalls.Add(1)
	}
	return nil
}

func mustParseURLForServiceTest(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}
