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

// A member holding no chat:read grant can still load their own session — via
// owner-matching, and the open is recorded in the audit log — but is forbidden
// from loading another user's session.
func TestLoadChat_RBAC_MemberSelfAccess(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	own := seedChat(t, ctx, ti, authCtx.UserID, "", "my session")
	other := seedChat(t, ctx, ti, "someone-else", "", "their session")

	// RBAC active, no grants: the owner-bypass authorizes their own session.
	selfCtx := authztest.WithExactGrants(t, ctx)

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

// A member (no chat:read grant) cannot open an anonymous session — one with no
// internal owner (external / Elements chats). Owner-bypass requires a real
// owner match, so these fall through to the chat:read check, which members
// fail. Admins, holding an unrestricted chat:read, can still open them.
func TestLoadChat_RBAC_MemberCannotReadAnonymous(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	anon := seedChat(t, ctx, ti, "", "", "anonymous session")
	selfCtx := authztest.WithExactGrants(t, ctx)

	_, err := ti.service.LoadChat(selfCtx, loadPayload(anon.String()))
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

// Opening a session writes exactly one chat_session:access audit entry that
// records the actor, the session subject, and the session owner.
func TestLoadChat_AuditsSessionAccess(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "owner-user", "", "support thread")
	adminCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource))

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)

	_, err = ti.service.LoadChat(adminCtx, loadPayload(chatID.String()))
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "loading a session writes one access audit entry")

	rec, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionChatSessionAccess), rec.Action)
	require.Equal(t, "chat_session", rec.SubjectType)
	require.Equal(t, "support thread", rec.SubjectDisplay)
	require.Equal(t, "owner-user", rec.SubjectSlug, "audit records the session owner")
	require.True(t, rec.ProjectID.Valid)
	require.Equal(t, ti.projectID, rec.ProjectID.UUID)
}

// A Speakeasy admin impersonating an org via the dev-tools override is blocked
// from opening chat sessions outright, even though impersonation grants every
// scope, and no chat_session:access audit event is written.
func TestLoadChat_ImpersonatingAdminBlocked(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true

	other := seedChat(t, ctx, ti, "someone-else", "", "their session")

	adminCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource))
	adminCtx = contextvalues.SetAdminOverrideInContext(adminCtx, "customer-org")

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)

	_, err = ti.service.LoadChat(adminCtx, loadPayload(other.String()))
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, before, after, "blocked opens must not record an access audit event")
}

// A stray admin-override cookie on a non-admin session has no effect: auth
// ignores the override for non-admins, so LoadChat must not block them.
func TestLoadChat_OverrideWithoutAdminNotBlocked(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	own := seedChat(t, ctx, ti, authCtx.UserID, "", "my session")

	selfCtx := authztest.WithExactGrants(t, ctx)
	selfCtx = contextvalues.SetAdminOverrideInContext(selfCtx, "customer-org")

	res, err := ti.service.LoadChat(selfCtx, loadPayload(own.String()))
	require.NoError(t, err)
	require.Equal(t, own.String(), res.ID)
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
	selfCtx := authztest.WithExactGrants(t, ctx)

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
