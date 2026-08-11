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
	_, err = repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: threadChatID, ProjectID: ti.projectID})
	require.NoError(t, err)

	plainChatID := seedChat(t, ctx, ti, "", "ext-user", "Plain Chat")
	err = ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: plainChatID.String()})
	require.NoError(t, err)

	// Now soft-deleted, so GetChat no longer returns it.
	_, err = repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: plainChatID, ProjectID: ti.projectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// A chat whose only assistant has been soft-deleted IS deletable — the
	// leftover thread must not block cleanup forever (DeleteAssistant leaves
	// threads behind).
	deadChatID := seedChat(t, ctx, ti, "", "ext-user", "Dead Assistant Chat")
	deadAssistantID := seedAssistantThread(t, ctx, ti, deadChatID)
	require.NoError(t, repo.New(ti.conn).SeedSoftDeleteAssistant(ctx, deadAssistantID))

	err = ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: deadChatID.String()})
	require.NoError(t, err)

	_, err = repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: deadChatID, ProjectID: ti.projectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// Deleting is destructive, so a member who cannot read a session must not be
// able to delete it either. Before the shared gate, DeleteChat never read the
// row at all and any project member could delete any chat.
func TestService_DeleteChat_MemberCannotDeleteAnothersChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	seedCtx := initSessionCtx(t, ti)

	victim := seedChat(t, seedCtx, ti, "some-other-user", "", "their session")

	memberCtx, _ := memberSessionCtx(t, ti)
	err := ti.service.DeleteChat(memberCtx, &gen.DeleteChatPayload{ID: victim.String()})
	requireOopsCode(t, err, oops.CodeForbidden)

	// The row survives — a rejected delete must not have side effects.
	_, err = repo.New(ti.conn).GetChat(seedCtx, repo.GetChatParams{ID: victim, ProjectID: ti.projectID})
	require.NoError(t, err)
}

// The gate runs before the live-thread probe, so a member cannot use the
// conflict response to learn whether an arbitrary chat backs an assistant
// thread.
func TestService_DeleteChat_MemberGetsForbiddenNotConflict(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	seedCtx := initSessionCtx(t, ti)

	threadChatID := seedChat(t, seedCtx, ti, "some-other-user", "", "thread-backed session")
	seedAssistantThread(t, seedCtx, ti, threadChatID)

	memberCtx, _ := memberSessionCtx(t, ti)
	err := ti.service.DeleteChat(memberCtx, &gen.DeleteChatPayload{ID: threadChatID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// Deleting a chat that is already gone stays a success: the caller's desired
// end state holds, and the dashboard's delete mutation relies on that.
func TestService_DeleteChat_IdempotentForMissingAndAlreadyDeleted(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	require.NoError(t, ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: uuid.NewString()}))

	chatID := seedChat(t, ctx, ti, "", "ext-user", "Twice Deleted")
	require.NoError(t, ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: chatID.String()}))
	require.NoError(t, ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: chatID.String()}))
}

// A chat in another project is not deletable and, being indistinguishable from
// one that never existed, reports the same idempotent success.
func TestService_DeleteChat_CrossProjectLeavesChatIntact(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	otherProject := createProjectInSameOrg(t, ti)
	other := seedChatInProject(t, ti, otherProject, "other project session")

	ctx := initSessionCtx(t, ti)
	require.NoError(t, ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: other.String()}))

	_, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: other, ProjectID: otherProject})
	require.NoError(t, err, "the other project's chat must survive")
}
