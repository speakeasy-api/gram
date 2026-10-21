package runtimepolicy

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func authGrant(principal string, scope authz.Scope, resourceID string, extra map[string]string) authz.Grant {
	selector := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: resourceID}
	maps.Copy(selector, extra)
	return authz.Grant{PrincipalUrn: principal, Scope: scope, Selector: selector}
}

func ownedBy(owner string) map[string]string {
	return map[string]string{authz.SelectorKeyClientType: authz.AuthClientTypeRemoteSession, authz.SelectorKeyConnectionOwner: owner}
}

func mustAuthPolicy(t *testing.T, grants ...authz.Grant) DelegatedPolicy {
	t.Helper()
	policy, err := NewDelegatedPolicy(CurrentDelegatedPolicyVersion, grants)
	require.NoError(t, err)
	return policy
}

func TestMCPAuthIsRuntimeSafeOnlyFromVersion5(t *testing.T) {
	t.Parallel()

	for _, scope := range []authz.Scope{authz.ScopeMCPAuth, authz.ScopeMCPBlockedAuth} {
		require.True(t, IsRuntimeScopeSafe(RuntimeScopeRegistryVersion5, scope), scope)
		require.False(t, IsRuntimeScopeSafe(RuntimeScopeRegistryVersion4, scope), scope)
	}

	grant := authGrant("", authz.ScopeMCPAuth, "*", ownedBy(authz.SelectorValueRepresentedUser))
	_, err := NewDelegatedPolicy(DelegatedPolicyVersion4, []authz.Grant{grant})
	require.Error(t, err, "a version 4 policy cannot carry mcp:auth")
}

