package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundOwnerSelectorContainment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		scope   Scope
		target  string
		caller  string
		owner   string
		allowed bool
	}{
		{name: "self", scope: ScopeAgentWrite, target: AgentOwnerSelf, caller: "user-a", owner: "user-a", allowed: true},
		{name: "each holder", scope: ScopeAgentWrite, target: AgentOwnerSelf, caller: "user-b", owner: "user-b", allowed: true},
		{name: "not another holder", scope: ScopeAgentWrite, target: AgentOwnerSelf, caller: "user-a", owner: "user-b"},
		{name: "literal self owner is not relative self", scope: ScopeAgentWrite, target: AgentOwnerSelf, caller: "user-a", owner: "self"},
		{name: "literal self owner can create for itself", scope: ScopeAgentWrite, target: AgentOwnerSelf, caller: "self", owner: "self", allowed: true},
		{name: "wildcard includes literal self owner", scope: ScopeAgentWrite, target: "*", caller: "user-a", owner: "self", allowed: true},
		{name: "specific owner", scope: ScopeAgentWrite, target: "user-b", caller: "user-a", owner: "user-b", allowed: true},
		{name: "different owner", scope: ScopeAgentWrite, target: "user-b", caller: "user-a", owner: "user-a"},
		{name: "everyone", scope: ScopeAgentWrite, target: "*", caller: "user-a", owner: "user-b", allowed: true},
		{name: "read does not create", scope: ScopeAgentRead, target: "*", caller: "user-a", owner: "user-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			grant := NewGrantWithSelector(tc.scope, AgentOwnerSelector(tc.scope, tc.target))
			require.NoError(t, ValidateSelector(tc.scope, grant.Selector))
			grants := BindSelectorValue([]Grant{grant}, SelectorKeyOwner, AgentOwnerSelf, tc.caller)
			allowed := GrantsContainSelector(grants, ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, tc.owner))
			require.Equal(t, tc.allowed, allowed)
		})
	}
	require.Error(t, ValidateSelector(ScopeAgentWrite, Selector{SelectorKeyResourceKind: "agent_owner", SelectorKeyResourceID: "*"}))
}

func TestOwnerScopedAgentGrantRejectsMissingOwnerDimension(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"self", "employee", "*"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			grant := NewGrantWithSelector(ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, owner))
			allowed, err := GrantsAuthorize([]Grant{grant}, AgentCheck(ScopeAgentWrite, "existing-agent", ""))
			require.NoError(t, err)
			require.False(t, allowed)
		})
	}
}

func TestAgentSelectorUsesOwnerRatherThanOwnerUserID(t *testing.T) {
	t.Parallel()
	selector := AgentOwnerSelector(ScopeAgentWrite, AgentOwnerSelf)
	require.Equal(t, "self", selector["owner"])
	require.NotContains(t, selector, "owner_user_id")
	require.NoError(t, ValidateSelector(ScopeAgentWrite, selector))
	delete(selector, SelectorKeyOwner)
	selector["owner_user_id"] = AgentOwnerSelf
	require.Error(t, ValidateSelector(ScopeAgentWrite, selector))
}
