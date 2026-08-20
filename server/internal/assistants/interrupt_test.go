package assistants

import (
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const interruptTestUserID = "user-owner"

// insertDashboardAssistantFixture builds the shape a dashboard conversation
// actually has: a chats row owned by a user, a thread whose correlation id IS
// the chat id, and one queued turn. The interrupt path walks exactly that
// chain, so a Slack-shaped fixture would not exercise it.
func insertDashboardAssistantFixture(t *testing.T, conn *pgxpool.Pool, dbName string) (projectID, assistantID, chatID, threadID uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           dbName,
		OrganizationID: "org-test",
	})
	require.NoError(t, err)

	assistant, err := assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       proj.ID,
		OrganizationID:  "org-test",
		CreatedByUserID: pgtype.Text{String: interruptTestUserID, Valid: true},
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
		UserID:         pgtype.Text{String: interruptTestUserID, Valid: true},
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)

	threadID, err = assistantsrepo.New(conn).UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     proj.ID,
		CorrelationID: chatID.String(),
		ChatID:        chatID,
		SourceKind:    sourceKindDashboard,
		SourceRefJson: []byte(`{"user_id":"` + interruptTestUserID + `"}`),
	})
	require.NoError(t, err)

	_, err = assistantsrepo.New(conn).InsertAssistantThreadEvent(ctx, assistantsrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     threadID,
		AssistantID:           assistant.ID,
		ProjectID:             proj.ID,
		TriggerInstanceID:     uuid.NullUUID{Valid: false},
		EventID:               "evt-1",
		CorrelationID:         chatID.String(),
		Status:                eventStatusPending,
		NormalizedPayloadJson: []byte(`{"text":"hello","user_id":"` + interruptTestUserID + `"}`),
		SourcePayloadJson:     []byte("{}"),
	})
	require.NoError(t, err)

	return proj.ID, assistant.ID, chatID, threadID
}

func newInterruptTestCore(t *testing.T, conn *pgxpool.Pool, backend testRuntimeBackend) *ServiceCore {
	t.Helper()
	logger := testenv.NewLogger(t)
	return NewServiceCore(
		logger,
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		nil,
		nil,
		backend,
		nil,
		assistanttokens.New("test-jwt-secret", conn, nil),
		nil,
		telemetry.NewStub(logger),
		nil,
		newTestAuditLogger(),
	)
}

// A stop pressed while the runtime is still cold has no generation to cancel:
// the turn is queued, waiting for a VM. Dropping it from the queue is the only
// thing that keeps the assistant from replying moments after the user said
// stop.
func TestInterruptDashboardTurnCancelsQueuedTurn(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_interrupt_queued")
	require.NoError(t, err)

	projectID, assistantID, chatID, threadID := insertDashboardAssistantFixture(t, conn, "interrupt-queued")
	core := newInterruptTestCore(t, conn, testRuntimeBackend{backend: runtimeBackendFlyIO})

	result, err := core.InterruptDashboardTurn(t.Context(), projectID, assistantID, interruptTestUserID, chatID)
	require.NoError(t, err)
	require.True(t, result.StoppedSomething())
	require.Equal(t, int64(1), result.CancelledQueued)
	require.False(t, result.Interrupted, "no runtime exists, so nothing was generating")
	require.Equal(t, threadID, result.ThreadID)

	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{
		AssistantThreadID: threadID,
		ProjectID:         projectID,
	})
	require.NoError(t, err)
	require.Equal(t, eventStatusCancelled, event.Status)

	// The decisive assertion: admission must no longer see the thread, or the
	// coordinator would spin up a VM to run the turn the user just stopped.
	admitted, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Empty(t, admitted.ThreadIDs)
}

// The other half: a turn already dispatched to a live runtime is stopped by
// the runner, and it must be told which thread to stop — one VM serves every
// thread under an assistant.
func TestInterruptDashboardTurnInterruptsRunningTurn(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_interrupt_running")
	require.NoError(t, err)

	projectID, assistantID, chatID, threadID := insertDashboardAssistantFixture(t, conn, "interrupt-running")

	interruptedThread := &atomic.Pointer[uuid.UUID]{}
	backend := testRuntimeBackend{
		backend:           runtimeBackendFlyIO,
		interruptResult:   true,
		interruptThreadID: interruptedThread,
	}
	core := newInterruptTestCore(t, conn, backend)

	// Drive the real admit → process path so the runtime row lands in the
	// state a live turn actually leaves behind.
	admitted, err := core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)
	processed, err := core.ProcessThreadEvents(t.Context(), projectID, threadID)
	require.NoError(t, err)
	require.True(t, processed.RuntimeActive)

	result, err := core.InterruptDashboardTurn(t.Context(), projectID, assistantID, interruptTestUserID, chatID)
	require.NoError(t, err)
	require.True(t, result.Interrupted)
	require.Equal(t, int64(0), result.CancelledQueued, "the queued turn was already claimed")
	require.True(t, result.StoppedSomething())

	captured := interruptedThread.Load()
	require.NotNil(t, captured, "the runtime must be told which thread to interrupt")
	require.Equal(t, threadID, *captured)
}

// Chat ids are not user-namespaced, so the ownership gate is the only thing
// stopping one user from cancelling another's reply.
func TestInterruptDashboardTurnRejectsForeignChat(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_interrupt_foreign")
	require.NoError(t, err)

	projectID, assistantID, chatID, threadID := insertDashboardAssistantFixture(t, conn, "interrupt-foreign")
	core := newInterruptTestCore(t, conn, testRuntimeBackend{backend: runtimeBackendFlyIO})

	_, err = core.InterruptDashboardTurn(t.Context(), projectID, assistantID, "someone-else", chatID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// And the queued turn survives, so a rejected stop cannot be used to
	// silently drop another user's turn.
	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{
		AssistantThreadID: threadID,
		ProjectID:         projectID,
	})
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, event.Status)
}

// Pressing stop just as the reply lands is the ordinary way this button gets
// used. It reports "nothing to stop" rather than failing.
func TestInterruptDashboardTurnWithNothingRunning(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_interrupt_idle")
	require.NoError(t, err)

	ctx := t.Context()
	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "interrupt-idle",
		OrganizationID: "org-test",
	})
	require.NoError(t, err)

	assistant, err := assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       proj.ID,
		OrganizationID:  "org-test",
		CreatedByUserID: pgtype.Text{String: interruptTestUserID, Valid: true},
		Name:            "Assistant",
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          StatusActive,
	})
	require.NoError(t, err)

	// A chat with no thread yet — the conversation exists but has never
	// dispatched a turn.
	chatID := uuid.New()
	require.NoError(t, assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      proj.ID,
		OrganizationID: "org-test",
		UserID:         pgtype.Text{String: interruptTestUserID, Valid: true},
		Title:          pgtype.Text{},
	}))

	core := newInterruptTestCore(t, conn, testRuntimeBackend{backend: runtimeBackendFlyIO})

	result, err := core.InterruptDashboardTurn(ctx, proj.ID, assistant.ID, interruptTestUserID, chatID)
	require.NoError(t, err)
	require.False(t, result.StoppedSomething())
	require.Equal(t, uuid.Nil, result.ThreadID)
}
