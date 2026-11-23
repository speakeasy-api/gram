package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolKindsOnServerListsTheKindsALimitedRoleAllows(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		roleGrant(ScopeMCPConnect, toolKindsServer, DispositionReadOnly),
		roleGrant(ScopeMCPConnect, toolKindsServer, DispositionUnannotated),
	}

	kinds, err := ToolKindsOnServer(grants, toolKindsServerRef())
	require.NoError(t, err)
	require.Equal(t, []string{DispositionReadOnly, DispositionUnannotated}, kinds.Allowed)
	require.Empty(t, kinds.Blocked)
}

func TestToolKindsOnServerListsEveryKindForAnUnlimitedRole(t *testing.T) {
	t.Parallel()

	grants := []Grant{roleGrant(ScopeMCPConnect, toolKindsServer, "")}

	kinds, err := ToolKindsOnServer(grants, toolKindsServerRef())
	require.NoError(t, err)
	require.Equal(t, Dispositions(), kinds.Allowed)
	require.Empty(t, kinds.Blocked)
}

func TestToolKindsOnServerListsTheKindsARoleBlocks(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		roleGrant(ScopeMCPConnect, toolKindsServer, ""),
		roleGrant(ScopeMCPBlockedConnect, toolKindsServer, DispositionDestructive),
	}

	kinds, err := ToolKindsOnServer(grants, toolKindsServerRef())
	require.NoError(t, err)
	require.NotContains(t, kinds.Allowed, DispositionDestructive)
	require.Len(t, kinds.Allowed, len(Dispositions())-1)
	require.Equal(t, []string{DispositionDestructive}, kinds.Blocked)
}

func TestToolKindsOnServerListsABlockOnARoleThatAllowsNothing(t *testing.T) {
	t.Parallel()

	grants := []Grant{roleGrant(ScopeMCPBlockedConnect, toolKindsServer, DispositionDestructive)}

	kinds, err := ToolKindsOnServer(grants, toolKindsServerRef())
	require.NoError(t, err)
	require.Empty(t, kinds.Allowed)
	require.Equal(t, []string{DispositionDestructive}, kinds.Blocked)
}

func TestToolKindsOnServerIgnoresRulesForAnotherServerOrProject(t *testing.T) {
	t.Parallel()

	otherServer := roleGrant(ScopeMCPConnect, "server-2", DispositionReadOnly)
	otherProject := roleGrant(ScopeMCPConnect, toolKindsServer, DispositionIdempotent)
	otherProject.Selector[SelectorKeyProjectID] = "project-2"
	grants := []Grant{otherServer, otherProject}

	kinds, err := ToolKindsOnServer(grants, toolKindsServerRef())
	require.NoError(t, err)
	require.Empty(t, kinds.Allowed)
	require.Empty(t, kinds.Blocked)
}

func TestToolKindsOnServerIgnoresRulesThatNameATool(t *testing.T) {
	t.Parallel()

	named := roleGrant(ScopeMCPConnect, toolKindsServer, DispositionReadOnly)
	named.Selector[SelectorKeyTool] = "list_tasks"
	grants := []Grant{named}

	kinds, err := ToolKindsOnServer(grants, toolKindsServerRef())
	require.NoError(t, err)
	require.Empty(t, kinds.Allowed)
}

const toolKindsServer = "server-1"

func toolKindsServerRef() MCPServerRef {
	return MCPServer(MCPServerIDFrom(toolKindsServer), ProjectIDFrom("project-1"))
}

func roleGrant(scope Scope, serverID, disposition string) Grant {
	selector := NewSelector(scope, serverID)
	if disposition != "" {
		selector[SelectorKeyDisposition] = disposition
	}
	return Grant{PrincipalUrn: "role:organization:role-1", Scope: scope, Selector: selector, Inferred: false}
}
