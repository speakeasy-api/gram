package anthropicinference

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// hookCapturedSession creates the chat an agent hook stream writes a session's
// transcript to: keyed by the chat id derived from the harness session id,
// with no external chat id. The label is the user the hook stream resolved.
func hookCapturedSession(t *testing.T, db chatrepo.DBTX, config Config, sessionID, label string) uuid.UUID {
	t.Helper()
	id, err := chatrepo.New(db).UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID:             chat.SessionIDToChatID(sessionID),
		ProjectID:      config.ProjectID,
		OrganizationID: config.OrganizationID,
		UserID:         conv.ToPGTextEmpty(""),
		ExternalUserID: conv.ToPGTextEmpty(label),
		Title:          conv.ToPGText("Claude Code Session"),
	})
	require.NoError(t, err)
	return id
}

// importedConversation creates the chat a compliance import writes a
// provider-hosted conversation to: keyed by the provider's chat id and
// labelled with the provider's own account id.
func importedConversation(t *testing.T, db chatrepo.DBTX, config Config, providerChatID, label string) uuid.UUID {
	t.Helper()
	id, err := chatrepo.New(db).UpsertExternalChat(t.Context(), chatrepo.UpsertExternalChatParams{
		ID:             uuid.New(),
		ProjectID:      config.ProjectID,
		OrganizationID: config.OrganizationID,
		ExternalUserID: conv.ToPGTextEmpty(label),
		ExternalChatID: conv.ToPGText(providerChatID),
		Title:          conv.ToPGText("Imported conversation"),
		CreatedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
		UpdatedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)
	return id
}

func TestStoreAdoptsHookCapturedSession(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Source.Application = "claude-code"
	frame.SessionID = uuid.NewString()
	hookChatID := hookCapturedSession(t, db, config, frame.SessionID, frame.Actor.EmailAddress)

	saveFrame(t, store, config, frame, "")

	queries := chatrepo.New(db)
	_, err := queries.GetChat(t.Context(), chatrepo.GetChatParams{ID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.ErrorContains(t, err, "no rows", "the hook stream already stores this session; inference must not open a second conversation")
	messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: hookChatID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Empty(t, messages, "the hook stream's own transcript must not gain a second copy of the frame")
}

func TestStoreAdoptsImportedConversation(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.SessionID = uuid.NewString()
	importedChatID := importedConversation(t, db, config, frame.SessionID, frame.Actor.ID)

	saveFrame(t, store, config, frame, "")

	queries := chatrepo.New(db)
	_, err := queries.GetChat(t.Context(), chatrepo.GetChatParams{ID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.ErrorContains(t, err, "no rows")
	messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: importedChatID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestStoreArchivesSessionOwnedByAnotherActor(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.SessionID = uuid.NewString()
	// Session identifiers can be client asserted. Asserting somebody else's
	// must not keep this actor's transcript out of the record.
	hookCapturedSession(t, db, config, frame.SessionID, "colleague@example.test")

	saveFrame(t, store, config, frame, "")

	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
}

func TestStoreAdoptsSessionWithNothingContradictingTheActor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		label string
		email string
	}{
		{name: "hook stream never resolved a user", label: "", email: "user@example.test"},
		// A later frame of an adopted session that omits the actor email must
		// keep adopting it rather than splitting the transcript in two.
		{name: "frame omits the actor email", label: "user@example.test", email: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, db, config := newTestStore(t)
			frame := exampleFrame()
			frame.SessionID = uuid.NewString()
			frame.Actor.EmailAddress = tc.email
			hookChatID := hookCapturedSession(t, db, config, frame.SessionID, tc.label)

			saveFrame(t, store, config, frame, "")

			queries := chatrepo.New(db)
			_, err := queries.GetChat(t.Context(), chatrepo.GetChatParams{ID: conversationID(config, frame), ProjectID: config.ProjectID})
			require.ErrorContains(t, err, "no rows")
			messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: hookChatID, ProjectID: config.ProjectID})
			require.NoError(t, err)
			require.Empty(t, messages)
		})
	}
}

func TestStoreArchivesSessionOwnedByAnotherResolvedUser(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.SessionID = uuid.NewString()
	sessionChatID := chat.SessionIDToChatID(frame.SessionID)
	_, err := chatrepo.New(db).UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID: sessionChatID, ProjectID: config.ProjectID, OrganizationID: config.OrganizationID,
		UserID: conv.ToPGTextEmpty("colleague-user"), ExternalUserID: conv.ToPGTextEmpty(""),
		Title: conv.ToPGText("Claude Code Session"),
	})
	require.NoError(t, err)

	saveFrame(t, store, config, frame, "actor-user")

	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
}

func TestStoreKeepsConversationItAlreadyArchived(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.SessionID = uuid.NewString()
	saveFrame(t, store, config, frame, "")
	// A hook stream that only reaches this session mid-conversation must not
	// split its transcript across two conversations.
	hookCapturedSession(t, db, config, frame.SessionID, frame.Actor.EmailAddress)
	frame.Messages = append(frame.Messages, textMessage("assistant", "reply"), textMessage("user", "second prompt"))

	saveFrame(t, store, config, frame, "")

	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 3)
}

func TestStoreArchivesSessionCapturedInAnotherProject(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	other, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Other Example", Slug: "other-example", OrganizationID: config.OrganizationID})
	require.NoError(t, err)
	frame := exampleFrame()
	frame.SessionID = uuid.NewString()
	otherProject := config
	otherProject.ProjectID = other.ID
	hookCapturedSession(t, db, otherProject, frame.SessionID, frame.Actor.EmailAddress)

	saveFrame(t, store, config, frame, "")

	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1, "a session captured under another project's policies is a different tenant's transcript")
}

func TestAdoptedTranscriptIsStillEnforcedAndCheckpointed(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Source.Application = "claude-code"
	frame.SessionID = uuid.NewString()
	frame.Messages = []Message{textMessage("user", "first prompt"), textMessage("assistant", "reply"), textMessage("user", "second prompt")}
	hookChatID := hookCapturedSession(t, db, config, frame.SessionID, frame.Actor.EmailAddress)
	scanner := &recordingScanner{}
	service := NewService(testenv.NewLogger(t), db, store.writer, scanner)

	verdict, err := service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Len(t, scanner.inputs, 3, "suppressing a duplicate archive must not suppress enforcement")

	// The acceptance marker lives on the conversation the delivery was bound
	// to, so the next delivery still only scans the current turn.
	scanner.reset()
	frame.Messages = append(frame.Messages, textMessage("assistant", "second reply"), textMessage("user", "third prompt"))
	verdict, err = service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Len(t, scanner.inputs, 2)
	require.Equal(t, "second reply", scanner.inputs[0].text)
	require.Equal(t, "third prompt", scanner.inputs[1].text)

	queries := chatrepo.New(db)
	marker, err := queries.GetInferenceAcceptedCheckpoint(t.Context(), chatrepo.GetInferenceAcceptedCheckpointParams{ProjectID: config.ProjectID, ChatID: hookChatID})
	require.NoError(t, err)
	require.NotEmpty(t, marker)
	messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: hookChatID, ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestRequestScopedFrameIsNeverAdopted(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.SessionID = ""
	// A chat id derived from the empty session id exists in the project.
	hookCapturedSession(t, db, config, "", frame.Actor.EmailAddress)

	saveFrame(t, store, config, frame, "")

	messages, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
}
