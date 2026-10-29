package authz

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGrantsContainSelectorWildcards(t *testing.T) {
	t.Parallel()
	broad := NewGrant(ScopeMCPConnect, "*")
	candidate := NewSelector(ScopeMCPConnect, "*")
	require.True(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	require.False(t, GrantsContainSelector([]Grant{NewGrant(ScopeMCPConnect, "one-server")}, ScopeMCPConnect, candidate))
	require.False(t, GrantsContainSelector([]Grant{NewGrant(ScopeMCPWrite, "*")}, ScopeMCPConnect, candidate), "configuring servers never contains using them")
	broad.Selector[SelectorKeyTool] = "allowed-tool"
	require.False(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	candidate[SelectorKeyTool] = "allowed-tool"
	require.True(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	candidate[SelectorKeyTool] = "other-tool"
	require.False(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	// Delegation support must not relax runtime's concrete-resource requirement.
	_, err := GrantsAuthorize([]Grant{broad}, MCPCheck(ScopeMCPConnect, "*", "example-project"))
	require.ErrorIs(t, err, ErrInvalidCheck)
}
