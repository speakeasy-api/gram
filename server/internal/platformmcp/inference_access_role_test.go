package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestInferenceAccessRoleGrantIsNarrowAndDistinctFromMCP(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	grant := inferenceAccessRoleGrant(project, false)
	require.Equal(t, string(authz.ScopeInferenceExecute), grant.Scope)
	require.Len(t, grant.Selectors, 1)
	require.Equal(t, "inference", grant.Selectors[0].ResourceKind)
	require.Equal(t, "*", grant.Selectors[0].ResourceID)
	require.Equal(t, project.String(), *grant.Selectors[0].ProjectID)
	role := &accessgen.Role{Grants: []*accessgen.RoleGrant{grant}}
	require.True(t, hasInferenceAccessRoleGrant(role, project, false))
	require.False(t, hasInferenceAccessRoleGrant(role, uuid.New(), false))
	// A broader grant is deliberately not reported as the exact grant managed here.
	allProjects := "*"
	grant.Selectors[0].ProjectID = &allProjects
	require.False(t, hasInferenceAccessRoleGrant(role, project, false))
	grant.Selectors[0].ProjectID = nil
	require.False(t, hasInferenceAccessRoleGrant(role, project, false))
	require.True(t, hasInferenceAccessRoleGrant(role, project, true))
	require.Nil(t, inferenceAccessRoleGrant(project, true).Selectors[0].ProjectID)
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

func TestExplicitAllProjectsInferenceGrantIsNotMCPAccess(t *testing.T) {
	t.Parallel()
	project := uuid.NewString()
	grants := []authz.Grant{{
		PrincipalUrn: "role:organization:test",
		Scope:        authz.ScopeInferenceExecute,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: "inference",
			authz.SelectorKeyResourceID:   "*",
			authz.SelectorKeyProjectID:    "*",
		},
		Inferred: false,
	}}
	allowed, err := authz.GrantsAuthorize(grants, authz.InferenceExecuteCheck(project))
	require.NoError(t, err)
	require.True(t, allowed, "an explicit all-projects grant authorizes inference in this project")
	allowed, err = authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPConnect, uuid.NewString(), project))
	require.NoError(t, err)
	require.False(t, allowed, "inference grants cannot cause MCP role delivery or exposure approval")
}
