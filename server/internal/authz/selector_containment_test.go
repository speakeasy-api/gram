package authz

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGrantsContainSelectorWildcards(t *testing.T) {
	t.Parallel()
	broad := NewGrant(ScopeMCPWrite, "*")
	candidate := NewSelector(ScopeMCPConnect, "*")
	require.True(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	require.False(t, GrantsContainSelector([]Grant{NewGrant(ScopeMCPWrite, "one-server")}, ScopeMCPConnect, candidate))
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
