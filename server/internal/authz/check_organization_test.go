package authz

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const otherOrganizationLog = "authz check refused: it is about another organization than the caller's"

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
		requireForbidden(t, engine.Require(ctx, AnyOf(other)))
		requireForbidden(t, engine.RequireAny(ctx, other))
		requireForbidden(t, engine.RequireAnyUnblocked(ctx, other))

		allowed, err := engine.Can(ctx, other)
		require.NoError(t, err, name)
		require.False(t, allowed, name)

		allowed, err = engine.Can(ctx, AnyOf(other))
		require.NoError(t, err, name)
		require.False(t, allowed, name)

		// Another alternative can still satisfy the request.
		require.NoError(t, engine.RequireAny(ctx, other, own), name)
		require.NoError(t, engine.Require(ctx, AnyOf(other, own)), name)

		ids, err := engine.Filter(ctx, []Check{other, own})
		require.NoError(t, err, name)
		require.Equal(t, []string{"org-one"}, ids, name)

		matched, err := engine.FindMatched(ctx, []Check{other, own, other})
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
