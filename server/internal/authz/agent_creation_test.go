package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentCreationChecksRespectOwnerTargeting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		scope   Scope
		target  string
		caller  string
		owner   string
		allowed bool
	}{
		{name: "self", scope: ScopeAgentCreate, target: AgentOwnerSelf, caller: "user-a", owner: "user-a", allowed: true},
		{name: "each holder", scope: ScopeAgentCreate, target: AgentOwnerSelf, caller: "user-b", owner: "user-b", allowed: true},
		{name: "not another holder", scope: ScopeAgentCreate, target: AgentOwnerSelf, caller: "user-a", owner: "user-b"},
		{name: "literal self owner is not relative self", scope: ScopeAgentCreate, target: AgentOwnerSelf, caller: "user-a", owner: "self"},
		{name: "literal self owner can create for itself", scope: ScopeAgentCreate, target: AgentOwnerSelf, caller: "self", owner: "self", allowed: true},
		{name: "wildcard includes literal self owner", scope: ScopeAgentCreate, target: "*", caller: "user-a", owner: "self", allowed: true},
		{name: "specific owner", scope: ScopeAgentCreate, target: "user-b", caller: "user-a", owner: "user-b", allowed: true},
		{name: "different owner", scope: ScopeAgentCreate, target: "user-b", caller: "user-a", owner: "user-a"},
		{name: "everyone", scope: ScopeAgentCreate, target: "*", caller: "user-a", owner: "user-b", allowed: true},
		{name: "management is not creation", scope: ScopeAgentWrite, target: "*", caller: "user-a", owner: "user-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			grant := NewGrant(tc.scope, tc.target)
			require.NoError(t, ValidateSelector(tc.scope, grant.Selector))
			allowed := false
			for _, check := range AgentCreationChecks(tc.caller, tc.owner) {
				match, err := GrantsAuthorize([]Grant{grant}, check)
				require.NoError(t, err)
				allowed = allowed || match
			}
			require.Equal(t, tc.allowed, allowed)
		})
	}
	require.Error(t, ValidateSelector(ScopeAgentCreate, NewSelector(ScopeAgentWrite, "*")))
}
