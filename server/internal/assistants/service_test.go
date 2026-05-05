package assistants

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
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

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()

	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	admitted, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)
	require.Equal(t, projectID, admitted.ProjectID)

	var backend string
	err = conn.QueryRow(t.Context(), `
SELECT backend
FROM assistant_runtimes
WHERE assistant_thread_id = $1
  AND deleted IS FALSE
`, threadID).Scan(&backend)
	require.NoError(t, err)
	require.Equal(t, runtimeBackendFlyIO, backend)
}

func TestServiceCoreReapStuckRuntimesSkipsLiveProcessingLease(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()

	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

	var eventID uuid.UUID
	err = conn.QueryRow(t.Context(), `
SELECT id
FROM assistant_thread_events
WHERE assistant_thread_id = $1
`, threadID).Scan(&eventID)
	require.NoError(t, err)

	runtimeID := uuid.New()
	_, err = conn.Exec(t.Context(), `
INSERT INTO assistant_runtimes (
  id, assistant_thread_id, assistant_id, project_id, backend, backend_metadata_json, state, warm_until, last_heartbeat_at, updated_at
) VALUES (
  $1, $2, $3, $4, $5, '{}'::jsonb, $6, $7, $8, $9
)
`, runtimeID, threadID, assistantID, projectID, runtimeBackendFlyIO, runtimeStateActive, time.Now().UTC().Add(-2*time.Minute), time.Now().UTC(), time.Now().UTC().Add(-10*time.Minute))
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
UPDATE assistant_thread_events
SET status = $1, updated_at = $2
WHERE id = $3
`, eventStatusProcessing, time.Now().UTC(), eventID)
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	result, err := core.ReapStuckRuntimes(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 0, result.StaleRuntimesStopped)
	require.EqualValues(t, 0, result.StaleEventsRequeued)

	var deletedAt sql.NullTime
	var status string
	err = conn.QueryRow(t.Context(), `
SELECT deleted_at, state
FROM assistant_runtimes
WHERE id = $1
`, runtimeID).Scan(&deletedAt, &status)
	require.NoError(t, err)
	require.False(t, deletedAt.Valid)
	require.Equal(t, runtimeStateActive, status)

	err = conn.QueryRow(t.Context(), `
SELECT status
FROM assistant_thread_events
WHERE id = $1
`, eventID).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, eventStatusProcessing, status)
}

func TestServiceCoreReapStuckRuntimesReclaimsStaleProcessingLease(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()

	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

	var eventID uuid.UUID
	err = conn.QueryRow(t.Context(), `
SELECT id
FROM assistant_thread_events
WHERE assistant_thread_id = $1
`, threadID).Scan(&eventID)
	require.NoError(t, err)

	runtimeID := uuid.New()
	staleHeartbeat := time.Now().UTC().Add(-(runtimeProcessingLeaseGrace + time.Minute))
	staleWarmUntil := time.Now().UTC().Add(-(runtimeWarmExpiryReapGrace + time.Minute))
	_, err = conn.Exec(t.Context(), `
INSERT INTO assistant_runtimes (
  id, assistant_thread_id, assistant_id, project_id, backend, backend_metadata_json, state, warm_until, last_heartbeat_at, updated_at
) VALUES (
  $1, $2, $3, $4, $5, '{}'::jsonb, $6, $7, $8, $9
)
`, runtimeID, threadID, assistantID, projectID, runtimeBackendFlyIO, runtimeStateActive, staleWarmUntil, staleHeartbeat, staleHeartbeat)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
UPDATE assistant_thread_events
SET status = $1, updated_at = $2
WHERE id = $3
`, eventStatusProcessing, time.Now().UTC().Add(-(eventProcessingRequeueGrace + time.Minute)), eventID)
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	result, err := core.ReapStuckRuntimes(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, result.StaleRuntimesStopped)
	require.EqualValues(t, 1, result.StaleEventsRequeued)

	var deletedAt sql.NullTime
	var status string
	err = conn.QueryRow(t.Context(), `
SELECT deleted_at, state
FROM assistant_runtimes
WHERE id = $1
`, runtimeID).Scan(&deletedAt, &status)
	require.NoError(t, err)
	require.True(t, deletedAt.Valid)
	require.Equal(t, runtimeStateStopped, status)

	err = conn.QueryRow(t.Context(), `
SELECT status
FROM assistant_thread_events
WHERE id = $1
`, eventID).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, status)
}

