package chat_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// The shared gate answers for a member's own session with no grant, and for
// nobody while impersonating: the support block runs before the owner rule, as
// it does when the transcript itself is opened.
func TestSessionAccess_AnswersForSession_SupportSessionRefused(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx, userID := memberSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	logger := testenv.NewLogger(t)
	access := chat.NewSessionAccess(logger, ti.conn, authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()))
	own := seedChat(t, ctx, ti, userID, "", "my session")

	answers, err := access.AnswersForSession(ctx, own)
	require.NoError(t, err)
	require.True(t, answers, "a member answers for their own session")

	support := *authCtx
	support.IsAdmin = true
	support.SupportOrganizationID = authCtx.ActiveOrganizationID
	supportCtx := contextvalues.WithValidatedSupportSession(ctx, &support)
	require.True(t, contextvalues.IsSupportSession(supportCtx))

	answers, err = access.AnswersForSession(supportCtx, own)
	require.NoError(t, err)
	require.False(t, answers, "impersonation answers for no session")
}