func TestSymbolicOwnerSurvivesPolicyRoundTrip(t *testing.T) {
	t.Parallel()

	grant := authGrant("", authz.ScopeMCPAuth, "*", ownedBy(authz.SelectorValueRepresentedUser))
	block := authGrant("", authz.ScopeMCPBlockedAuth, "client_a", ownedBy("user_x"))
	policy := mustAuthPolicy(t, grant, block)

	encoded, err := EncodeDelegatedPolicy(CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	decoded, err := DecodeDelegatedPolicy(CurrentDelegatedPolicyVersion, encoded)
	require.NoError(t, err)
	require.ElementsMatch(t, policy.RuntimeGrants(), decoded.RuntimeGrants())
	require.Contains(t, decoded.RuntimeGrants(), grant, "the marker is stored symbolically, never bound")

	_, err = DecodeDelegatedPolicy(DelegatedPolicyVersion4, encoded)
	require.Error(t, err, "older versions do not admit the new scopes")
}

func TestDelegationContainmentPreservesSymbolicOwner(t *testing.T) {
	t.Parallel()

	symbolic := authGrant("", authz.ScopeMCPAuth, "*", ownedBy(authz.SelectorValueRepresentedUser))
	concrete := authGrant("", authz.ScopeMCPAuth, "*", ownedBy("user_x"))
	anyOwner := authGrant("", authz.ScopeMCPAuth, "*", ownedBy("*"))
	unrestricted := authGrant("", authz.ScopeMCPAuth, "*", nil)

	cases := map[string]struct {
		delegated authz.Grant
		parent    authz.Grant
		contained bool
	}{
		"symbolic under symbolic":     {symbolic, symbolic, true},
		"symbolic under any owner":    {symbolic, anyOwner, true},
		"symbolic under unrestricted": {symbolic, unrestricted, true},
		"symbolic under concrete":     {symbolic, concrete, false},
		"concrete under symbolic":     {concrete, symbolic, false},
		"any owner under symbolic":    {anyOwner, symbolic, false},
		"unrestricted under symbolic": {unrestricted, symbolic, false},
	}
	for name, tc := range cases {
		contained, err := DelegationContained(mustAuthPolicy(t, tc.delegated), []authz.Grant{tc.parent})
		require.NoError(t, err, name)
		require.Equal(t, tc.contained, contained, name)
	}
}

func TestDelegationContainmentAccountsForConcreteOwnerRestrictions(t *testing.T) {
	t.Parallel()

	allow := authGrant("user:user_u", authz.ScopeMCPAuth, "*", ownedBy(authz.SelectorValueRepresentedUser))
	// An owner-scoped restriction on the agent: user_x blocked it on their connection.
	restriction := authGrant("agent:agent_1", authz.ScopeMCPBlockedAuth, "client_a", ownedBy("user_x"))
	parent := []authz.Grant{allow, restriction}

	uncarried := mustAuthPolicy(t, authGrant("", authz.ScopeMCPAuth, "*", ownedBy(authz.SelectorValueRepresentedUser)))
	contained, err := DelegationContained(uncarried, parent)
	require.NoError(t, err)
	require.False(t, contained, "unequal literals do not prove a symbolic owner disjoint from user_x")

	contained, err = DelegationContainedFor("user_u", uncarried, parent)
	require.NoError(t, err)
	require.True(t, contained, "user_x's restriction cannot reach a credential acting for user_u")

	contained, err = DelegationContainedFor("user_x", uncarried, parent)
	require.NoError(t, err)
	require.False(t, contained, "acting for user_x, the restriction must be carried")

	carried := mustAuthPolicy(t,
		authGrant("", authz.ScopeMCPAuth, "*", ownedBy(authz.SelectorValueRepresentedUser)),
		authGrant("", authz.ScopeMCPBlockedAuth, "client_a", ownedBy("user_x")),
	)
	contained, err = DelegationContained(carried, parent)
	require.NoError(t, err)
	require.True(t, contained, "carrying the concrete restriction keeps it concrete and contained")

	symbolicCarry := mustAuthPolicy(t,
		authGrant("", authz.ScopeMCPAuth, "*", ownedBy(authz.SelectorValueRepresentedUser)),
		authGrant("", authz.ScopeMCPBlockedAuth, "client_a", ownedBy(authz.SelectorValueRepresentedUser)),
	)
	contained, err = DelegationContained(symbolicCarry, parent)
	require.NoError(t, err)
	require.False(t, contained, "a symbolic carry does not cover a concrete owner pin")
}

func TestDelegableGrantsDoNotWidenSymbolicOwners(t *testing.T) {
	t.Parallel()

	symbolic := authGrant("", authz.ScopeMCPAuth, "*", ownedBy(authz.SelectorValueRepresentedUser))
	concrete := authGrant("", authz.ScopeMCPAuth, "*", ownedBy("user_x"))
	unrestricted := authGrant("", authz.ScopeMCPAuth, "*", nil)

	grants, err := DelegableGrants([]authz.Grant{unrestricted}, []authz.Grant{symbolic}, []authz.Grant{unrestricted})
	require.NoError(t, err)
	require.Equal(t, []authz.Grant{symbolic}, grants, "intersection keeps the symbolic constraint")

	grants, err = DelegableGrants([]authz.Grant{symbolic}, []authz.Grant{concrete}, []authz.Grant{unrestricted})
	require.NoError(t, err)
	require.Empty(t, grants, "symbolic and concrete owners intersect to nothing an allow can express")

	restriction := authGrant("agent:agent_1", authz.ScopeMCPBlockedAuth, "client_a", ownedBy("user_x"))
	derived, err := DelegatedRuntimeGrants([]authz.Grant{unrestricted, restriction}, []authz.Grant{symbolic}, []authz.Grant{unrestricted})
	require.NoError(t, err)
	require.Contains(t, derived, symbolic)
	require.Contains(t, derived, authz.Grant{PrincipalUrn: "", Scope: authz.ScopeMCPBlockedAuth, Selector: restriction.Selector}, "the concrete restriction is carried as-is")
}

func TestKnownRepresentedUserIgnoresOtherOwnersRestrictions(t *testing.T) {
	t.Parallel()

	restriction := authGrant("agent:agent_1", authz.ScopeMCPBlockedAuth, "client_a", ownedBy("user_x"))
	for name, allow := range map[string]authz.Grant{
		"absent owner":   authGrant("", authz.ScopeMCPAuth, "*", map[string]string{authz.SelectorKeyClientType: authz.AuthClientTypeRemoteSession}),
		"wildcard owner": authGrant("", authz.ScopeMCPAuth, "*", ownedBy("*")),
		"unrestricted":   authGrant("", authz.ScopeMCPAuth, "*", nil),
	} {
		parent := []authz.Grant{allow, restriction}
		policy := mustAuthPolicy(t, allow)

		contained, err := DelegationContainedFor("user_u", policy, parent)
		require.NoError(t, err, name)
		require.True(t, contained, "%s: user_x's restriction cannot reach user_u's credential", name)

		contained, err = DelegationContainedFor("user_x", policy, parent)
		require.NoError(t, err, name)
		require.False(t, contained, "%s: acting for user_x the restriction must be carried", name)

		contained, err = DelegationContained(policy, parent)
		require.NoError(t, err, name)
		require.False(t, contained, "%s: an unknown represented user stays conservative", name)
	}
}

func TestDelegableGrantsForKnownUserOffersSymbolicCandidates(t *testing.T) {
	t.Parallel()

	symbolic := authGrant("", authz.ScopeMCPAuth, "*", ownedBy(authz.SelectorValueRepresentedUser))
	restriction := authGrant("agent:agent_1", authz.ScopeMCPBlockedAuth, "client_a", ownedBy("user_x"))
	agent := []authz.Grant{symbolic, restriction}

	grants, err := DelegableGrantsFor("user_u", agent, []authz.Grant{symbolic}, []authz.Grant{symbolic})
	require.NoError(t, err)
	require.Equal(t, []authz.Grant{symbolic}, grants)

	grants, err = DelegableGrants(agent, []authz.Grant{symbolic}, []authz.Grant{symbolic})
	require.NoError(t, err)
	require.Empty(t, grants, "without a known user the uncarried restriction removes the candidate")
}

func TestAuthScopesAreNotIssuableYet(t *testing.T) {
	t.Parallel()

	require.False(t, IsIssuableScope(authz.ScopeMCPAuth))
	require.False(t, IsIssuableScope(authz.ScopeMCPBlockedAuth))
	require.True(t, IsIssuableScope(authz.ScopeMCPConnect))
}