func insertAssistantFixture(t *testing.T, conn *pgxpool.Pool, projectID, assistantID, chatID, threadID uuid.UUID) {
	t.Helper()

	_, err := conn.Exec(t.Context(), `
INSERT INTO projects (id, name, slug, organization_id)
VALUES ($1, 'Project', 'project', 'org-test')
`, projectID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO assistants (id, project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status)
VALUES ($1, $2, 'org-test', 'Assistant', 'openai/gpt-4o-mini', '', 300, 1, 'active')
`, assistantID, projectID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO chats (id, project_id, organization_id)
VALUES ($1, $2, 'org-test')
`, chatID, projectID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO assistant_threads (id, assistant_id, project_id, correlation_id, chat_id, source_kind, source_ref_json, last_event_at)
VALUES ($1, $2, $3, 'corr-1', $4, 'slack', '{}'::jsonb, clock_timestamp())
`, threadID, assistantID, projectID, chatID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO assistant_thread_events (assistant_thread_id, assistant_id, project_id, event_id, correlation_id, status, normalized_payload_json, source_payload_json)
VALUES ($1, $2, $3, 'evt-1', 'corr-1', 'pending', '{"text":"hello"}'::jsonb, '{}'::jsonb)
`, threadID, assistantID, projectID)
	require.NoError(t, err)
}

func TestServiceCoreLoadChatHistoryReplaysToolTurns(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_load_history")
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

	toolCallsJSON := `[{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"oslo\"}"}}]`
	rows := []struct {
		role       string
		content    string
		toolCalls  *string
		toolCallID *string
	}{
		{role: "system", content: "You are Gram."},
		{role: "user", content: "what's the weather in oslo?"},
		{role: "assistant", content: "", toolCalls: &toolCallsJSON},
		{role: "tool", content: `{"temp":"cold"}`, toolCallID: new("call_abc")},
		{role: "assistant", content: "It's cold."},
		{role: "user", content: "thanks"},
	}
	for _, r := range rows {
		_, err = conn.Exec(t.Context(), `
INSERT INTO chat_messages (chat_id, project_id, role, content, tool_calls, tool_call_id)
VALUES ($1, $2, $3, $4, $5::jsonb, $6)
`, chatID, projectID, r.role, r.content, r.toolCalls, r.toolCallID)
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

	_, err = conn.Exec(t.Context(), `
INSERT INTO chat_messages (chat_id, project_id, role, content)
VALUES ($1, $2, 'tool', 'orphan result')
`, chatID, projectID)
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	_, err = core.loadChatHistory(t.Context(), chatID, projectID)
	require.ErrorContains(t, err, "tool chat row missing tool_call_id")
}

func TestServiceCoreProcessThreadEventsCompletesEvent(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_process_ok")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()
	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

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

	var status string
	err = conn.QueryRow(t.Context(), `
SELECT status
FROM assistant_thread_events
WHERE assistant_thread_id = $1
`, threadID).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, eventStatusCompleted, status)

	var runtimeState string
	err = conn.QueryRow(t.Context(), `
SELECT state
FROM assistant_runtimes
WHERE assistant_thread_id = $1
  AND deleted IS FALSE
`, threadID).Scan(&runtimeState)
	require.NoError(t, err)
	require.Equal(t, runtimeStateActive, runtimeState)
}

func TestServiceCoreProcessThreadEventsRequeuesOnTurnFailure(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_process_fail")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()
	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

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

	var status string
	var attempts int
	var lastError sql.NullString
	err = conn.QueryRow(t.Context(), `
SELECT status, attempts, last_error
FROM assistant_thread_events
WHERE assistant_thread_id = $1
`, threadID).Scan(&status, &attempts, &lastError)
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, status)
	require.Equal(t, 1, attempts)
	require.True(t, lastError.Valid)
	require.Contains(t, lastError.String, "runtime RunTurn blew up")
}

