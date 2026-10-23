package runtimepolicy

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestInferenceExplicitDelegation(t *testing.T) {
	t.Parallel()
	grant := authz.NewGrant(authz.ScopeInferenceExecute, authz.WildcardResource)
	grant.Selector[authz.SelectorKeyProjectID] = "project_a"
	require.False(t, IsRuntimeScopeSafe(RuntimeScopeRegistryVersion5, grant.Scope))
	require.True(t, IsRuntimeScopeSafe(RuntimeScopeRegistryVersion6, grant.Scope))
	_, err := NewDelegatedPolicy(DelegatedPolicyVersion5, []authz.Grant{grant})
	require.Error(t, err)
	policy, err := NewDelegatedPolicy(DelegatedPolicyVersion6, []authz.Grant{grant})
	require.NoError(t, err)
	require.Equal(t, []authz.Grant{grant}, policy.RuntimeGrants())
	encoded, err := EncodeDelegatedPolicy(DelegatedPolicyVersion6, policy)
	require.NoError(t, err)
	decoded, err := DecodeDelegatedPolicy(DelegatedPolicyVersion6, encoded)
	require.NoError(t, err)
	require.Equal(t, policy.RuntimeGrants(), decoded.RuntimeGrants())
	empty, err := NewDelegatedPolicy(DelegatedPolicyVersion6, nil)
	require.NoError(t, err)
	require.Empty(t, empty.RuntimeGrants())
}
