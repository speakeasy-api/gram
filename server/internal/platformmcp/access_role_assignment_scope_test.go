package platformmcp

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestAccessRoleAssignmentEligibilityRejectsHiddenAccess(t *testing.T) {
	t.Parallel()
	project, mcp := uuid.New(), uuid.NewString()
	for _, test := range []struct {
		name     string
		alter    func(*accessgen.Role)
		eligible bool
	}{
		{name: "exact MCP", alter: func(*accessgen.Role) {}, eligible: true},
		{name: "system", alter: func(r *accessgen.Role) { r.IsSystem = true }},
		{name: "non MCP", alter: func(r *accessgen.Role) {
			r.Grants = append(r.Grants, &accessgen.RoleGrant{Scope: string(authz.ScopeOrgAdmin)})
		}},
		{name: "wildcard", alter: func(r *accessgen.Role) { r.Grants[0].Selectors[0].ResourceID = "*" }},
		{name: "other MCP", alter: func(r *accessgen.Role) { r.Grants[0].Selectors[0].ResourceID = uuid.NewString() }},
		{name: "other project", alter: func(r *accessgen.Role) { r.Grants[0].Selectors[0].ProjectID = new(uuid.NewString()) }},
		{name: "unknown disposition", alter: func(r *accessgen.Role) { r.Grants[0].Selectors[0].Disposition = new("unknown") }},
		{name: "no selectors", alter: func(r *accessgen.Role) { r.Grants[0].Selectors = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			role := &accessgen.Role{Grants: accessRoleRulesToGenGrants([]normalizedMCPAccessRoleRule{{MCPID: mcp}}, project)}
			test.alter(role)
			require.Equal(t, test.eligible, accessRoleAssignmentEligible(role, project.String(), mcp))
		})
	}
}

func TestAccessRoleAssignmentRulesDescribeCompleteScope(t *testing.T) {
	t.Parallel()
	project, mcp := uuid.New(), uuid.NewString()
	role := &accessgen.Role{Grants: accessRoleRulesToGenGrants([]normalizedMCPAccessRoleRule{
		{MCPID: mcp}, {MCPID: mcp, Tool: "list_tasks", Disposition: authz.DispositionReadOnly},
		{MCPID: mcp, Tool: "*"}, {MCPID: mcp, Tool: "*", Disposition: authz.DispositionReadOnly},
	}, project)}
	require.Equal(t, []AccessRoleAssignmentRule{
		{AllTools: true}, {Tool: "list_tasks", Disposition: authz.DispositionReadOnly},
		{AllTools: true}, {Disposition: authz.DispositionReadOnly},
	}, accessRoleAssignmentRules(role, project.String(), mcp))
	role.Grants = append(role.Grants, &accessgen.RoleGrant{Scope: string(authz.ScopeProjectRead)})
	require.Nil(t, accessRoleAssignmentRules(role, project.String(), mcp))
}

func TestAccessMemberVersionNormalizesRolelessState(t *testing.T) {
	t.Parallel()
	key := make([]byte, 32)
	nilVersion, err := accessMemberRoleVersion(key, "member", nil)
	require.NoError(t, err)
	emptyVersion, err := accessMemberRoleVersion(key, "member", []string{})
	require.NoError(t, err)
	require.Equal(t, nilVersion, emptyVersion)
}

func TestOversizedAssignmentReceiptIsNotFeatureDisabled(t *testing.T) {
	t.Parallel()
	_, err := encodeAccessRoleAssignmentReceipt(AccessRoleAssignmentReceiptResult{
		MaskedIdentity: "Selected member", AssignedRole: "Operators", Roles: []string{strings.Repeat("x", maxAccessRoleAssignmentReceiptPayloadBytes)},
		Version: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ResultCategory: "assigned", Reconciliation: "pending",
	})
	require.ErrorIs(t, err, ErrAccessRoleMutationUnavailable)
	require.Contains(t, err.Error(), "temporarily unavailable")
	require.NotContains(t, err.Error(), "not enabled")
}
