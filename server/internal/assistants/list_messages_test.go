package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListMessages(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_list_messages")
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)

	// The RBAC harness authenticates as "user-test"; record turns under that
	// user so the ownership check passes.
	const correlation = "conv-1"
	_, err = svc.core.EnqueueTriggerTask(ctx, dashboardTask(managed.ID, correlation, "evt-1", "first question", "user-test"))
	require.NoError(t, err)
	_, err = svc.core.EnqueueTriggerTask(ctx, dashboardTask(managed.ID, correlation, "evt-2", "second question", "user-test"))
	require.NoError(t, err)

	chatID := deterministicChatID(managed.ID, correlation).String()

	full, err := svc.ListMessages(ctx, &gen.ListMessagesPayload{
		ChatID:           chatID,
		AfterSeq:         nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, full.Messages, 2)
	require.Equal(t, "user", full.Messages[0].Role)
	require.Equal(t, "first question", full.Messages[0].Content)
	require.Equal(t, "second question", full.Messages[1].Content)

	// after_seq returns only newer messages (poll cursor).
	afterFirst := full.Messages[0].Seq
	rest, err := svc.ListMessages(ctx, &gen.ListMessagesPayload{
		ChatID:           chatID,
		AfterSeq:         &afterFirst,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, rest.Messages, 1)
	require.Equal(t, "second question", rest.Messages[0].Content)
}

// A project member must not be able to read another user's conversation by its
// chat id (the conversation key is client-chosen and not project-unique).
func TestListMessagesRejectsCrossUserRead(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_list_messages_xuser")
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)

	// A different user owns this conversation.
	const correlation = "someone-elses-conv"
	_, err = svc.core.EnqueueTriggerTask(ctx, dashboardTask(managed.ID, correlation, "evt-1", "private", "other-user"))
	require.NoError(t, err)

	// The caller ("user-test") must not read it — surfaced as not-found to avoid
	// disclosing existence.
	_, err = svc.ListMessages(ctx, &gen.ListMessagesPayload{
		ChatID:           deterministicChatID(managed.ID, correlation).String(),
		AfterSeq:         nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A chat_id is a hash of the client-chosen correlation id, not namespaced by
// user, so two project members can write into the same chat_id. A reader must
// see only their own messages, never the other user's.
func TestListMessagesScopesCoMingledChatToCaller(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_list_messages_comingled")
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)

	// Both users thread under the same correlation id, so both land in the same
	// chat_id.
	const correlation = "shared-key"
	_, err = svc.core.EnqueueTriggerTask(ctx, dashboardTask(managed.ID, correlation, "evt-mine", "my secret", "user-test"))
	require.NoError(t, err)
	_, err = svc.core.EnqueueTriggerTask(ctx, dashboardTask(managed.ID, correlation, "evt-theirs", "their secret", "other-user"))
	require.NoError(t, err)

	got, err := svc.ListMessages(ctx, &gen.ListMessagesPayload{
		ChatID:           deterministicChatID(managed.ID, correlation).String(),
		AfterSeq:         nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 1, "caller must not see the other user's co-mingled message")
	require.Equal(t, "my secret", got.Messages[0].Content)
}

func TestListMessagesNotFound(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_list_messages_404")
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	_, err := svc.ListMessages(ctx, &gen.ListMessagesPayload{
		ChatID:           uuid.NewString(),
		AfterSeq:         nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListMessagesRequiresProjectGrant(t *testing.T) {
	t.Parallel()

	svc, ctx, _ := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx) // no grants

	_, err := svc.ListMessages(ctx, &gen.ListMessagesPayload{
		ChatID:           uuid.NewString(),
		AfterSeq:         nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
