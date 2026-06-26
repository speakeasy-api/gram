package chat_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// A member holding only a self-scoped chat:read grant can load their own
// session — and the open is recorded in the audit log — but is forbidden from
// loading another user's session.
func TestLoadChat_RBAC_MemberSelfAccess(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	own := seedChat(t, ctx, ti, authCtx.UserID, "", "my session")
	other := seedChat(t, ctx, ti, "someone-else", "", "their session")

	selfCtx := authztest.WithExactGrants(t, ctx, authz.ChatReadSelfGrant(authCtx.UserID))

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)

	res, err := ti.service.LoadChat(selfCtx, loadPayload(own.String()))
	require.NoError(t, err)
	require.Equal(t, own.String(), res.ID)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "loading a session records an access audit event")

	rec, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, "chat_session", rec.SubjectType)
	require.Equal(t, "my session", rec.SubjectDisplay)
	require.Equal(t, authCtx.UserID, rec.SubjectSlug, "audit records the session owner")

	_, err = ti.service.LoadChat(selfCtx, loadPayload(other.String()))
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
}

// An admin holding an unrestricted chat:read grant can load any user's session.
func TestLoadChat_RBAC_AdminReadsAll(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	other := seedChat(t, ctx, ti, "someone-else", "", "their session")
	adminCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource))

	res, err := ti.service.LoadChat(adminCtx, loadPayload(other.String()))
	require.NoError(t, err)
	require.Equal(t, other.String(), res.ID)
}

// Scroll pagination (before_seq/after_seq) does not emit additional audit
// events: only the initial open of a session is recorded.
func TestLoadChat_RBAC_PaginationNotReAudited(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	own := seedChat(t, ctx, ti, authCtx.UserID, "", "my session")
	ids := seedNMessages(t, ctx, ti, own, 5)
	selfCtx := authztest.WithExactGrants(t, ctx, authz.ChatReadSelfGrant(authCtx.UserID))

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)

	// Initial open: audited.
	first, err := ti.service.LoadChat(selfCtx, loadPayload(own.String()))
	require.NoError(t, err)
	require.NotEmpty(t, first.Messages)

	// Scroll up with a before_seq cursor: same open, not re-audited.
	p := loadPayload(own.String())
	p.BeforeSeq = &first.Messages[0].Seq
	_, err = ti.service.LoadChat(selfCtx, p)
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "pagination must not emit extra access audit events")
	_ = ids
}
