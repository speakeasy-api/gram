package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// seedAssistantThread inserts an assistant and an active thread backed by
// chatID, returning the assistant's id.
func seedAssistantThread(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID) uuid.UUID {
	t.Helper()
	r := repo.New(ti.conn)
	assistantID, err := r.SeedAssistant(ctx, repo.SeedAssistantParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		Name:           "Test Assistant " + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	err = r.SeedAssistantThread(ctx, repo.SeedAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     ti.projectID,
		CorrelationID: "corr-" + uuid.NewString()[:8],
		ChatID:        chatID,
	})
	require.NoError(t, err)
	return assistantID
}

// A chat that backs an active assistant thread must not be deletable: the
// runtime reloads the conversation every turn, so soft-deleting it wedges the
// thread. A plain chat still deletes normally.
func TestService_DeleteChat_BlocksAssistantThreadChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	threadChatID := seedChat(t, ctx, ti, "", "ext-user", "Thread Chat")
	seedAssistantThread(t, ctx, ti, threadChatID)

	err := ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: threadChatID.String()})
	requireOopsCode(t, err, oops.CodeConflict)

	// Still present (GetChat filters deleted IS FALSE).
	_, err = repo.New(ti.conn).GetChat(ctx, threadChatID)
	require.NoError(t, err)

	plainChatID := seedChat(t, ctx, ti, "", "ext-user", "Plain Chat")
	err = ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: plainChatID.String()})
	require.NoError(t, err)

	// Now soft-deleted, so GetChat no longer returns it.
	_, err = repo.New(ti.conn).GetChat(ctx, plainChatID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// A chat whose only assistant has been soft-deleted IS deletable — the
	// leftover thread must not block cleanup forever (DeleteAssistant leaves
	// threads behind).
	deadChatID := seedChat(t, ctx, ti, "", "ext-user", "Dead Assistant Chat")
	deadAssistantID := seedAssistantThread(t, ctx, ti, deadChatID)
	require.NoError(t, repo.New(ti.conn).SeedSoftDeleteAssistant(ctx, deadAssistantID))

	err = ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: deadChatID.String()})
	require.NoError(t, err)

	_, err = repo.New(ti.conn).GetChat(ctx, deadChatID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
