package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// countFeedback reports how many feedback rows exist for a chat, so the tests
// can assert a rejected submission wrote nothing rather than only that it
// returned an error.
func countFeedback(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID) int {
	t.Helper()
	rows, err := repo.New(ti.conn).ListUserFeedbackForChat(ctx, repo.ListUserFeedbackForChatParams{
		ChatID:    chatID,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	return len(rows)
}

func TestService_SubmitFeedback_RecordsFeedback(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "", "ext-user", "Deploy session")
	seedNMessages(t, ctx, ti, chatID, 2)

	res, err := ti.service.SubmitFeedback(ctx, &gen.SubmitFeedbackPayload{ID: chatID.String(), Feedback: "success"})
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, 1, countFeedback(t, ctx, ti, chatID))
}

// The AIS-424 case for this endpoint: feedback is a write against someone
// else's session, so it needs the same grant reading it does.
func TestService_SubmitFeedback_MemberCannotSubmitOnAnothersChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	seedCtx := initSessionCtx(t, ti)

	victim := seedChat(t, seedCtx, ti, "some-other-user", "", "their session")
	seedNMessages(t, seedCtx, ti, victim, 2)

	memberCtx, _ := memberSessionCtx(t, ti)
	_, err := ti.service.SubmitFeedback(memberCtx, &gen.SubmitFeedbackPayload{ID: victim.String(), Feedback: "failure"})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, 0, countFeedback(t, seedCtx, ti, victim), "a rejected submission must not write a row")
}

// submitFeedback declares no method-level Security, so it inherits the
// service-level ChatSessionsToken scheme and is reachable with an embedded
// end-user token. That token must still match the chat's external user.
func TestService_SubmitFeedback_ExternalTokenOwnerMismatch(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	seedCtx := initSessionCtx(t, ti)

	chatID := seedChat(t, seedCtx, ti, "", "external-user-A", "user A session")
	seedNMessages(t, seedCtx, ti, chatID, 2)

	_, err := ti.service.SubmitFeedback(
		chatSessionTokenCtx(t, ti, "external-user-B"),
		&gen.SubmitFeedbackPayload{ID: chatID.String(), Feedback: "failure"},
	)
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.Equal(t, 0, countFeedback(t, seedCtx, ti, chatID))
}

// The matching external user is the real Elements path and must keep working.
func TestService_SubmitFeedback_ExternalTokenOwnerMatch(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	seedCtx := initSessionCtx(t, ti)

	chatID := seedChat(t, seedCtx, ti, "", "external-user-A", "user A session")
	seedNMessages(t, seedCtx, ti, chatID, 2)

	res, err := ti.service.SubmitFeedback(
		chatSessionTokenCtx(t, ti, "external-user-A"),
		&gen.SubmitFeedbackPayload{ID: chatID.String(), Feedback: "success"},
	)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, 1, countFeedback(t, seedCtx, ti, chatID))
}

func TestService_SubmitFeedback_MissingChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	_, err := ti.service.SubmitFeedback(ctx, &gen.SubmitFeedbackPayload{ID: uuid.NewString(), Feedback: "success"})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestService_SubmitFeedback_CrossProject(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	other := seedChatInProject(t, ti, createProjectInSameOrg(t, ti), "other project session")

	ctx := initSessionCtx(t, ti)
	_, err := ti.service.SubmitFeedback(ctx, &gen.SubmitFeedbackPayload{ID: other.String(), Feedback: "success"})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestService_SubmitFeedback_InvalidValue(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "", "ext-user", "Session")
	seedNMessages(t, ctx, ti, chatID, 1)

	_, err := ti.service.SubmitFeedback(ctx, &gen.SubmitFeedbackPayload{ID: chatID.String(), Feedback: "maybe"})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestService_SubmitFeedback_NoMessages(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "", "ext-user", "Empty session")

	_, err := ti.service.SubmitFeedback(ctx, &gen.SubmitFeedbackPayload{ID: chatID.String(), Feedback: "success"})
	requireOopsCode(t, err, oops.CodeInvalid)
}
