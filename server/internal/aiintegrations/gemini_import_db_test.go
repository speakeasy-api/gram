package aiintegrations

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

func TestGeminiImportStoresTranscriptsIdempotentlyAndLinksOwner(t *testing.T) {
	t.Parallel()
	ctx, conn, store, orgID := newStoreTestDB(t)
	matterID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	created := upsertGeminiConfigWithTx(t, ctx, conn, store, orgID, geminiTestSettings(), true)
	cfg := created.Config
	user, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: "user_" + uuid.NewString(), Email: "person@example.com", DisplayName: "Example Person", PhotoUrl: conv.ToPGTextEmpty(""), Admin: false})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: orgID, UserID: conv.ToPGText(user.ID)}))
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	beats := 0
	svc := NewGeminiImportService(testenv.NewLogger(t), conn, nil, writer, func(context.Context, string, int) { beats++ })
	earlier := strings.ReplaceAll(syntheticGeminiDocument("person@example.com", "conversation-1", syntheticGeminiTurn), "Synthetic &amp; example", "Earlier title")
	laterTurn := strings.ReplaceAll(syntheticGeminiTurn, "2026-01-02T03:04:05+0000", "2026-01-03T03:04:05+0000")
	later := strings.ReplaceAll(syntheticGeminiDocument("person@example.com", "conversation-1", laterTurn), "Synthetic &amp; example", "Latest title")
	// Latest archive deliberately arrives first; creation and last-activity
	// metadata must not depend on Google's file ordering.
	files := []geminiExportFile{syntheticGeminiZIP(t,
		geminiExportFile{Name: "later.xml", Content: []byte(later)},
		geminiExportFile{Name: "earlier.xml", Content: []byte(earlier)},
	)}
	require.NoError(t, svc.writeExport(ctx, cfg, files))
	require.NoError(t, svc.writeExport(ctx, cfg, files))
	chats, err := chatrepo.New(conn).ListChats(ctx, chatrepo.ListChatsParams{ProjectID: cfg.ProjectID, ExternalUserID: "", UserID: "", Search: "", AssistantID: "", SortBy: "created_at", SortOrder: "desc", MinRiskScore: -1, PageLimit: 10})
	require.NoError(t, err)
	require.Len(t, chats, 1)
	chatID := chats[0].ID
	chatRow, err := chatrepo.New(conn).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: cfg.ProjectID})
	require.NoError(t, err)
	require.Equal(t, user.ID, chatRow.UserID.String)
	require.Equal(t, geminiExternalChatID(matterID, "person@example.com", "conversation-1"), chatRow.ExternalChatID.String)
	require.Equal(t, "Latest title", chatRow.Title.String)
	require.Equal(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), chatRow.CreatedAt.Time.UTC())
	require.Equal(t, time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC), chatRow.UpdatedAt.Time.UTC())
	messages, err := chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: cfg.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 4, "overlapping exports must not duplicate turns")
	roles := map[string]bool{}
	for _, message := range messages {
		roles[message.Role] = true
		require.Equal(t, user.ID, message.UserID.String)
		require.Equal(t, "person@example.com", message.ExternalUserID.String)
		require.Equal(t, "gemini", message.Source.String)
		require.Equal(t, "synthetic-model", message.Model.String)
	}
	require.Equal(t, map[string]bool{"user": true, "assistant": true}, roles)
	require.Positive(t, beats)
}
