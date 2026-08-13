package triggers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/triggers"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// seedTriggerEvent creates the full assistant→chat→thread→event chain that the
// runtime writes when a trigger fires, and returns the chat id the event should
// resolve to.
func seedTriggerEvent(t *testing.T, ctx context.Context, ti *testInstance, triggerID uuid.UUID, eventID string, status string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	ar := assistantsrepo.New(ti.conn)
	assistant, err := ar.CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:      projectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "Trigger Events Assistant " + uuid.NewString()[:8],
		Model:          "anthropic/claude-opus-4.8",
		Instructions:   "be helpful",
		WarmTtlSeconds: 300,
		MaxConcurrency: 1,
		Status:         "active",
	})
	require.NoError(t, err)

	chatID, err := chatrepo.New(ti.conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             uuid.New(),
		ProjectID:      projectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         pgtype.Text{String: "", Valid: false},
		ExternalUserID: pgtype.Text{String: "", Valid: false},
		Title:          pgtype.Text{String: "Trigger Chat", Valid: true},
	})
	require.NoError(t, err)

	correlationID := "corr-" + uuid.NewString()[:8]
	threadID, err := ar.UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     projectID,
		CorrelationID: correlationID,
		ChatID:        chatID,
		SourceKind:    "slack",
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)

	_, err = ar.InsertAssistantThreadEvent(ctx, assistantsrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     threadID,
		AssistantID:           assistant.ID,
		ProjectID:             projectID,
		TriggerInstanceID:     uuid.NullUUID{UUID: triggerID, Valid: true},
		EventID:               eventID,
		CorrelationID:         correlationID,
		Status:                status,
		NormalizedPayloadJson: []byte("{}"),
		SourcePayloadJson:     []byte("{}"),
	})
	require.NoError(t, err)

	return chatID
}

func TestListTriggerEvents_ReturnsEventsWithChatLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateTriggerInstance(ctx, newCreatePayload(ti.environmentID, "events-happy"))
	require.NoError(t, err)
	triggerID := uuid.MustParse(created.ID)

	other, err := ti.service.CreateTriggerInstance(ctx, newCreatePayload(ti.environmentID, "events-other"))
	require.NoError(t, err)
	otherID := uuid.MustParse(other.ID)

	chatID := seedTriggerEvent(t, ctx, ti, triggerID, "evt-1", "completed")
	seedTriggerEvent(t, ctx, ti, otherID, "evt-2", "pending")

	result, err := ti.service.ListTriggerEvents(ctx, &gen.ListTriggerEventsPayload{
		ID:               created.ID,
		Limit:            50,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Events, 1, "must only list events for the requested trigger")

	event := result.Events[0]
	require.Equal(t, created.ID, event.TriggerInstanceID)
	require.Equal(t, "completed", event.Status)
	require.NotNil(t, event.ChatID)
	require.Equal(t, chatID.String(), *event.ChatID)
	require.NotEmpty(t, event.CreatedAt)
}

func TestListTriggerEvents_EmptyForQuietTrigger(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateTriggerInstance(ctx, newCreatePayload(ti.environmentID, "events-quiet"))
	require.NoError(t, err)

	result, err := ti.service.ListTriggerEvents(ctx, &gen.ListTriggerEventsPayload{
		ID:               created.ID,
		Limit:            50,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Events)
}

func TestListTriggerEvents_UnknownTriggerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListTriggerEvents(ctx, &gen.ListTriggerEventsPayload{
		ID:               uuid.NewString(),
		Limit:            50,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
}
