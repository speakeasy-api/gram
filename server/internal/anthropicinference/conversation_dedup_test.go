package anthropicinference

import (
	"github.com/jackc/pgx/v5"
	"testing"
	"time"

	"github.com/google/uuid"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

func TestInferenceCannotJoinComplianceUsingOnlyGramUser(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	now := conv.ToPGTimestamptz(time.Now())
	_, err := chatrepo.New(db).UpsertExternalChat(t.Context(), chatrepo.UpsertExternalChatParams{
		ID: uuid.New(), ProjectID: config.ProjectID, OrganizationID: config.OrganizationID,
		UserID: conv.ToPGText("known-user"), ExternalUserID: conv.ToPGText("different-provider-actor"),
		ExternalChatID: conv.ToPGText(frame.SessionID), Title: conv.ToPGText("Provider title"),
		CreatedAt: now, UpdatedAt: now, PreferStoredTitle: false,
	})
	require.NoError(t, err)
	_, err = store.Save(t.Context(), config, frame, "known-user")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestInferenceOptionalActorEvidence(t *testing.T) {
	t.Parallel()
	for _, first := range []string{"both", "id", "email"} {
		t.Run(first, func(t *testing.T) {
			t.Parallel()
			store, db, config := newTestStore(t)
			frame := exampleFrame()
			switch first {
			case "id":
				frame.Actor.EmailAddress = ""
			case "email":
				frame.Actor.ID = ""
			}
			id := conversationID(config, frame)
			saveFrame(t, store, config, frame, "known-user")
			// A signed frame with both fields bridges the two identities.
			frame = exampleFrame()
			saveFrame(t, store, config, frame, "known-user")
			for _, field := range []string{"id", "email", "id"} {
				frame = exampleFrame()
				if field == "id" {
					frame.Actor.EmailAddress = ""
				} else {
					frame.Actor.ID = ""
				}
				user, err := store.ResolveActor(t.Context(), config, frame)
				require.NoError(t, err)
				require.Equal(t, "known-user", user)
				saveFrame(t, store, config, frame, user)
			}
			messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ProjectID: config.ProjectID, ChatID: id})
			require.NoError(t, err)
			require.Len(t, messages, 1)
		})
	}
}

func TestInferenceLegacyAdoptionRacesComplianceImport(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	legacyID := conversationID(config, frame)
	now := conv.ToPGTimestamptz(time.Now())
	params := chatrepo.UpsertExternalChatParams{
		ID: legacyID, ProjectID: config.ProjectID, OrganizationID: config.OrganizationID,
		UserID: conv.ToPGText("known-user"), ExternalUserID: conv.ToPGText(frame.Actor.ID),
		ExternalChatID: conv.ToPGText("anthropic-inference:" + legacyID.String()), Title: conv.ToPGText("Existing title"),
		CreatedAt: now, UpdatedAt: now, PreferStoredTitle: false,
	}
	_, err := chatrepo.New(db).UpsertExternalChat(t.Context(), params)
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), db)
	params.ID = uuid.New()
	params.ExternalChatID = conv.ToPGText(frame.SessionID)
	canonicalID, err := chatrepo.New(tx).UpsertExternalChat(t.Context(), params)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		_, err := store.Save(t.Context(), config, frame, "known-user")
		result <- err
	}()
	// The uncommitted import is invisible to NOT EXISTS, but its unique-index
	// claim blocks adoption. Commit only after that interleaving is established.
	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(db).IsQueryBlockedOnLockFixture(t.Context(), "-- name: AdoptLegacyInferenceConversation%")
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit(t.Context()))
	require.NoError(t, <-result)
	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ProjectID: config.ProjectID, ChatID: canonicalID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
}

func TestInferenceAdoptsEmailFirstLegacyConversation(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Actor.ID = ""
	id := conversationID(config, frame)
	now := conv.ToPGTimestamptz(time.Now())
	_, err := chatrepo.New(db).UpsertExternalChat(t.Context(), chatrepo.UpsertExternalChatParams{
		ID: id, ProjectID: config.ProjectID, OrganizationID: config.OrganizationID,
		UserID: conv.ToPGText("known-user"), ExternalUserID: conv.ToPGText(frame.Actor.EmailAddress),
		ExternalChatID: conv.ToPGText("anthropic-inference:" + id.String()), Title: conv.ToPGText("Existing title"),
		CreatedAt: now, UpdatedAt: now, PreferStoredTitle: true,
	})
	require.NoError(t, err)
	frame = exampleFrame()
	user, err := store.ResolveActor(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "known-user", user)
	saveFrame(t, store, config, frame, user)
	conversation, err := chatrepo.New(db).GetChat(t.Context(), chatrepo.GetChatParams{ProjectID: config.ProjectID, ID: id})
	require.NoError(t, err)
	require.Equal(t, frame.SessionID, conversation.ExternalChatID.String)
	require.Equal(t, "Existing title", conversation.Title.String)
}

func TestInferenceRejectsEmailOnlyAfterComplianceReplacesActorLabel(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	saveFrame(t, store, config, frame, "known-user")
	now := conv.ToPGTimestamptz(time.Now())
	_, err := chatrepo.New(db).UpsertExternalChat(t.Context(), chatrepo.UpsertExternalChatParams{
		ID: uuid.New(), ProjectID: config.ProjectID, OrganizationID: config.OrganizationID,
		UserID: conv.ToPGText("known-user"), ExternalUserID: conv.ToPGText(frame.Actor.ID),
		ExternalChatID: conv.ToPGText(frame.SessionID), Title: conv.ToPGText("Provider title"),
		CreatedAt: now, UpdatedAt: now, PreferStoredTitle: false,
	})
	require.NoError(t, err)
	frame.Actor.ID = ""
	_, err = store.Save(t.Context(), config, frame, "known-user")
	require.ErrorIs(t, err, pgx.ErrNoRows, "a Gram user match cannot replace missing signed actor evidence")
}
