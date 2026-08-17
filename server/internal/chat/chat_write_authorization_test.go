package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// summarizingCompletionClient answers any summarize call, for the cases that
// are expected to reach the model. The cases that must NOT reach it use a bare
// mockCompletionClient with no expectation registered, so a call fails the test.
func summarizingCompletionClient(t *testing.T) *mockCompletionClient {
	t.Helper()
	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse("A session summary."), nil).
		Maybe()
	return client
}

// memberSessionCtx returns a dashboard session with RBAC active and NO grants —
// the ordinary project member, who holds no chat:read. Every other context
// helper in this package grants chat:read, which is precisely why the gaps this
// file covers went unnoticed: the fixtures all authenticated as a chat:read
// holder, for whom the endpoints behaved correctly.
func memberSessionCtx(t *testing.T, ti *chatTestInstance) (context.Context, string) {
	t.Helper()
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return authztest.WithExactGrants(t, ctx), authCtx.UserID
}

// chatEndpoint names one per-chat operation and how to invoke it, so the access
// rule can be asserted uniformly across all of them.
type chatEndpoint struct {
	name string
	// write is true when the endpoint mutates the session, and so needs
	// chat:write rather than chat:read for a non-owner.
	write bool
	call  func(ctx context.Context, ti *chatTestInstance, chatID string) error
}

// errOnly drops a call's result so endpoints that return a value and those that
// return only an error share the one signature the table below needs. The error
// is passed through unchanged: these tests assert on its oops code, so wrapping
// it would only add a layer for the assertion to see through.
func errOnly[T any](_ T, err error) error {
	return err
}

// allChatEndpoints is every endpoint that resolves a caller-supplied chat id.
// A new one that forgets loadAuthorizedChat fails the tests below — add it here
// when adding the endpoint.
func allChatEndpoints() []chatEndpoint {
	manualTitle := "renamed by someone else"
	return []chatEndpoint{
		{"loadChat", false, func(ctx context.Context, ti *chatTestInstance, id string) error {
			return errOnly(ti.service.LoadChat(ctx, loadPayload(id)))
		}},
		{"generateTitle/read", false, func(ctx context.Context, ti *chatTestInstance, id string) error {
			return errOnly(ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: id}))
		}},
		{"generateTitle/rename", true, func(ctx context.Context, ti *chatTestInstance, id string) error {
			return errOnly(ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: id, Title: &manualTitle}))
		}},
		{"setPinned", true, func(ctx context.Context, ti *chatTestInstance, id string) error {
			return ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: id, Pinned: true})
		}},
		{"summarize", false, func(ctx context.Context, ti *chatTestInstance, id string) error {
			return errOnly(ti.service.Summarize(ctx, &gen.SummarizePayload{ID: id}))
		}},
		{"submitFeedback", true, func(ctx context.Context, ti *chatTestInstance, id string) error {
			return errOnly(ti.service.SubmitFeedback(ctx, &gen.SubmitFeedbackPayload{ID: id, Feedback: "failure"}))
		}},
		{"deleteChat", true, func(ctx context.Context, ti *chatTestInstance, id string) error {
			return ti.service.DeleteChat(ctx, &gen.DeleteChatPayload{ID: id})
		}},
	}
}

// The AIS-424 regression guard. A member who holds no chat:read must not be
// able to touch another user's session in the same project by supplying its id
// — not read it, not summarize it, not rename, pin, delete, or attach feedback
// to it. Before the shared gate, only loadChat enforced this.
func TestChatEndpoints_MemberCannotActOnAnotherUsersChat(t *testing.T) {
	t.Parallel()

	for _, ep := range allChatEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatServiceWithCompletion(t, &mockCompletionClient{})
			seedCtx := initSessionCtx(t, ti)
			victim := seedChat(t, seedCtx, ti, "some-other-user", "", "their private session")
			seedNMessages(t, seedCtx, ti, victim, 2)

			memberCtx, _ := memberSessionCtx(t, ti)
			requireOopsCode(t, ep.call(memberCtx, ti, victim.String()), oops.CodeForbidden)
		})
	}
}