func TestServiceCoreProcessThreadEventsMarksRuntimeFailedOnUnhealthyTurn(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_unhealthy_turn")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()
	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

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

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()
	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

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

	var state string
	var deleted bool
	var metadata []byte
	err = conn.QueryRow(t.Context(), `
SELECT state, deleted, backend_metadata_json
FROM assistant_runtimes
WHERE assistant_thread_id = $1
`, threadID).Scan(&state, &deleted, &metadata)
	require.NoError(t, err)
	require.Equal(t, runtimeStateFailed, state)
	require.True(t, deleted)
	require.JSONEq(t, string(backend.ensureResult.BackendMetadataJSON), string(metadata))

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

	var nextMetadata []byte
	err = conn.QueryRow(t.Context(), `
SELECT backend_metadata_json
FROM assistant_runtimes
WHERE assistant_thread_id = $1
  AND deleted IS FALSE
ORDER BY created_at DESC
LIMIT 1
`, threadID).Scan(&nextMetadata)
	require.NoError(t, err)
	require.JSONEq(t, string(backend.ensureResult.BackendMetadataJSON), string(nextMetadata))
}

// insertReapableProject inserts an isolated project + assistant + thread so a
// single test can host several independent fixtures without colliding on the
// project slug or the per-thread-active runtime unique index.
func insertReapableProject(t *testing.T, conn *pgxpool.Pool, slug string) (projectID, assistantID, threadID uuid.UUID) {
	t.Helper()

	projectID = uuid.New()
	assistantID = uuid.New()
	chatID := uuid.New()
	threadID = uuid.New()

	_, err := conn.Exec(t.Context(), `
INSERT INTO projects (id, name, slug, organization_id) VALUES ($1, $2, $3, 'org-test')
`, projectID, slug, slug)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO assistants (id, project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status)
VALUES ($1, $2, 'org-test', 'Assistant', 'openai/gpt-4o-mini', '', 300, 1, 'active')
`, assistantID, projectID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `INSERT INTO chats (id, project_id, organization_id) VALUES ($1, $2, 'org-test')`, chatID, projectID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO assistant_threads (id, assistant_id, project_id, correlation_id, chat_id, source_kind, source_ref_json, last_event_at)
VALUES ($1, $2, $3, $4, $5, 'slack', '{}'::jsonb, clock_timestamp())
`, threadID, assistantID, projectID, "corr-"+slug, chatID)
	require.NoError(t, err)

	return projectID, assistantID, threadID
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
	endedAt := pgNullTimeFor(state, updatedAt)
	_, err := conn.Exec(t.Context(), `
INSERT INTO assistant_runtimes (
  id, assistant_thread_id, assistant_id, project_id, backend, backend_metadata_json, state, updated_at, ended_at
) VALUES (
  $1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9
)
`, runtimeID, threadID, assistantID, projectID, runtimeBackendFlyIO, metadata, state, updatedAt, endedAt)
	require.NoError(t, err)
	return runtimeID
}

func pgNullTimeFor(state string, updatedAt time.Time) sql.NullTime {
	switch state {
	case runtimeStateActive, runtimeStateStarting:
		return sql.NullTime{Time: time.Time{}, Valid: false}
	default:
		return sql.NullTime{Time: updatedAt, Valid: true}
	}
}

