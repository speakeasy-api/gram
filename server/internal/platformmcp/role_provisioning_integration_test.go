package platformmcp

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestRoleProvisioningSharedConfigureIntegration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_role_provisioning")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	ctx = authz.GrantsToContext(contextWithPrincipal(ctx, principal), []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)})
	backend := provisioningBackend(NewPostgresReader(testenv.NewLogger(t), conn))
	require.NotNil(t, backend)
	status, err := backend.Status(ctx, principal.OrganizationID)
	require.NoError(t, err)
	in := ConfigureRoleProvisioningInput{OrganizationID: principal.OrganizationID, ProjectID: project.ID.String(), ExpectedVersion: status.Version, Enabled: true, Roles: []ConfigureRoleProvisioningSelection{}, Confirmed: true}
	out, err := configureRoleProvisioning(ctx, principal, backend, in)
	require.NoError(t, err)
	require.Equal(t, status.Version+1, out.Version)
	require.NotNil(t, out.Status)
	require.True(t, out.Status.Enabled)
	require.Equal(t, project.ID.String(), out.Status.ProjectID)
	_, err = configureRoleProvisioning(ctx, principal, backend, in)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "version_conflict", refusal.Code)
	// An existing destination in another tenant cannot be persisted through
	// MCP: the same transactional service as HTTP rejects it.
	foreignPrincipal, foreignProject := seedRegistrationLifecycle(t, ctx, conn)
	require.NotEqual(t, principal.OrganizationID, foreignPrincipal.OrganizationID)
	in.ExpectedVersion = out.Version
	in.ProjectID = foreignProject.ID.String()
	_, err = configureRoleProvisioning(ctx, principal, backend, in)
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_configuration", refusal.Code)
	latest, err := backend.Status(ctx, principal.OrganizationID)
	require.NoError(t, err)
	require.True(t, latest.Enabled)
	require.Equal(t, out.Version, latest.Version)
	require.Equal(t, project.ID, latest.ProjectID.UUID)
}
