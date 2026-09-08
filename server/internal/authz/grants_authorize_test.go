package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrantsAuthorizeAppliesMCPExclusions(t *testing.T) {
	t.Parallel()

	serverID := "server-1"
	tool := "delete_tasks"
	allow := NewSelector(ScopeMCPConnect, serverID)
	block := NewSelector(ScopeMCPBlockedConnect, serverID)
	block[SelectorKeyTool] = tool
	grants := []Grant{
		{PrincipalUrn: "role:organization:role-1", Scope: ScopeMCPConnect, Selector: allow},
		{PrincipalUrn: "role:organization:role-1", Scope: ScopeMCPBlockedConnect, Selector: block},
	}

	allowed, err := GrantsAuthorize(grants, MCPCheck(ScopeMCPConnect, serverID, "project-1"))
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, MCPToolCallCheck(serverID, MCPToolCallDimensions{Tool: tool, ProjectID: "project-1"}))
	require.NoError(t, err)
	require.False(t, allowed)

	allowed, err = GrantsAuthorize(grants, MCPToolCallCheck(serverID, MCPToolCallDimensions{Tool: "list_tasks", ProjectID: "project-1"}))
	require.NoError(t, err)
	require.True(t, allowed)
}