func TestServiceCoreDeleteAssistantReapsRuntimes(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "delete_assistant_reaps")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()
	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

	runtimeID := insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-delete-target", time.Now().UTC().Add(-time.Hour))

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	require.NoError(t, core.DeleteAssistant(t.Context(), projectID, assistantID))
	require.EqualValues(t, 1, reapCalls.Load())

	var state, metadataJSON string
	err = conn.QueryRow(t.Context(), `SELECT state, backend_metadata_json::text FROM assistant_runtimes WHERE id = $1`, runtimeID).Scan(&state, &metadataJSON)
	require.NoError(t, err)
	require.Equal(t, runtimeStateReaped, state)
	require.JSONEq(t, `{}`, metadataJSON)

	var deletedAt sql.NullTime
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT deleted_at FROM assistants WHERE id = $1`, assistantID).Scan(&deletedAt))
	require.True(t, deletedAt.Valid)
}

func TestServiceCoreDeleteAssistantSucceedsEvenWhenReapErrors(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "delete_assistant_reap_error")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()
	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)
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
	var deletedAt sql.NullTime
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT deleted_at FROM assistants WHERE id = $1`, assistantID).Scan(&deletedAt))
	require.True(t, deletedAt.Valid)
}

func TestServiceCoreReapAssistantRuntimesCallsBackendAndClearsMetadata(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_assistant_runtimes")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()
	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

	runtimeID := insertReapableRuntimeRow(t, conn, projectID, assistantID, threadID, runtimeStateStopped, "gram-asst-orphan", time.Now().UTC().Add(-time.Hour))

	reapCalls := &atomic.Int64{}
	backend := testRuntimeBackend{backend: runtimeBackendFlyIO, reapCalls: reapCalls}
	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)))

	result, err := core.ReapAssistantRuntimes(t.Context(), projectID, assistantID)
	require.NoError(t, err)
	require.Equal(t, 1, result.Reaped)
	require.Equal(t, 0, result.Errors)
	require.EqualValues(t, 1, reapCalls.Load())

	var state string
	var metadataJSON string
	err = conn.QueryRow(t.Context(), `
SELECT state, backend_metadata_json::text
FROM assistant_runtimes
WHERE id = $1
`, runtimeID).Scan(&state, &metadataJSON)
	require.NoError(t, err)
	require.Equal(t, runtimeStateReaped, state)
	require.JSONEq(t, `{}`, metadataJSON)
}

func TestServiceCoreReapAssistantRuntimesSkipsRowsWithoutMetadata(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_assistant_runtimes_skip")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()
	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

	runtimeID := uuid.New()
	_, err = conn.Exec(t.Context(), `
INSERT INTO assistant_runtimes (id, assistant_thread_id, assistant_id, project_id, backend, backend_metadata_json, state)
VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, $6)
`, runtimeID, threadID, assistantID, projectID, runtimeBackendFlyIO, runtimeStateStopped)
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

	var staleState, freshState string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT state FROM assistant_runtimes WHERE id = $1`, staleRuntimeID).Scan(&staleState))
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT state FROM assistant_runtimes WHERE id = $1`, freshRuntimeID).Scan(&freshState))
	require.Equal(t, runtimeStateReaped, staleState)
	require.Equal(t, runtimeStateStopped, freshState)
}

func TestServiceCoreReapInactiveAssistantRuntimesSkipsAssistantWithRecentActivity(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_inactive_recent_activity")
	require.NoError(t, err)

	projectID := uuid.New()
	assistantID := uuid.New()
	chatID := uuid.New()
	threadID := uuid.New()
	insertAssistantFixture(t, conn, projectID, assistantID, chatID, threadID)

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

	var oldState string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT state FROM assistant_runtimes WHERE id = $1`, oldRuntimeID).Scan(&oldState))
	require.Equal(t, runtimeStateStopped, oldState)
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
	threadB := uuid.New()
	chatB := uuid.New()
	_, err = conn.Exec(t.Context(), `INSERT INTO chats (id, project_id, organization_id) VALUES ($1, $2, 'org-test')`, chatB, projectID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `
INSERT INTO assistant_threads (id, assistant_id, project_id, correlation_id, chat_id, source_kind, source_ref_json, last_event_at)
VALUES ($1, $2, $3, 'corr-siblings-b', $4, 'slack', '{}'::jsonb, clock_timestamp())
`, threadB, assistantID, projectID, chatB)
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

func (t testRuntimeBackend) Stop(context.Context, assistantRuntimeRecord) error {
	if t.stopCalls != nil {
		t.stopCalls.Add(1)
	}
	return nil
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
