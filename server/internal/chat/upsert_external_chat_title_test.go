package chat_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

// TestUpsertExternalChatTitleRegimes: two importers share this upsert with
// opposite title rules, and the SQL CASE is the only thing separating them.
// Feeds with authoritative titles (ChatGPT conversations, Anthropic compliance)
// are newest-wins; Codex cloud titles are DERIVED from the session's first
// prompt, so a later poll window would derive a mid-session prompt as its
// "first" and retitle the chat on every window.
//
// The importer also guards this in memory (titledSessions), but that map is
// per-run: on the next poll window it is empty and the CASE is the only
// defense. Exercised at the query so an inverted branch cannot pass.
func TestUpsertExternalChatTitleRegimes(t *testing.T) {
	t.Parallel()

	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	queries := repo.New(ti.conn)

	now := time.Now().UTC()
	chatID := uuid.New()
	externalChatID := "external-chat-" + uuid.NewString()

	upsert := func(t *testing.T, title string, preferStored bool) string {
		t.Helper()
		id, err := queries.UpsertExternalChat(ctx, repo.UpsertExternalChatParams{
			ID:                chatID,
			ProjectID:         ti.projectID,
			OrganizationID:    ti.orgID,
			UserID:            conv.ToPGTextEmpty(""),
			ExternalUserID:    conv.ToPGTextEmpty("opaque-provider-user-id"),
			ExternalChatID:    conv.ToPGText(externalChatID),
			Title:             conv.ToPGTextEmpty(title),
			CreatedAt:         conv.ToPGTimestamptz(now),
			UpdatedAt:         conv.ToPGTimestamptz(now),
			PreferStoredTitle: preferStored,
		})
		require.NoError(t, err)
		row, err := queries.GetChat(ctx, repo.GetChatParams{ID: id, ProjectID: ti.projectID})
		require.NoError(t, err)
		return row.Title.String
	}

	// A session whose opening prompt has not arrived yet is stored untitled.
	require.Empty(t, upsert(t, "", true))

	// first-wins backfills over a NULL title...
	require.Equal(t, "first prompt", upsert(t, "first prompt", true))

	// ...but a later prompt in the same session must not retitle it. This is
	// the assertion the in-memory guard hides.
	require.Equal(t, "first prompt", upsert(t, "a later prompt", true))

	// newest-wins refreshes instead: an authoritative feed renaming a
	// conversation must land.
	require.Equal(t, "renamed upstream", upsert(t, "renamed upstream", false))

	// Neither regime lets an absent incoming title erase a stored one.
	require.Equal(t, "renamed upstream", upsert(t, "", false))
	require.Equal(t, "renamed upstream", upsert(t, "", true))
}
