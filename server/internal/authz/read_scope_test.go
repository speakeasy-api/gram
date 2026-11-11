package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const readScopeProject = "proj_123"

func readScopeMemberCtx(t *testing.T, orgID string, mutate func(*contextvalues.AuthContext)) context.Context {
	t.Helper()
	ctx := enterpriseSessionCtxWithOrg(t, orgID)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.Email = conv.PtrEmpty("member@example.com")
	if mutate != nil {
		mutate(authCtx)
	}
	return contextvalues.SetAuthContext(ctx, authCtx)
}

func TestResolveReadScope_GrantHolderReadsWholeProject(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(readScopeMemberCtx(t, "org_123", nil), []Grant{NewGrant(ScopeLogRead, readScopeProject)})

	scope, err := engine.ResolveReadScope(ctx, ScopeLogRead, readScopeProject)
	require.NoError(t, err)
	require.True(t, scope.Unrestricted())
	require.Empty(t, scope.AgentIDs)
}

func TestResolveReadScope_UnenforcedReadsWholeProject(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(readScopeMemberCtx(t, "org_123", func(a *contextvalues.AuthContext) { a.SessionID = nil }), nil)

	scope, err := engine.ResolveReadScope(ctx, ScopeLogRead, readScopeProject)
	require.NoError(t, err)
	require.True(t, scope.Unrestricted())
}

func TestResolveReadScope_MemberWithoutGrantGetsOwnScope(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(readScopeMemberCtx(t, "org_read_scope", nil), nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedOrganization(t, ctx, conn, orgID)
	seedConnectedUser(t, ctx, conn, orgID, authCtx.UserID, "member@example.com", "Member", "workos_member", "membership_member")
	seedConnectedUser(t, ctx, conn, orgID, "user_other", "other@example.com", "Other", "workos_other", "membership_other")

	agents := agentsrepo.New(conn)
	fixtures := testrepo.New(conn)
	create := func(owner, name string) uuid.UUID {
		row, err := agents.CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: orgID, OwnerUserID: owner, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Name: name})
		require.NoError(t, err)
		return row.ID
	}
	owned := create(authCtx.UserID, "owned")
	latched := create(authCtx.UserID, "latched")
	require.NoError(t, fixtures.SetAgentOwnerLatchFixture(ctx, latched))
	deleted := create(authCtx.UserID, "deleted")
	require.NoError(t, fixtures.SoftDeleteAgentFixture(ctx, deleted))
	create("user_other", "theirs")

	scope, err := engine.ResolveReadScope(ctx, ScopeLogRead, readScopeProject)
	require.NoError(t, err)
	require.Equal(t, ReadScopeOwn, scope.Kind)
	require.Equal(t, authCtx.UserID, scope.UserID)
	require.Equal(t, "member@example.com", scope.UserEmail)
	require.Equal(t, []uuid.UUID{owned}, scope.AgentIDs, "latched, deleted and other owners' agents are not the member's")
}

func TestResolveReadScope_NonFallbackScopeIsDenied(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(readScopeMemberCtx(t, "org_123", nil), nil)

	_, err := engine.ResolveReadScope(ctx, ScopeProjectRead, readScopeProject)
	requireForbidden(t, err)
}

func TestResolveReadScope_EmbeddedCallerIsDenied(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(readScopeMemberCtx(t, "org_123", func(a *contextvalues.AuthContext) { a.ExternalUserID = "ext_1" }), nil)

	_, err := engine.ResolveReadScope(ctx, ScopeLogRead, readScopeProject)
	requireForbidden(t, err)
}

func TestResolveReadScope_FillsEmailFromTheUserRow(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(readScopeMemberCtx(t, "org_email_scope", func(a *contextvalues.AuthContext) { a.Email = nil }), nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "row@example.com", "Member", "workos_row", "membership_row")

	scope, err := engine.ResolveReadScope(ctx, ScopeLogRead, readScopeProject)
	require.NoError(t, err)
	require.Equal(t, ReadScopeOwn, scope.Kind)
	require.Equal(t, "row@example.com", scope.UserEmail, "an auth context without an email falls back to the user row")
}

func TestResolveReadScope_ChatReadFallsBackToOwnScope(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(readScopeMemberCtx(t, "org_chat_scope", nil), nil)

	scope, err := engine.ResolveReadScope(ctx, ScopeChatRead, readScopeProject)
	require.NoError(t, err)
	require.Equal(t, ReadScopeOwn, scope.Kind)
	require.Equal(t, "user_123", scope.UserID)
	require.Empty(t, scope.AgentIDs)
}