// The same member acting on a session they own is unaffected: owner-matching
// authorizes them without any chat:read grant. This is the guard against
// over-tightening — the dashboard's own pin/rename/summarize/delete controls
// operate on sessions the caller owns.
func TestChatEndpoints_MemberCanActOnOwnChat(t *testing.T) {
	t.Parallel()

	for _, ep := range allChatEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatServiceWithCompletion(t, summarizingCompletionClient(t))
			memberCtx, userID := memberSessionCtx(t, ti)

			own := seedChat(t, memberCtx, ti, userID, "", "my own session")
			seedMessageContent(t, memberCtx, ti, own, "please deploy the API to staging")

			require.NoError(t, ep.call(memberCtx, ti, own.String()))
		})
	}
}

// A chat in another project of the same org is indistinguishable from one that
// does not exist: GetChat is project-scoped, so there is no cross-project
// existence oracle on any of these endpoints.
func TestChatEndpoints_CrossProjectIsNotFound(t *testing.T) {
	t.Parallel()

	for _, ep := range allChatEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatServiceWithCompletion(t, &mockCompletionClient{})
			other := seedChatInProject(t, ti, createProjectInSameOrg(t, ti), "other project session")

			ctx := initSessionCtx(t, ti)
			err := ep.call(ctx, ti, other.String())
			if ep.name == "deleteChat" {
				// Delete stays idempotent: a chat the caller cannot see is
				// reported the same as one already gone.
				require.NoError(t, err)
				return
			}
			requireOopsCode(t, err, oops.CodeNotFound)
		})
	}
}

// The session-reviewer role: chat:read opens every transcript in the project,
// which is what the Agent Sessions page relies on — but it must NOT convey the
// power to delete, rename, pin, or annotate someone else's session. That is
// chat:write's job, and it is the reason the two scopes are separate.
func TestChatEndpoints_ChatReadHolderCanReadButNotWrite(t *testing.T) {
	t.Parallel()

	for _, ep := range allChatEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatServiceWithCompletion(t, summarizingCompletionClient(t))
			seedCtx := initSessionCtx(t, ti)
			someoneElses := seedChat(t, seedCtx, ti, "some-other-user", "", "their session")
			seedMessageContent(t, seedCtx, ti, someoneElses, "please deploy the API to staging")

			ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))
			err := ep.call(ctx, ti, someoneElses.String())
			if ep.write {
				requireOopsCode(t, err, oops.CodeForbidden)
				return
			}
			require.NoError(t, err)
		})
	}
}

// chat:write covers the mutations and, by scope expansion, the reads too — so a
// role carrying it alone can do everything to any session in the project.
func TestChatEndpoints_ChatWriteHolderCanActOnAnyChat(t *testing.T) {
	t.Parallel()

	for _, ep := range allChatEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatServiceWithCompletion(t, summarizingCompletionClient(t))
			seedCtx := initSessionCtx(t, ti)
			someoneElses := seedChat(t, seedCtx, ti, "some-other-user", "", "their session")
			seedMessageContent(t, seedCtx, ti, someoneElses, "please deploy the API to staging")

			ctx := grantOrgAdminWithChatWrite(t, initSessionCtx(t, ti))
			require.NoError(t, ep.call(ctx, ti, someoneElses.String()))
		})
	}
}

// An unknown chat id is a plain not-found on every endpoint, so a member cannot
// distinguish "does not exist" from "exists but is not yours" by id probing.
func TestChatEndpoints_MissingChatIsNotFound(t *testing.T) {
	t.Parallel()

	for _, ep := range allChatEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatServiceWithCompletion(t, &mockCompletionClient{})
			memberCtx, _ := memberSessionCtx(t, ti)

			err := ep.call(memberCtx, ti, uuid.NewString())
			if ep.name == "deleteChat" {
				require.NoError(t, err)
				return
			}
			requireOopsCode(t, err, oops.CodeNotFound)
		})
	}
}
