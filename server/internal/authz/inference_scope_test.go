package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInferenceExecuteRequiresExplicitProjectGrant(t *testing.T) {
	t.Parallel()
	grant := NewGrant(ScopeInferenceExecute, WildcardResource)
	grant.Selector[SelectorKeyProjectID] = "project_a"
	require.NoError(t, ValidateSelector(grant.Scope, grant.Selector))
	require.Equal(t, ResourceKindInference, ResourceKindForScope(grant.Scope))
	require.Equal(t, "project_a", InferenceExecuteCheck("project_a").ResourceID)
	require.NoError(t, validateInput(InferenceExecuteCheck("project_a")))
	require.Error(t, validateInput(InferenceExecuteCheck("")))
	for _, project := range []string{"project_a", "project_b", ""} {
		got, _ := matchingGrant([]Grant{grant}, InferenceExecuteCheck(project).expand())
		if project == "project_a" {
			require.NotNil(t, got)
		} else {
			require.Nil(t, got)
		}
	}
	for _, scope := range []Scope{ScopeChatWrite, ScopeProjectWrite, ScopeOrgAdmin} {
		got, _ := matchingGrant([]Grant{NewGrant(scope, WildcardResource)}, InferenceExecuteCheck("project_a").expand())
		require.Nil(t, got, scope)
	}
	got, _ := matchingGrant([]Grant{grant}, ChatWriteCheck("chat_a").expand())
	require.Nil(t, got)
	require.Equal(t, []Scope{ScopeInferenceExecute}, ScopeImplicationClosure(ScopeInferenceExecute))
	for role, grants := range SystemRoleGrants {
		for _, g := range grants {
			require.NotEqual(t, string(ScopeInferenceExecute), g.Scope, role)
		}
	}
	require.NoError(t, ValidateGrantSurface(GrantSurfaceAccess, []*RoleGrant{{Scope: string(ScopeInferenceExecute)}}))
}

func TestInferenceSelector(t *testing.T) {
	t.Parallel()
	for _, project := range []string{"project_a", WildcardResource} {
		grant := NewGrant(ScopeInferenceExecute, WildcardResource)
		grant.Selector[SelectorKeyProjectID] = project
		require.NoError(t, ValidateSelector(grant.Scope, grant.Selector))
	}
	require.NoError(t, ValidateSelector(ScopeInferenceExecute, NewGrant(ScopeInferenceExecute, WildcardResource).Selector))
	require.Error(t, ValidateSelector(ScopeInferenceExecute, NewGrant(ScopeInferenceExecute, "project_a").Selector))
	for key, value := range map[string]string{SelectorKeyProjectID: "", SelectorKeyTool: "tool_a"} {
		grant := NewGrant(ScopeInferenceExecute, WildcardResource)
		grant.Selector[key] = value
		require.Error(t, ValidateSelector(grant.Scope, grant.Selector))
	}
}
