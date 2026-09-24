package anthropicinference

import (
	"testing"
	"time"

	"github.com/google/uuid"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
)

func TestInferenceJoinsComplianceConversation(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	now := conv.ToPGTimestamptz(time.Now())
	chatID, err := chatrepo.New(db).UpsertExternalChat(t.Context(), chatrepo.UpsertExternalChatParams{
		ID: uuid.New(), ProjectID: config.ProjectID, OrganizationID: config.OrganizationID,
		UserID: conv.ToPGText("known-user"), ExternalUserID: conv.ToPGText(frame.Actor.ID),
		ExternalChatID: conv.ToPGText(frame.SessionID), Title: conv.ToPGText("Provider title"),
		CreatedAt: now, UpdatedAt: now, PreferStoredTitle: false,
	})
	require.NoError(t, err)
	userID, err := store.ResolveActor(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "known-user", userID)
	saveFrame(t, store, config, frame, userID)
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ProjectID: config.ProjectID, ChatID: chatID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	conversation, err := chatrepo.New(db).GetChat(t.Context(), chatrepo.GetChatParams{ProjectID: config.ProjectID, ID: chatID})
	require.NoError(t, err)
	require.Equal(t, "Provider title", conversation.Title.String)
	frame.Actor.EmailAddress = ""
	userID, err = store.ResolveActor(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "known-user", userID)
	saveFrame(t, store, config, frame, userID)
}

func TestComplianceJoinsInferenceConversation(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	saveFrame(t, store, config, frame, "")
	now := conv.ToPGTimestamptz(time.Now())
	chatID, err := chatrepo.New(db).UpsertExternalChat(t.Context(), chatrepo.UpsertExternalChatParams{
		ID: uuid.New(), ProjectID: config.ProjectID, OrganizationID: config.OrganizationID,
		UserID: conv.ToPGText("known-user"), ExternalUserID: conv.ToPGText(frame.Actor.ID),
		ExternalChatID: conv.ToPGText(frame.SessionID), Title: conv.ToPGText("Provider title"),
		CreatedAt: now, UpdatedAt: now, PreferStoredTitle: false,
	})
	require.NoError(t, err)
	require.Equal(t, conversationID(config, frame), chatID)
}

func TestInferenceCannotJoinAnotherActorsConversation(t *testing.T) {
	t.Parallel()
	store, _, config := newTestStore(t)
	frame := exampleFrame()
	saveFrame(t, store, config, frame, "")
	frame.Actor.ID = "another-actor"
	frame.Actor.EmailAddress = "another@example.test"
	_, err := store.Save(t.Context(), config, frame, "")
	require.Error(t, err)
}

func TestInferenceCannotMoveConversationBetweenProjects(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	saveFrame(t, store, config, frame, "known-user")
	other, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other", OrganizationID: config.OrganizationID,
	})
	require.NoError(t, err)
	otherConfig := config
	otherConfig.ProjectID = other.ID
	_, err = store.Save(t.Context(), otherConfig, frame, "known-user")
	require.Error(t, err)
	_, err = chatrepo.New(db).GetChat(t.Context(), chatrepo.GetChatParams{ProjectID: config.ProjectID, ID: conversationID(config, frame)})
	require.NoError(t, err, "the original conversation must retain its project")
}

func TestInferenceAdoptsLegacyConversationWithoutChangingID(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	id := conversationID(config, frame)
	now := conv.ToPGTimestamptz(time.Now())
	_, err := chatrepo.New(db).UpsertExternalChat(t.Context(), chatrepo.UpsertExternalChatParams{
		ID: id, ProjectID: config.ProjectID, OrganizationID: config.OrganizationID,
		UserID: conv.ToPGText("known-user"), ExternalUserID: conv.ToPGText(frame.Actor.EmailAddress),
		ExternalChatID: conv.ToPGText("anthropic-inference:" + id.String()), Title: conv.ToPGText("Existing title"),
		CreatedAt: now, UpdatedAt: now, PreferStoredTitle: true,
	})
	require.NoError(t, err)
	saveFrame(t, store, config, frame, "known-user")
	conversation, err := chatrepo.New(db).GetChat(t.Context(), chatrepo.GetChatParams{ProjectID: config.ProjectID, ID: id})
	require.NoError(t, err)
	require.Equal(t, frame.SessionID, conversation.ExternalChatID.String)
	require.Equal(t, "Existing title", conversation.Title.String)
}
