package assistants

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	platformdashboard "github.com/speakeasy-api/gram/server/internal/platformtools/dashboard"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// dashboardTask builds a dispatched dashboard trigger task for the assistant.
func dashboardTask(assistantID uuid.UUID, correlationID, eventID, text, userID string) bgtriggers.Task {
	body := []byte(`{"text":"` + text + `","user_id":"` + userID + `"}`)
	return bgtriggers.Task{
		TriggerInstanceID: "",
		DefinitionSlug:    sourceKindDashboard,
		TargetKind:        bgtriggers.TargetKindAssistant,
		TargetRef:         assistantID.String(),
		TargetDisplay:     "",
		EventID:           eventID,
		CorrelationID:     correlationID,
		EventJSON:         body,
		RawPayload:        body,
	}
}

// Enqueuing a dashboard turn records the user's message in the conversation log,
// and the egress tool appends the assistant's reply — together forming the clean
// log the dashboard renders.
func TestDashboardConversationLogRoundTrip(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_dashboard_log")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "dash-log")
	managed, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)

	const correlation = "conv-abc"

	// Ingest the user's turn through the shared enqueue path.
	res, err := core.EnqueueTriggerTask(ctx, dashboardTask(managed.ID, correlation, "evt-1", "what are my top errors?", "user-1"))
	require.NoError(t, err)
	require.True(t, res.ShouldSignal)

	chatID := deterministicChatID(managed.ID, correlation)
	q := assistantsrepo.New(conn)

	logged, err := q.ListDashboardMessages(ctx, assistantsrepo.ListDashboardMessagesParams{
		ChatID:    chatID,
		ProjectID: projectID,
		UserID:    "user-1",
		AfterSeq:  0,
	})
	require.NoError(t, err)
	require.Len(t, logged, 1)
	require.Equal(t, "user", logged[0].Role)
	require.Equal(t, "what are my top errors?", logged[0].Content)

	// Re-enqueuing the same event id (idempotent retry) must not duplicate the
	// user row.
	_, err = core.EnqueueTriggerTask(ctx, dashboardTask(managed.ID, correlation, "evt-1", "what are my top errors?", "user-1"))
	require.NoError(t, err)
	logged, err = q.ListDashboardMessages(ctx, assistantsrepo.ListDashboardMessagesParams{ChatID: chatID, ProjectID: projectID, UserID: "user-1", AfterSeq: 0})
	require.NoError(t, err)
	require.Len(t, logged, 1, "idempotent retry must not duplicate the user turn")

	// The assistant delivers its reply via the egress tool, resolving the chat
	// from its thread id (no recipient supplied by the model).
	threadID, err := q.GetAssistantThreadIDByCorrelation(ctx, assistantsrepo.GetAssistantThreadIDByCorrelationParams{
		ProjectID:     projectID,
		AssistantID:   managed.ID,
		CorrelationID: correlation,
	})
	require.NoError(t, err)

	toolCtx := contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: managed.ID,
		ThreadID:    threadID,
	})
	toolCtx = contextvalues.SetAuthContext(toolCtx, &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		ProjectID:            &projectID,
	})

	var out bytes.Buffer
	require.NoError(t, platformdashboard.NewSendMessageTool(conn).Call(
		toolCtx,
		toolconfig.ToolCallEnv{},
		strings.NewReader(`{"message":"here are your top errors"}`),
		&out,
	))

	logged, err = q.ListDashboardMessages(ctx, assistantsrepo.ListDashboardMessagesParams{ChatID: chatID, ProjectID: projectID, UserID: "user-1", AfterSeq: 0})
	require.NoError(t, err)
	require.Len(t, logged, 2)
	require.Equal(t, "assistant", logged[1].Role)
	require.Equal(t, "here are your top errors", logged[1].Content)
}

// A fresh correlation id starts a new conversation thread/log, leaving the prior
// conversation untouched — the configurable-correlation escape hatch.
func TestDashboardFreshCorrelationStartsNewLog(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_dashboard_fresh")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "dash-fresh")
	managed, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)

	_, err = core.EnqueueTriggerTask(ctx, dashboardTask(managed.ID, "conv-1", "evt-1", "first conversation", "user-1"))
	require.NoError(t, err)
	_, err = core.EnqueueTriggerTask(ctx, dashboardTask(managed.ID, "conv-2", "evt-2", "fresh start", "user-1"))
	require.NoError(t, err)

	q := assistantsrepo.New(conn)
	first, err := q.ListDashboardMessages(ctx, assistantsrepo.ListDashboardMessagesParams{ChatID: deterministicChatID(managed.ID, "conv-1"), ProjectID: projectID, UserID: "user-1", AfterSeq: 0})
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "first conversation", first[0].Content)

	second, err := q.ListDashboardMessages(ctx, assistantsrepo.ListDashboardMessagesParams{ChatID: deterministicChatID(managed.ID, "conv-2"), ProjectID: projectID, UserID: "user-1", AfterSeq: 0})
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, "fresh start", second[0].Content)
}
