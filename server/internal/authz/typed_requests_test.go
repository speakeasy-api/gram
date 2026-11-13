package authz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Require(AnyOf(...)) and Can(AnyOf(...)) decide exactly as
// RequireAnyUnblocked does, for the same alternatives and grants.
func TestAnyOf_MatchesRequireAnyUnblocked(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	alternatives := []Check{
		Project(ProjectIDFrom("project-one")).Write(),
		Environment(EnvironmentIDFrom("env-one"), ProjectIDFrom("project-one")).Write(),
	}
	for name, grants := range map[string][]Grant{
		"project allow":                 {NewGrant(ScopeProjectWrite, "project-one")},
		"resource allow":                {NewGrant(ScopeEnvironmentWrite, "env-one")},
		"project allow, resource block": {NewGrant(ScopeProjectWrite, "project-one"), NewGrant(ScopeEnvironmentBlockedWrite, "env-one")},
		"root, resource block":          {NewGrant(ScopeRoot, WildcardResource), NewGrant(ScopeEnvironmentBlockedWrite, "env-one")},
		"other resource block":          {NewGrant(ScopeProjectWrite, "project-one"), NewGrant(ScopeEnvironmentBlockedWrite, "env-two")},
		"unrelated grant":               {NewGrant(ScopeSkillRead, "skill-one")},
		"no grants":                     nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := GrantsToContext(enterpriseSessionCtx(t), grants)
			want := engine.RequireAnyUnblocked(ctx, alternatives...)
			got := engine.Require(ctx, AnyOf(alternatives...))
			if want == nil {
				require.NoError(t, got)
			} else {
				require.Error(t, got)
				require.Equal(t, want.Error(), got.Error())
			}
			can, err := engine.Can(ctx, AnyOf(alternatives...))
			require.NoError(t, err)
			require.Equal(t, want == nil, can)
		})
	}
}

func TestAnyOf_OnlyAsTheSingleCheckOfRequireOrCan(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	anyOf := AnyOf(Project(ProjectIDFrom("project-one")).Write(), Project(ProjectIDFrom("project-one")).Read())

	// A misuse is refused whether or not RBAC is enforced for the caller.
	for name, ctx := range shapeTestContexts(t, []Grant{NewGrant(ScopeRoot, WildcardResource)}) {
		requireInvalid := func(err error) {
			t.Helper()
			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable, name)
			require.NotEqual(t, oops.CodeForbidden, shareable.Code, "%s: a misused AnyOf is a bad check, not a denial", name)
		}
		requireInvalid(engine.Require(ctx, anyOf, Project(ProjectIDFrom("project-one")).Read()))
		requireInvalid(engine.Require(ctx, AnyOf(anyOf)))
		_, err := engine.Evaluate(ctx, anyOf)
		requireInvalid(err)
		_, err = engine.Filter(ctx, []Check{anyOf})
		requireInvalid(err)
		_, err = engine.FindMatched(ctx, []Check{anyOf})
		requireInvalid(err)
		_, err = engine.Can(ctx, anyOf, Project(ProjectIDFrom("project-one")).Read())
		requireInvalid(err)
		_, err = engine.Can(ctx, AnyOf(anyOf))
		requireInvalid(err)
		requireInvalid(engine.Require(ctx, AnyOf()))
		_, err = engine.Can(ctx, AnyOf())
		requireInvalid(err)
	}
}

// A probe is a discovery question: Can and Filter answer it, Require refuses
// it, alone or inside AnyOf, whether or not RBAC is enforced.
func TestProbe_RefusedByRequire(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	probe := MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect().Probe()

	for name, ctx := range shapeTestContexts(t, []Grant{NewGrant(ScopeMCPConnect, "server-1")}) {
		can, err := engine.Can(ctx, probe)
		require.NoError(t, err, name)
		require.True(t, can, name)
		ids, err := engine.Filter(ctx, []Check{probe})
		require.NoError(t, err, name)
		require.Equal(t, []string{"server-1"}, ids, name)

		for call, err := range map[string]error{
			"require":               engine.Require(ctx, probe),
			"require any":           engine.RequireAny(ctx, probe),
			"require any unblocked": engine.RequireAnyUnblocked(ctx, probe),
			"anyof alternative":     engine.Require(ctx, AnyOf(probe)),
			"probed anyof":          engine.Require(ctx, AnyOf(MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect()).Probe()),
		} {
			require.ErrorIs(t, err, ErrInvalidCheck, "%s: %s", name, call)
		}
	}
}

// shapeTestContexts returns a caller RBAC is enforced for, holding grants,
// and one it is skipped for (no session).
func shapeTestContexts(t *testing.T, grants []Grant) map[string]context.Context {
	t.Helper()
	return map[string]context.Context{
		"enforced": GrantsToContext(enterpriseSessionCtx(t), grants),
		"skipped":  contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", UserID: sessionUserID, AccountType: "enterprise"}),
	}
}
