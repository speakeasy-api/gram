package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestMemoryAccessRoleGrantAuthorizesOnlyItsProject(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	for _, scope := range []authz.Scope{authz.ScopeMemoryRead, authz.ScopeMemoryWrite} {
		parsed, ok := memoryAccessRoleScope(" " + string(scope) + " ")
		require.True(t, ok)
		require.Equal(t, scope, parsed)

		grant := memoryAccessRoleGrant(scope, project)
		require.Equal(t, string(scope), grant.Scope)
		require.True(t, hasMemoryAccessRoleGrant(&accessgen.Role{Grants: []*accessgen.RoleGrant{grant}}, scope, project))
		require.False(t, hasMemoryAccessRoleGrant(&accessgen.Role{Grants: []*accessgen.RoleGrant{grant}}, scope, uuid.New()))

		// The stored selector authorizes the scope the team memory tools check.
		selector := grant.Selectors[0]
		grants := []authz.Grant{{PrincipalUrn: "role:organization:test", Scope: scope, Selector: authz.Selector{authz.SelectorKeyResourceKind: selector.ResourceKind, authz.SelectorKeyResourceID: selector.ResourceID}, Inferred: false}}
		allowed, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeMemoryRead, ResourceKind: "", ResourceID: project.String(), Dimensions: nil})
		require.NoError(t, err)
		require.True(t, allowed, "%s on the project allows memory:read there", scope)
		allowed, err = authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeMemoryRead, ResourceKind: "", ResourceID: uuid.NewString(), Dimensions: nil})
		require.NoError(t, err)
		require.False(t, allowed, "a memory grant never reaches another project")
	}
	for _, other := range []string{"", "chat:read", "log:read", "memory:*", "project:write"} {
		_, ok := memoryAccessRoleScope(other)
		require.False(t, ok, other)
	}
	require.True(t, accessRoleMutationOperation(operationConfigureMemoryAccessRole))
}
