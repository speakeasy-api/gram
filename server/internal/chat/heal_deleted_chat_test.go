package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedThreadBackedChat creates a chat backed by a live assistant thread.
func seedThreadBackedChat(t *testing.T, ctx context.Context, ti *chatTestInstance) uuid.UUID {
	t.Helper()
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Thread Chat")
	ar := assistantsrepo.New(ti.conn)
	a, err := ar.CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		Name:           "Test Assistant " + uuid.NewString()[:8],
		Model:          "anthropic/claude-opus-4.8",
		Instructions:   "be helpful",
		WarmTtlSeconds: 300,
		MaxConcurrency: 1,
		Status:         "active",
	})
	require.NoError(t, err)
	_, err = ar.UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   a.ID,
		ProjectID:     ti.projectID,
		CorrelationID: "corr-" + uuid.NewString()[:8],
		ChatID:        chatID,
		SourceKind:    "cron",
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)
	return chatID
}

// A chat that backs a live assistant thread self-heals when it receives another
// message (UpsertChat clears deleted_at), because the runtime keeps writing to
// it and a deleted chat wedges the thread. A plain chat the user intentionally
// deleted is NOT resurrected by a write.
func TestUpsertChat_HealsDeletedThreadBackedChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	r := repo.New(ti.conn)

	upsert := func(id uuid.UUID) {
		_, err := r.UpsertChat(ctx, repo.UpsertChatParams{
			ID:             id,
			ProjectID:      ti.projectID,
			OrganizationID: ti.orgID,
			Title:          pgtype.Text{String: "New Chat", Valid: true},
		})
		require.NoError(t, err)
	}

	// SoftDeleteChat refuses to delete a chat backing a live thread (that guard is
	// the point of the sibling PR). To exercise self-heal we have to land in the
	// wedged state some other way — legacy rows, direct DB manipulation, or a future
	// code path that bypasses the guard — so use the test-only bypass.
	tr := testrepo.New(ti.conn)

	// Thread-backed deleted chat heals on the next write.
	threadChatID := seedThreadBackedChat(t, ctx, ti)
	require.NoError(t, tr.ForceSoftDeleteChat(ctx, threadChatID))
	_, err := r.GetChat(ctx, threadChatID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "chat should be soft-deleted before the write")

	upsert(threadChatID)
	_, err = r.GetChat(ctx, threadChatID)
	require.NoError(t, err, "thread-backed chat should self-heal (deleted_at cleared)")

	// A plain chat with no thread stays deleted: a write must not resurrect a chat
	// the user intentionally deleted.
	plainChatID := seedChat(t, ctx, ti, "", "ext-user", "Plain Chat")
	_, err = r.SoftDeleteChat(ctx, repo.SoftDeleteChatParams{ID: plainChatID, ProjectID: ti.projectID})
	require.NoError(t, err)
	upsert(plainChatID)
	_, err = r.GetChat(ctx, plainChatID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "intentionally-deleted plain chat must stay deleted")
}
