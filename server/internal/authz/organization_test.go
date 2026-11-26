package authz

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Grants are loaded for the organization the caller is acting in, so an org
// check about any other organization is refused, even for a caller whose
// grants name every organization. The call site states nothing: the
// organization is the check's own resource.
func TestCheckOrganization_AnotherOrganizationIsRefused(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, "org-one"), []Grant{
		NewGrant(ScopeOrgAdmin, WildcardResource),
		NewGrant(ScopeProjectRead, WildcardResource),
	})

	own := Check{Scope: ScopeOrgAdmin, ResourceID: "org-one"}
	project := Check{Scope: ScopeProjectRead, ResourceID: "project-one"}
	for name, check := range map[string]Check{
		"org check on the caller's organization": own,
		"check on another kind of resource":      project,
	} {
		logs.Reset()
		require.NoError(t, engine.Require(ctx, check), name)
		require.NotContains(t, logs.String(), otherOrganizationLog, name)
	}

	for name, other := range map[string]Check{
		"another organization":          {Scope: ScopeOrgAdmin, ResourceID: "org-two"},
		"an id that cannot be compared": {Scope: ScopeOrgAdmin, ResourceID: "org two"},
	} {
		logs.Reset()
		requireForbidden(t, engine.Require(ctx, other))
		require.Contains(t, logs.String(), otherOrganizationLog, name)

		requireForbidden(t, engine.Require(ctx, own, other))
		requireForbidden(t, engine.Require(ctx, Either(other)))
		requireForbidden(t, engine.Require(ctx, Either(other)))
		requireForbidden(t, engine.Require(ctx, Either(other)))

		allowed, err := allAllowed(ctx, engine, other)
		require.NoError(t, err, name)
		require.False(t, allowed, name)

		allowed, err = allAllowed(ctx, engine, Either(other))
		require.NoError(t, err, name)
		require.False(t, allowed, name)

		// Another alternative can still satisfy the request.
		require.NoError(t, engine.Require(ctx, Either(other, own)), name)
		require.NoError(t, engine.Require(ctx, Either(other, own)), name)

		ids, err := allowedIDs(ctx, engine, []Check{other, own})
		require.NoError(t, err, name)
		require.Equal(t, []string{"org-one"}, ids, name)

		matched, err := allowedEach(ctx, engine, []Check{other, own, other})
		require.NoError(t, err, name)
		require.Equal(t, []bool{false, true, false}, matched, name)
	}
}

// A caller for whom enforcement is skipped is refused all the same.
func TestCheckOrganization_RefusedWhereEnforcementIsSkipped(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	authCtx, ok := contextvalues.GetAuthContext(enterpriseSessionCtxWithOrg(t, "org-one"))
	require.True(t, ok)
	sessionless := *authCtx
	sessionless.SessionID = nil
	ctx := contextvalues.SetAuthContext(t.Context(), &sessionless)
	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce)

	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: "org-one"}))
	requireForbidden(t, engine.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: "org-two"}))
}

func TestRequireUserOrganizationScopeLoadsRequestedOrganizationRoleGrants(t *testing.T) {
	t.Parallel()

	activeOrganizationID := "org_requested_scope_active"
	targetOrganizationID := "org_requested_scope_target"
	userID := "user_requested_scope_member"
	ctx := enterpriseSessionCtxWithOrg(t, activeOrganizationID)
	conn := newTestDB(t)

	seedOrganization(t, ctx, conn, activeOrganizationID)
	seedOrganization(t, ctx, conn, targetOrganizationID)
	seedActiveOrganizationUser(t, ctx, conn, targetOrganizationID, userID)
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, targetOrganizationID))
	seedRoleAssignmentForUser(t, ctx, conn, targetOrganizationID, userID, SystemRoleMember)

	ctx = GrantsToContext(ctx, []Grant{NewGrant(ScopeOrgRead, activeOrganizationID)})
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	require.NoError(t, engine.RequireUserOrganizationScope(ctx, targetOrganizationID, userID, ScopeOrgRead))
}

func TestRequireUserOrganizationScopeRejectsActiveOrganizationGrants(t *testing.T) {
	t.Parallel()

	activeOrganizationID := "org_requested_scope_only_active"
	targetOrganizationID := "org_requested_scope_without_grants"
	userID := "user_requested_scope_without_target_grants"
	ctx := enterpriseSessionCtxWithOrg(t, activeOrganizationID)
	conn := newTestDB(t)

	seedOrganization(t, ctx, conn, activeOrganizationID)
	seedOrganization(t, ctx, conn, targetOrganizationID)
	seedActiveOrganizationUser(t, ctx, conn, targetOrganizationID, userID)
	ctx = GrantsToContext(ctx, []Grant{NewGrant(ScopeOrgAdmin, WildcardResource)})
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	err := engine.RequireUserOrganizationScope(ctx, targetOrganizationID, userID, ScopeOrgRead)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
}

func TestRequireUserOrganizationScopeSkipsSessionlessRequests(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	engine := NewEngine(testenv.NewLogger(t), nil, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	require.NoError(t, engine.RequireUserOrganizationScope(ctx, "org_requested_scope_target", authCtx.UserID, ScopeOrgRead))
}

func TestRequireUserOrganizationScopeRejectsAPIKeys(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.APIKeyID = "api_key_requested_scope"
	authCtx.SessionID = nil
	ctx = contextvalues.WithLegacyAPIKeyAuthorization(ctx, authCtx)
	engine := NewEngine(testenv.NewLogger(t), nil, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	err := engine.RequireUserOrganizationScope(ctx, "org_requested_scope_target", authCtx.UserID, ScopeOrgRead)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
}

const otherOrganizationLog = "authz check refused: it is about another organization than the caller's"
