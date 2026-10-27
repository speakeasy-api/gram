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

func TestInferencePolicyVersionCompatibility(t *testing.T) {
	t.Parallel()
	require.Equal(t, DelegatedPolicyVersion4, IssuedDelegatedPolicyVersion)
	require.Equal(t, DelegatedPolicyVersion5, AssistantIssuedPolicyVersion)
	require.Equal(t, DelegatedPolicyVersion6, InferenceIssuedPolicyVersion)
	grant := authz.NewGrant(authz.ScopeInferenceExecute, authz.WildcardResource)
	grant.Selector[authz.SelectorKeyProjectID] = "project_a"
	policy, err := NewDelegatedPolicy(InferenceIssuedPolicyVersion, []authz.Grant{grant})
	require.NoError(t, err)
	raw, err := EncodeDelegatedPolicy(InferenceIssuedPolicyVersion, policy)
	require.NoError(t, err)
	// Pre-inference readers supported through v5 and reject v6 before reading its grants.
	rejected, err := decodeDelegatedPolicyThroughVersion(InferenceIssuedPolicyVersion, raw, DelegatedPolicyVersion5)
	require.ErrorIs(t, err, ErrInvalidDelegatedPolicy)
	require.Empty(t, rejected.RuntimeGrants())
	for _, version := range []DelegatedPolicyVersion{DelegatedPolicyVersion4, DelegatedPolicyVersion5} {
		require.False(t, IsRuntimeScopeSafe(RuntimeScopeRegistryVersion(version), grant.Scope))
		_, err := NewDelegatedPolicy(version, []authz.Grant{grant})
		require.ErrorIs(t, err, ErrInvalidDelegatedPolicy)
		decoded, err := DecodeDelegatedPolicy(version, raw)
		require.ErrorIs(t, err, ErrInvalidDelegatedPolicy, "relabeling inference as a legacy policy must fail closed")
		require.Empty(t, decoded.RuntimeGrants())
		legacy, err := NewDelegatedPolicy(version, []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, "server_a")})
		require.NoError(t, err)
		encoded, err := EncodeDelegatedPolicy(version, legacy)
		require.NoError(t, err)
		compatible, err := decodeDelegatedPolicyThroughVersion(version, encoded, DelegatedPolicyVersion5)
		require.NoError(t, err)
		require.Equal(t, legacy.RuntimeGrants(), compatible.RuntimeGrants())
		for _, admitted := range compatible.RuntimeGrants() {
			require.NotEqual(t, authz.ScopeInferenceExecute, admitted.Scope)
		}
	}
}
