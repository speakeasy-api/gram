package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestBuiltinAccessReportsSharedCredentialRoles(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_builtin_auth_clients")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	serverID := builtinmcp.ID(project.ID, builtinmcp.Slack)
	reads := NewAccessReadService(testenv.NewLogger(t), conn, allowBudget(), "builtin-access-read-key")
	input := GetMCPAccessInput{ProjectID: project.ID.String(), MCPID: serverID.String()}

	// A code-owned built-in with no materialized row still has an empty,
	// non-null credential-role list and does not create any assignments.
	output, err := reads.GetMCPAccess(ctx, principal, input)
	require.NoError(t, err)
	require.NotNil(t, output.SharedAuthClientRoles)
	require.Empty(t, output.SharedAuthClientRoles)
	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"shared_auth_client_roles":[]`)

	// Store.get uses this same materialization path before normal auth client
	// associations are attached to the deterministic server identity.
	require.NoError(t, builtinmcp.EnsureServer(ctx, conn, principal.OrganizationID, project.ID, builtinmcp.Slack, uuid.NullUUID{}))
	fixtures := testrepo.New(conn)
	shared, err := fixtures.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		ClientType: authz.AuthClientTypeStaticCredential, Name: "Shared Slack credential",
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.AssociateAuthClientFixture(ctx, testrepo.AssociateAuthClientFixtureParams{
		ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: serverID, AuthClientID: shared,
	}))
	output, err = reads.GetMCPAccess(ctx, principal, input)
	require.NoError(t, err)
	require.NotNil(t, output.SharedAuthClientRoles)
	require.Empty(t, output.SharedAuthClientRoles)

	selector, err := authz.AuthClientSelector(shared.String(), "").MarshalJSON()
	require.NoError(t, err)
	for _, tc := range []struct {
		name  string
		scope authz.Scope
	}{
		{name: "Allowed", scope: authz.ScopeMCPAuth},
		{name: "Blocked", scope: authz.ScopeMCPBlockedAuth},
	} {
		role := seedAccessRole(t, ctx, conn, principal.OrganizationID, tc.name, tc.name)
		_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: principal.OrganizationID, PrincipalUrn: role, Scope: string(tc.scope), Selectors: selector,
		})
		require.NoError(t, err)
	}
	output, err = reads.GetMCPAccess(ctx, principal, input)
	require.NoError(t, err)
	require.Len(t, output.SharedAuthClientRoles, 2)
	byRole := map[string][]MCPRoleAuthClientAccess{}
	for _, role := range output.SharedAuthClientRoles {
		require.NotEmpty(t, role.Reference)
		require.False(t, role.UnevaluatedGrants)
		byRole[role.Name] = role.AuthClients
	}
	for name, access := range map[string]string{"Allowed": "allowed", "Blocked": "blocked"} {
		require.Equal(t, []MCPRoleAuthClientAccess{{AuthClientID: shared.String(), Name: "Shared Slack credential", ClientType: authz.AuthClientTypeStaticCredential, Access: access}}, byRole[name])
	}
	for _, role := range output.Roles {
		require.False(t, role.CanEnterServer, "credential permission does not grant connect")
		require.False(t, role.AssignmentEligible)
		require.Empty(t, role.MatchingPlugins)
	}
}
