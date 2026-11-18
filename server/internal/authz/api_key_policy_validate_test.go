package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAPIKeyPolicyGrant(t *testing.T) {
	t.Parallel()

	client := Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: "client_a"}
	require.NoError(t, ValidateAPIKeyPolicyGrant(ScopeMCPConnect, projectConnectSelector("p")))
	require.NoError(t, ValidateAPIKeyPolicyGrant(ScopeMCPAuth, client))
	require.NoError(t, ValidateAPIKeyPolicyGrant(ScopeMCPBlockedAuth, client))
	require.NoError(t, ValidateAPIKeyPolicyGrant(ScopeMCPAuth, Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: WildcardResource}))

	invalid := map[string]struct {
		scope    Scope
		selector Selector
	}{
		"org admin":       {ScopeOrgAdmin, NewSelector(ScopeOrgAdmin, "org")},
		"mcp write":       {ScopeMCPWrite, NewSelector(ScopeMCPWrite, "*")},
		"personal owner":  {ScopeMCPAuth, Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: "client_a", SelectorKeyClientType: AuthClientTypeRemoteSession, SelectorKeyConnectionOwner: SelectorValueRepresentedUser}},
		"remote session":  {ScopeMCPAuth, Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: "client_a", SelectorKeyClientType: AuthClientTypeRemoteSession}},
		"invalid sel":     {ScopeMCPConnect, Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: "*"}},
		"root":            {ScopeRoot, Selector{SelectorKeyResourceKind: WildcardResource, SelectorKeyResourceID: WildcardResource}},
		"hooks ingestion": {ScopeOrgHooksIngest, NewSelector(ScopeOrgHooksIngest, "org")},
	}
	for name, tc := range invalid {
		require.ErrorIs(t, ValidateAPIKeyPolicyGrant(tc.scope, tc.selector), ErrInvalidAPIKeyGrant, name)
	}
}
