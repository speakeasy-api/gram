package platformmcp

import (
	"github.com/google/uuid"
	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInferenceAccessRoleGrantIsNarrowAndDistinctFromMCP(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	grant := inferenceAccessRoleGrant(project)
	require.Equal(t, string(authz.ScopeInferenceExecute), grant.Scope)
	require.Len(t, grant.Selectors, 1)
	require.Equal(t, "inference", grant.Selectors[0].ResourceKind)
	require.Equal(t, "*", grant.Selectors[0].ResourceID)
	require.Equal(t, project.String(), *grant.Selectors[0].ProjectID)
	role := &accessgen.Role{Grants: []*accessgen.RoleGrant{grant}}
	require.True(t, hasInferenceAccessRoleGrant(role, project))
	require.False(t, hasInferenceAccessRoleGrant(role, uuid.New()))
	// A broader grant is deliberately not reported as the exact grant managed here.
	grant.Selectors[0].ProjectID = nil
	require.False(t, hasInferenceAccessRoleGrant(role, project))
	mcp := accessRoleRulesToGenGrants([]normalizedMCPAccessRoleRule{{MCPID: uuid.NewString()}}, project)
	require.Equal(t, string(authz.ScopeMCPConnect), mcp[0].Scope)
	require.Equal(t, "mcp", mcp[0].Selectors[0].ResourceKind)
	require.True(t, accessRoleMutationOperation(operationConfigureInferenceAccessRole))
	inferenceHash, err := accessRoleMutationInputHash(operationConfigureInferenceAccessRole, "same-input")
	require.NoError(t, err)
	mcpHash, err := accessRoleMutationInputHash(operationUpdateMCPAccessRole, "same-input")
	require.NoError(t, err)
	require.NotEqual(t, inferenceHash, mcpHash)
}
