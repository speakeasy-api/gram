package runtimepolicy

import (
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDelegableGrants(t *testing.T) {
	t.Parallel()
	server := "example-server"
	broad := authz.Grant{PrincipalUrn: "", Scope: authz.ScopeMCPWrite, Selector: authz.NewSelector(authz.ScopeMCPWrite, server)}
	caller := authz.Grant{PrincipalUrn: "", Scope: authz.ScopeMCPConnect, Selector: authz.NewSelector(authz.ScopeMCPConnect, server)}
	caller.Selector[authz.SelectorKeyTool] = "allowed-tool"
	owner := authz.Grant{PrincipalUrn: "", Scope: authz.ScopeMCPWrite, Selector: authz.NewSelector(authz.ScopeMCPWrite, server)}
	owner.Selector[authz.SelectorKeyProjectID] = "example-project"

	t.Run("implications and pinned dimensions", func(t *testing.T) {
		t.Parallel()
		grants, err := DelegableGrants([]authz.Grant{broad}, []authz.Grant{owner}, []authz.Grant{caller})
		require.NoError(t, err)
		require.Len(t, grants, 1)
		require.Equal(t, authz.ScopeMCPConnect, grants[0].Scope)
		require.Equal(t, "allowed-tool", grants[0].Selector[authz.SelectorKeyTool])
		require.Equal(t, "example-project", grants[0].Selector[authz.SelectorKeyProjectID])
	})
	t.Run("incompatible and missing rights", func(t *testing.T) {
		t.Parallel()
		incompatible := authz.Grant{PrincipalUrn: "", Scope: caller.Scope, Selector: authz.NewSelector(caller.Scope, "other-server")}
		for _, policy := range [][]authz.Grant{nil, {incompatible}} {
			grants, err := DelegableGrants([]authz.Grant{broad}, []authz.Grant{owner}, policy)
			require.NoError(t, err)
			require.Empty(t, grants)
		}
	})
	t.Run("exclusions cannot leak through broad candidates", func(t *testing.T) {
		t.Parallel()
		exclusion := authz.Grant{PrincipalUrn: "", Scope: authz.ScopeMCPBlockedConnect, Selector: authz.NewSelector(authz.ScopeMCPBlockedConnect, server)}
		exclusion.Selector[authz.SelectorKeyTool] = "denied-tool"
		grants, err := DelegableGrants([]authz.Grant{broad}, []authz.Grant{broad}, []authz.Grant{broad, exclusion})
		require.NoError(t, err)
		require.Empty(t, grants, "write and read imply connect, whose broad candidate overlaps the exclusion")
		grants, err = DelegableGrants([]authz.Grant{broad}, []authz.Grant{owner}, []authz.Grant{caller, exclusion})
		require.NoError(t, err)
		require.Len(t, grants, 1, "a disjoint pinned tool remains representable")
		exclusion.Selector[authz.SelectorKeyTool] = "allowed-tool"
		grants, err = DelegableGrants([]authz.Grant{broad}, []authz.Grant{owner}, []authz.Grant{caller, exclusion})
		require.NoError(t, err)
		require.Empty(t, grants)
	})
	t.Run("runtime unsafe scopes never become candidates", func(t *testing.T) {
		t.Parallel()
		unsafe := authz.Grant{PrincipalUrn: "", Scope: authz.ScopeAgentAuthorize, Selector: authz.NewSelector(authz.ScopeAgentAuthorize, "example-agent")}
		grants, err := DelegableGrants([]authz.Grant{unsafe}, []authz.Grant{unsafe}, []authz.Grant{unsafe})
		require.NoError(t, err)
		require.Empty(t, grants)
	})
}

func TestDelegableWildcardCandidates(t *testing.T) {
	t.Parallel()
	broad := authz.NewGrant(authz.ScopeMCPWrite, "*")
	caller := authz.NewGrant(authz.ScopeMCPConnect, "*")
	for _, resource := range []string{"*", "example-server"} {
		t.Run(resource, func(t *testing.T) {
			t.Parallel()
			agent := authz.NewGrant(authz.ScopeMCPWrite, resource)
			grants, err := DelegableGrants([]authz.Grant{agent}, []authz.Grant{broad}, []authz.Grant{caller})
			require.NoError(t, err)
			require.Len(t, grants, 1)
			require.Equal(t, resource, grants[0].Selector[authz.SelectorKeyResourceID])
			policy, err := NewDelegatedPolicyV1(grants)
			require.NoError(t, err)
			allowed, err := DelegationContained(policy, []authz.Grant{agent}, []authz.Grant{broad}, []authz.Grant{caller})
			require.NoError(t, err)
			require.True(t, allowed)
			exclusion := authz.NewGrant(authz.ScopeMCPBlockedConnect, "example-server")
			allowed, err = DelegationContained(policy, []authz.Grant{agent}, []authz.Grant{broad}, []authz.Grant{caller, exclusion})
			require.NoError(t, err)
			require.False(t, allowed)
			grants, err = DelegableGrants([]authz.Grant{agent}, []authz.Grant{broad}, []authz.Grant{caller, exclusion})
			require.NoError(t, err)
			require.Empty(t, grants)
		})
	}
}
