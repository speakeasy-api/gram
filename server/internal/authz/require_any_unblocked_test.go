package authz

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

func TestRequireAnyUnblocked(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	checks := []Check{
		{Scope: ScopeSkillRead, ResourceID: "project-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one"}},
		{Scope: ScopeSkillRead, ResourceID: "skill-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one"}},
	}
	for _, tc := range []struct {
		name    string
		grants  []Grant
		allowed bool
	}{
		{"project allow", []Grant{NewGrant(ScopeSkillWrite, "project-one")}, true},
		{"skill allow", []Grant{NewGrant(ScopeSkillRead, "skill-one")}, true},
		{"root is not an exclusion", []Grant{NewGrant(ScopeRoot, "*")}, true},
		{"project allow skill deny", []Grant{NewGrant(ScopeSkillWrite, "project-one"), NewGrant(ScopeSkillBlockedRead, "skill-one")}, false},
		{"skill allow project deny", []Grant{NewGrant(ScopeSkillWrite, "skill-one"), NewGrant(ScopeSkillBlockedRead, "project-one")}, false},
		{"root explicit deny", []Grant{NewGrant(ScopeRoot, "*"), NewGrant(ScopeSkillBlockedRead, "skill-one")}, false},
		{"other skill exclusion", []Grant{NewGrant(ScopeSkillWrite, "project-one"), NewGrant(ScopeSkillBlockedRead, "skill-two")}, true},
		{"write exclusion does not deny reads", []Grant{NewGrant(ScopeSkillWrite, "project-one"), NewGrant(ScopeSkillBlockedWrite, "skill-one")}, true},
		{"wrong project exclusion", []Grant{NewGrant(ScopeSkillWrite, "project-one"), NewGrantWithSelector(ScopeSkillBlockedRead, Selector{SelectorKeyResourceKind: ResourceKindSkill, SelectorKeyResourceID: "skill-one", SelectorKeyProjectID: "project-two"})}, true},
		{"no grants", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := GrantsToContext(enterpriseSessionCtx(t), tc.grants)
			err := engine.RequireAnyUnblocked(ctx, checks...)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				var denied *oops.ShareableError
				require.ErrorAs(t, err, &denied)
				require.Equal(t, oops.CodeForbidden, denied.Code)
			}
		})
	}
	t.Run("every admitted policy can exclude", func(t *testing.T) {
		allow := []Grant{NewGrant(ScopeSkillRead, "project-one")}
		deny := []Grant{NewGrant(ScopeSkillRead, "project-one"), NewGrant(ScopeSkillBlockedRead, "skill-one")}
		for i := range 3 {
			policies := [][]Grant{allow, allow, allow}
			policies[i] = deny
			ctx := principalPolicyTestContext(t, policies[0], policies[1], policies[2])
			var forbidden *oops.ShareableError
			require.ErrorAs(t, engine.RequireAnyUnblocked(ctx, checks...), &forbidden)
			require.Equal(t, oops.CodeForbidden, forbidden.Code)
		}
	})
	t.Run("admitted policies cannot combine different allow alternatives", func(t *testing.T) {
		ctx := principalPolicyTestContext(t,
			[]Grant{NewGrant(ScopeSkillRead, "project-one")},
			[]Grant{NewGrant(ScopeSkillRead, "skill-one")},
			[]Grant{NewGrant(ScopeSkillRead, "*")},
		)
		var forbidden *oops.ShareableError
		require.ErrorAs(t, engine.RequireAnyUnblocked(ctx, checks...), &forbidden)
		require.Equal(t, oops.CodeForbidden, forbidden.Code)
	})
}
