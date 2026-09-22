package runtimepolicy

import (
	"fmt"
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
	t.Run("current registry scopes are discoverable", func(t *testing.T) {
		t.Parallel()
		sync := authz.NewGrant(authz.ScopeOrgDeviceAgentSync, "example-org")
		grants, err := DelegableGrants([]authz.Grant{sync}, []authz.Grant{sync}, []authz.Grant{sync})
		require.NoError(t, err)
		require.Len(t, grants, 1)
		require.Equal(t, authz.ScopeOrgDeviceAgentSync, grants[0].Scope)
		_, err = NewDelegatedPolicy(CurrentDelegatedPolicyVersion, grants)
		require.NoError(t, err)
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

func TestDelegableGrantsConcreteResource(t *testing.T) {
	t.Parallel()
	constraint := authz.NewSelector(authz.ScopeMCPConnect, "selected-server")
	constraint[authz.SelectorKeyProjectID] = "selected-project"
	broad := authz.NewGrant(authz.ScopeMCPWrite, "*")
	for _, excludedBy := range []int{0, 1, 2} {
		for _, dimension := range []string{authz.SelectorKeyResourceID, authz.SelectorKeyProjectID, authz.SelectorKeyTool, authz.SelectorKeyDisposition} {
			t.Run(fmt.Sprintf("parent-%d/%s", excludedBy, dimension), func(t *testing.T) {
				t.Parallel()
				exclusion := authz.NewGrant(authz.ScopeMCPBlockedConnect, "*")
				exclusion.Selector[dimension] = "other"
				if dimension == authz.SelectorKeyDisposition {
					exclusion.Selector[dimension] = authz.DispositionDestructive
				}
				policies := [][]authz.Grant{{broad}, {broad}, {broad}}
				policies[excludedBy] = append(policies[excludedBy], exclusion)
				unscoped, err := DelegableGrants(policies[0], policies[1], policies[2])
				require.NoError(t, err)
				require.Empty(t, unscoped)
				grants, err := DelegableGrants(policies[0], policies[1], policies[2], constraint)
				require.NoError(t, err)
				if dimension == authz.SelectorKeyTool || dimension == authz.SelectorKeyDisposition {
					require.Empty(t, grants, "overlapping tool/disposition exclusions still fail closed")
					return
				}
				require.Len(t, grants, 3, "write, read, and connect implications remain delegable")
				for _, grant := range grants {
					require.Equal(t, constraint, grant.Selector)
					policy, err := NewDelegatedPolicyV1([]authz.Grant{grant})
					require.NoError(t, err)
					safe, err := DelegationContained(policy, policies...)
					require.NoError(t, err)
					require.True(t, safe)
				}
				exclusion.Selector[dimension] = constraint[dimension]
				grants, err = DelegableGrants(policies[0], policies[1], policies[2], constraint)
				require.NoError(t, err)
				require.Empty(t, grants, "selected resource exclusions also block implied scopes")
			})
		}
	}
}

func TestDelegableGrantsConcreteResourcePreservesPinnedDimensions(t *testing.T) {
	t.Parallel()
	constraint := authz.NewSelector(authz.ScopeMCPConnect, "selected-server")
	constraint[authz.SelectorKeyProjectID] = "selected-project"
	broad := authz.NewGrant(authz.ScopeMCPWrite, "*")
	pinned := authz.NewGrant(authz.ScopeMCPConnect, "*")
	pinned.Selector[authz.SelectorKeyTool] = "safe-tool"
	pinned.Selector[authz.SelectorKeyDisposition] = authz.DispositionReadOnly
	grants, err := DelegableGrants([]authz.Grant{broad}, []authz.Grant{broad}, []authz.Grant{pinned}, constraint)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, authz.ScopeMCPConnect, grants[0].Scope)
	require.Equal(t, "safe-tool", grants[0].Selector[authz.SelectorKeyTool])
	require.Equal(t, authz.DispositionReadOnly, grants[0].Selector[authz.SelectorKeyDisposition])
	for _, dimension := range []string{authz.SelectorKeyResourceID, authz.SelectorKeyProjectID} {
		incompatible := authz.NewGrant(authz.ScopeMCPConnect, "*")
		incompatible.Selector[dimension] = "other"
		grants, err := DelegableGrants([]authz.Grant{broad}, []authz.Grant{broad}, []authz.Grant{incompatible}, constraint)
		require.NoError(t, err)
		require.Empty(t, grants)
	}
	grants, err = DelegableGrants([]authz.Grant{authz.NewGrant(authz.ScopeSkillRead, "*")}, []authz.Grant{broad}, []authz.Grant{broad}, constraint)
	require.NoError(t, err)
	require.Empty(t, grants, "scoped discovery excludes unrelated resource kinds")
}
