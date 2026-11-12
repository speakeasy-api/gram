package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

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
		Project("project-one").Write(),
		Environment("env-one", "project-one").Write(),
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
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeRoot, WildcardResource)})
	anyOf := AnyOf(Project("project-one").Write(), Project("project-one").Read())

	requireInvalid := func(err error) {
		t.Helper()
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.NotEqual(t, oops.CodeForbidden, shareable.Code, "a misused AnyOf is a bad check, not a denial")
	}
	requireInvalid(engine.Require(ctx, anyOf, Project("project-one").Read()))
	requireInvalid(engine.Require(ctx, AnyOf(anyOf)))
	_, err := engine.Evaluate(ctx, anyOf)
	requireInvalid(err)
	_, err = engine.Filter(ctx, []Check{anyOf})
	requireInvalid(err)
	_, err = engine.Can(ctx, anyOf, Project("project-one").Read())
	requireInvalid(err)
	requireInvalid(engine.Require(ctx, AnyOf()))
}

// A probe is a discovery question: Can and Filter answer it, Require refuses
// it, alone or inside AnyOf.
func TestProbe_RefusedByRequire(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPConnect, "server-1")})
	probe := MCPServer("server-1", "project-1").Connect().Probe()

	can, err := engine.Can(ctx, probe)
	require.NoError(t, err)
	require.True(t, can)
	ids, err := engine.Filter(ctx, []Check{probe})
	require.NoError(t, err)
	require.Equal(t, []string{"server-1"}, ids)

	for name, err := range map[string]error{
		"require":               engine.Require(ctx, probe),
		"require any":           engine.RequireAny(ctx, probe),
		"require any unblocked": engine.RequireAnyUnblocked(ctx, probe),
		"anyof alternative":     engine.Require(ctx, AnyOf(probe)),
		"probed anyof":          engine.Require(ctx, AnyOf(MCPServer("server-1", "project-1").Connect()).Probe()),
	} {
		require.ErrorIs(t, err, ErrInvalidCheck, name)
	}
}
