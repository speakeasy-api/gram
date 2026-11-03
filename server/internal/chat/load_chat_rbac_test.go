package chat_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	authCtx.SupportOrganizationID = authCtx.ActiveOrganizationID
	ctx = contextvalues.WithValidatedSupportSession(ctx, authCtx)

	other := seedChat(t, ctx, ti, "someone-else", "", "their session")

	adminCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource))

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

// The shared demo org is exempt from the impersonation transcript block: its
// sessions are fabricated by seed/demo/ and exist to be read. Non-demo orgs
// stay blocked (covered above).
func TestLoadChat_ImpersonatingAdminAllowedForDemoOrg(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	authCtx.ActiveOrganizationID = constants.DemoOrganizationID
	authCtx.SupportOrganizationID = constants.DemoOrganizationID
	ctx = contextvalues.WithValidatedSupportSession(ctx, authCtx)

	// The audit log FK requires the demo org row to exist.
	err := testrepo.New(ti.conn).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 constants.DemoOrganizationID,
		Name:               "Acme Demo Workspace",
		Slug:               "acme-demo",
		GramAccountType:    "demo",
		WorkosID:           pgtype.Text{String: "", Valid: false},
		Whitelisted:        true,
		FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now().UTC()),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(time.Now().UTC().Add(14 * 24 * time.Hour)),
		DisabledAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
	})
	require.NoError(t, err)

	other := seedChat(t, ctx, ti, "someone-else", "", "demo session")

	adminCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource))

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)

	res, err := ti.service.LoadChat(adminCtx, loadPayload(other.String()))
	require.NoError(t, err)
	require.Equal(t, other.String(), res.ID)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "demo transcript opens must record an access audit event")
}

func TestLoadChat_LegacyImpersonationAllowedForDemoOrg(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	authCtx.ActiveOrganizationID = constants.DemoOrganizationID
	authCtx.SupportOrganizationID = constants.DemoOrganizationID
	ctx = contextvalues.WithValidatedGramSession(ctx, authCtx, true)

	// The audit log FK requires the demo org row to exist.
	err := testrepo.New(ti.conn).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 constants.DemoOrganizationID,
		Name:               "Acme Demo Workspace",
		Slug:               "acme-demo",
		GramAccountType:    "demo",
		WorkosID:           pgtype.Text{String: "", Valid: false},
		Whitelisted:        true,
		FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now().UTC()),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(time.Now().UTC().Add(14 * 24 * time.Hour)),
		DisabledAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
	})
	require.NoError(t, err)

	other := seedChat(t, ctx, ti, "someone-else", "", "demo session")

	adminCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource))

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)

	res, err := ti.service.LoadChat(adminCtx, loadPayload(other.String()))
	require.NoError(t, err)
	require.Equal(t, other.String(), res.ID)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "demo transcript opens must record an access audit event")
}

// An agent's session has no user owner; the agent's owner stands in for one,
// so a member without chat:read can open what their agent did — but not
// another owner's agent's session.
func TestLoadChat_RBAC_AgentOwnerReadsAgentSession(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	owned := seedAgentChat(t, ctx, ti, seedAgent(t, ctx, ti, authCtx.UserID, "My Agent"), "my agent's session")
	foreign := seedAgentChat(t, ctx, ti, seedAgent(t, ctx, ti, "other-owner", "Their Agent"), "their agent's session")
	selfCtx := authztest.WithExactGrants(t, ctx)

	res, err := ti.service.LoadChat(selfCtx, loadPayload(owned.String()))
	require.NoError(t, err)
	require.Equal(t, owned.String(), res.ID)

	overview, err := ti.service.LoadChatOverview(selfCtx, &gen.LoadChatOverviewPayload{ID: owned.String()})
	require.NoError(t, err)
	require.Equal(t, owned.String(), overview.ID)

	_, err = ti.service.LoadChat(selfCtx, loadPayload(foreign.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A session that carries a human identity stays that human's even when an
// agent the caller owns was stamped onto it: owning the agent does not open it.
func TestLoadChat_RBAC_AgentOwnerCannotReadHumanSession(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	agentID := seedAgent(t, ctx, ti, authCtx.UserID, "My Agent")
	selfCtx := authztest.WithExactGrants(t, ctx)
	for name, chatID := range map[string]uuid.UUID{
		"user_id":          seedChat(t, ctx, ti, "other-user-id", "", "human session"),
		"external_user_id": seedChat(t, ctx, ti, "", "someone@example.com", "external session"),
	} {
		linked, err := hooksrepo.New(ti.conn).LinkChatActingAgent(ctx, hooksrepo.LinkChatActingAgentParams{
			AgentID:   uuid.NullUUID{UUID: agentID, Valid: true},
			ID:        chatID,
			ProjectID: ti.projectID,
		})
		require.NoError(t, err, name)
		require.Equal(t, int64(1), linked, name)

		_, err = ti.service.LoadChat(selfCtx, loadPayload(chatID.String()))
		requireOopsCode(t, err, oops.CodeForbidden)
	}
}

// An owner whose agent is latched for reassignment no longer answers for it,
// so its sessions stop opening for them on the load path too.
func TestLoadChat_RBAC_LatchedAgentOwnerCannotReadAgentSession(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	chatID := seedAgentChat(t, ctx, ti, seedAgent(t, ctx, ti, authCtx.UserID, "Latched Agent"), "latched agent's session")
	_, err := agentrepo.New(ti.conn).LatchAgentsForOwnerLossByMembership(ctx, agentrepo.LatchAgentsForOwnerLossByMembershipParams{
		OwnerReassignmentReason: conv.ToPGText("membership_loss"),
		OrganizationID:          ti.orgID,
		OwnerUserID:             authCtx.UserID,
	})
	require.NoError(t, err)

	_, err = ti.service.LoadChat(authztest.WithExactGrants(t, ctx), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// Standing in for the owner covers reading only: deleting an agent's session
// still needs chat:write.
func TestDeleteChat_RBAC_AgentOwnerCannotDeleteAgentSession(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	owned := seedAgentChat(t, ctx, ti, seedAgent(t, ctx, ti, authCtx.UserID, "My Agent"), "my agent's session")
	selfCtx := authztest.WithExactGrants(t, ctx)

	err := ti.service.DeleteChat(selfCtx, &gen.DeleteChatPayload{ID: owned.String()})
	requireOopsCode(t, err, oops.CodeForbidden)

	res, err := ti.service.LoadChat(selfCtx, loadPayload(owned.String()))
	require.NoError(t, err)
	require.Equal(t, owned.String(), res.ID)
}

// A non-admin can load their own chat with the ordinary user grant.
func TestLoadChat_NonAdminOwnChatNotBlocked(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	own := seedChat(t, ctx, ti, authCtx.UserID, "", "my session")

	selfCtx := authztest.WithExactGrants(t, ctx)

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
