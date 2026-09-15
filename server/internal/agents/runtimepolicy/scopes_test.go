package runtimepolicy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestRuntimeScopeRegistryCoversAuthorizationScopes(t *testing.T) {
	t.Parallel()

	registered := authz.RegisteredScopes()
	for _, scope := range registered {
		require.Contains(t, runtimeScopeDefinitions, scope)
	}
	for scope, definition := range runtimeScopeDefinitions {
		if definition.lifecycle == RuntimeScopeLifecycleActive {
			require.Contains(t, registered, scope)
		}
	}
}

func TestRuntimeSafeScopesHaveSafeImplicationClosures(t *testing.T) {
	t.Parallel()

	for scope, definition := range runtimeScopeDefinitions {
		if definition.safeSince == 0 {
			continue
		}

		t.Run(string(scope), func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidateRuntimeScope(CurrentRuntimeScopeRegistryVersion, scope))
			for _, implied := range authz.ScopeImplicationClosure(scope) {
				impliedDefinition, ok := runtimeScopeDefinitions[implied]
				require.True(t, ok, "implication %q must remain registered", implied)
				require.Equal(t, RuntimeScopeLifecycleActive, impliedDefinition.lifecycle)
				require.NotZero(t, impliedDefinition.safeSince)
				require.LessOrEqual(t, impliedDefinition.safeSince, CurrentRuntimeScopeRegistryVersion)
			}
		})
	}
}

func TestRuntimeScopeAllowlist(t *testing.T) {
	t.Parallel()

	safe := []authz.Scope{
		authz.ScopeProjectRead, authz.ScopeProjectWrite,
		authz.ScopeMCPRead, authz.ScopeMCPWrite, authz.ScopeMCPConnect,
		authz.ScopeEnvironmentRead, authz.ScopeEnvironmentWrite,
		authz.ScopeSkillRead, authz.ScopeSkillWrite,
		authz.ScopeRiskPolicyEvaluate,
	}
	for _, scope := range safe {
		require.True(t, IsRuntimeScopeSafe(CurrentRuntimeScopeRegistryVersion, scope), scope)
	}

	unsafe := []authz.Scope{
		authz.Scope("unknown:scope"),
		authz.ScopeRoot,
		authz.ScopeOrgRead, authz.ScopeOrgBlockedRead, authz.ScopeOrgAdmin, authz.ScopeOrgBlockedAdmin,
		authz.ScopeProjectBlockedRead, authz.ScopeProjectBlockedWrite,
		authz.ScopeMCPBlockedRead, authz.ScopeMCPBlockedWrite, authz.ScopeMCPBlockedConnect,
		authz.ScopeEnvironmentBlockedRead, authz.ScopeEnvironmentBlockedWrite,
		authz.ScopeSkillBlockedRead, authz.ScopeSkillBlockedWrite,
		authz.ScopeRiskPolicyBypass, authz.ScopeRiskPolicyBlock,
		authz.ScopeChatRead, authz.ScopeChatWrite,
		authz.ScopeAgentRead, authz.ScopeAgentWrite, authz.ScopeAgentAuthorize, authz.ScopeAgentTransfer,
		scopeMCPApprovalReadTombstone, scopeMCPApprovalDecideTombstone,
	}
	for _, scope := range unsafe {
		require.False(t, IsRuntimeScopeSafe(CurrentRuntimeScopeRegistryVersion, scope), scope)
	}
	require.False(t, IsRuntimeScopeSafe(0, authz.ScopeMCPConnect))
}

func TestRuntimeScopeImplicationClosure(t *testing.T) {
	t.Parallel()

	require.Equal(t, []authz.Scope{authz.ScopeMCPConnect, authz.ScopeMCPRead, authz.ScopeMCPWrite}, authz.ScopeImplicationClosure(authz.ScopeMCPWrite))
	require.Equal(t, []authz.Scope{authz.ScopeProjectRead, authz.ScopeProjectWrite}, authz.ScopeImplicationClosure(authz.ScopeProjectWrite))
	require.Equal(t, []authz.Scope{authz.ScopeSkillRead}, authz.ScopeImplicationClosure(authz.ScopeSkillRead))
}

func TestRetiredScopeTombstonesFailClosedIndependently(t *testing.T) {
	t.Parallel()

	lifecycle, ok := RuntimeScopeLifecycleFor(scopeMCPApprovalReadTombstone)
	require.True(t, ok)
	require.Equal(t, RuntimeScopeLifecycleRetired, lifecycle)

	_, ok = RuntimeScopeLifecycleFor(authz.Scope("unknown:scope"))
	require.False(t, ok)
	require.Error(t, ValidateRuntimeScope(CurrentRuntimeScopeRegistryVersion, scopeMCPApprovalReadTombstone))
	require.Error(t, ValidateRuntimeScope(CurrentRuntimeScopeRegistryVersion, authz.Scope("unknown:scope")))
	require.NoError(t, ValidateRuntimeScope(CurrentRuntimeScopeRegistryVersion, authz.ScopeProjectRead))
}
