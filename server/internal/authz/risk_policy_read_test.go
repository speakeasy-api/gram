package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// risk_policy:read replaced org:admin on the risk read handlers, so it must
// behave exactly like org:admin for every org that reaches those handlers
// through an admin grant — including the org:blocked_admin exception, which
// only applies via scopeExclusions on the checked scope.
func TestRiskPolicyReadMatchesOrgAdmin(t *testing.T) {
	t.Parallel()

	const orgID = "org_1"
	check := Check{Scope: ScopeRiskPolicyRead, ResourceID: orgID}

	admin := []Grant{NewGrant(ScopeOrgAdmin, WildcardResource)}
	allowed, err := evaluateGrantCheck(admin, check)
	require.NoError(t, err)
	require.NotNil(t, allowed.Grant)

	blocked, err := evaluateGrantCheck(append(admin, NewGrant(ScopeOrgBlockedAdmin, orgID)), check)
	require.NoError(t, err)
	require.Nil(t, blocked.Grant)
	require.True(t, blocked.Denied)

	member := []Grant{NewGrant(ScopeOrgRead, WildcardResource)}
	denied, err := evaluateGrantCheck(member, check)
	require.NoError(t, err)
	require.Nil(t, denied.Grant)
}
