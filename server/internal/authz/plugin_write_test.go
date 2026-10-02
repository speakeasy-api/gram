package authz

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

func TestRequirePluginWrite(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	for _, tc := range []struct {
		name    string
		grants  []Grant
		allowed bool
	}{
		{"plugin writer", []Grant{NewGrant(ScopePluginWrite, "project-one")}, true},
		{"existing admin", []Grant{NewGrant(ScopeOrgAdmin, "org-one")}, true},
		{"root", []Grant{NewGrant(ScopeRoot, "*")}, true},
		{"skill author", []Grant{NewGrant(ScopeSkillWrite, "project-one")}, false},
		{"project writer", []Grant{NewGrant(ScopeProjectWrite, "project-one")}, false},
		{"mcp writer", []Grant{NewGrant(ScopeMCPWrite, "*")}, false},
		{"wrong project", []Grant{NewGrant(ScopePluginWrite, "project-two")}, false},
		{"wrong organization", []Grant{NewGrant(ScopeOrgAdmin, "org-two")}, false},
		{"blocked writer", []Grant{NewGrant(ScopePluginWrite, "*"), NewGrant(ScopePluginBlockedWrite, "project-one")}, false},
		{"blocked admin", []Grant{NewGrant(ScopeOrgAdmin, "org-one"), NewGrant(ScopePluginBlockedWrite, "project-one")}, false},
		{"blocked org admin", []Grant{NewGrant(ScopePluginWrite, "project-one"), NewGrant(ScopeOrgBlockedAdmin, "org-one")}, false},
		{"unrelated exclusion", []Grant{NewGrant(ScopePluginWrite, "project-one"), NewGrant(ScopePluginBlockedWrite, "project-two")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := engine.RequirePluginWrite(GrantsToContext(enterpriseSessionCtx(t), tc.grants), "org-one", "project-one")
			if tc.allowed {
				require.NoError(t, err)
			} else {
				var denied *oops.ShareableError
				require.ErrorAs(t, err, &denied)
				require.Equal(t, oops.CodeForbidden, denied.Code)
			}
		})
	}
}

func TestPluginScopeIsIndependent(t *testing.T) {
	t.Parallel()
	require.Equal(t, ResourceKindProject, ResourceKindForScope(ScopePluginWrite))
	require.Equal(t, ResourceKindProject, ResourceKindForScope(ScopePluginBlockedWrite))
	require.Contains(t, adminScopes, ScopePluginWrite)
	require.NotContains(t, memberScopes, ScopePluginWrite)
	require.Equal(t, ScopePluginBlockedWrite, scopeExclusions[ScopePluginWrite])
	for _, scope := range []Scope{ScopeSkillRead, ScopeSkillWrite, ScopeMCPRead, ScopeMCPWrite, ScopeProjectRead, ScopeProjectWrite, ScopeOrgRead} {
		require.False(t, GrantsSatisfy([]Grant{NewGrant(ScopePluginWrite, "*")}, Check{Scope: scope, ResourceID: "project-one"}), "plugin write must not imply %s", scope)
		require.False(t, GrantsSatisfy([]Grant{NewGrant(scope, "*")}, Check{Scope: ScopePluginWrite, ResourceID: "project-one"}), "%s must not imply plugin write", scope)
	}
}
