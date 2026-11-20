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

	allowed, err := GrantsAuthorize(grants, MCPServer(MCPServerIDFrom(serverID), ProjectIDFrom("project-1")).Connect())
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, MCPServer(MCPServerIDFrom(serverID), ProjectIDFrom("project-1")).ToolCall(tool, ""))
	require.NoError(t, err)
	require.False(t, allowed)

	allowed, err = GrantsAuthorize(grants, MCPServer(MCPServerIDFrom(serverID), ProjectIDFrom("project-1")).ToolCall("list_tasks", ""))
	require.NoError(t, err)
	require.True(t, allowed)
}
