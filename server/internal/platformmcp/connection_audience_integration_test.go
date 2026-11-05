package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// get_mcp_access reports who can connect to a server and the version a change
// must carry; set_mcp_connection_audience changes one audience's connect rule
// through the dashboard's save, with confirmation, a version check and an
// audit entry, and reports the audience after the change.
func TestPlatformMCPSetsConnectionAudience(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_connection_audience")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	// Access mutations are admitted only for an organization linked to its
	// identity provider.
	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: principal.OrganizationID, Name: organization.Name, Slug: organization.Slug,
		WorkosID: conv.ToPGText("workos-" + uuid.NewString()), Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	rows, err := platformrepo.New(conn).ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{
		OrganizationID: principal.OrganizationID, ConnectionID: uuid.NullUUID{}, ConnectionGeneration: uuid.NullUUID{}, SkipAuthorizationFilter: true,
		UserID: inventoryText(principal.UserID), ActingSurface: inventoryText(string(principal.surface())),
		ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, AfterMcpID: uuid.NullUUID{}, QueryText: "", ReadinessState: pgtype.Text{}, LimitValue: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	target := rows[0]
	seedAccessRole(t, ctx, conn, principal.OrganizationID, "ops", "Operations")

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "access-role-integration-key")
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard())
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", manager)
	require.NoError(t, err)

	read := func() GetMCPAccessOutput {
		t.Helper()
		output, err := reads.GetMCPAccess(ctx, principal, GetMCPAccessInput{ProjectID: project.ID.String(), MCPID: target.McpServerID.String()})
		require.NoError(t, err)
		return output
	}
	initial := read().ConnectionAudience
	require.Equal(t, access.ConnectRuleNotGranted, initial.Everyone)
	require.Empty(t, initial.Roles)
	require.NotEmpty(t, initial.Version)

	roleReference := ""
	roles, err := reads.ListRoles(ctx, principal)
	require.NoError(t, err)
	for _, role := range roles.Roles {
		if role.Name == "Operations" {
			roleReference = role.Reference
		}
	}
	require.NotEmpty(t, roleReference)

	input := SetMCPConnectionAudienceInput{
		ProjectID: project.ID.String(), MCPID: target.McpServerID.String(), Audience: "everyone",
		RoleReference: "", MemberReference: "", Access: access.ConnectRuleAllowed, ExpectedVersion: initial.Version, Confirmed: false,
	}
	var mutationErr *AccessRoleMutationError
	_, err = service.SetConnectionAudience(ctx, principal, input)
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, "confirmation_required", mutationErr.Code)

	input.Confirmed = true
	invalid := input
	invalid.Audience = "agent"
	_, err = service.SetConnectionAudience(ctx, principal, invalid)
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, "invalid_request", mutationErr.Code)

	auditsBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessResourceAudienceUpdate)
	require.NoError(t, err)

	everyone, err := service.SetConnectionAudience(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, access.ConnectRuleAllowed, everyone.ConnectionAudience.Everyone)
	require.NotEqual(t, initial.Version, everyone.ConnectionAudience.Version)

	// The first version is stale now: a change made against it is a conflict.
	stale := input
	stale.Access = access.ConnectRuleBlocked
	_, err = service.SetConnectionAudience(ctx, principal, stale)
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, "conflict", mutationErr.Code)

	role := input
	role.Audience, role.RoleReference, role.Access, role.ExpectedVersion = "role", roleReference, access.ConnectRuleBlocked, everyone.ConnectionAudience.Version
	blocked, err := service.SetConnectionAudience(ctx, principal, role)
	require.NoError(t, err)
	require.Equal(t, access.ConnectRuleAllowed, blocked.ConnectionAudience.Everyone, "a role rule leaves everyone's rule alone")
	require.Len(t, blocked.ConnectionAudience.Roles, 1)
	require.Equal(t, "Operations", blocked.ConnectionAudience.Roles[0].Name)
	require.Equal(t, access.ConnectRuleBlocked, blocked.ConnectionAudience.Roles[0].Access)

	// The read reports the committed state.
	reread := read().ConnectionAudience
	require.Equal(t, blocked.ConnectionAudience.Version, reread.Version)
	require.Equal(t, access.ConnectRuleAllowed, reread.Everyone)
	require.Len(t, reread.Roles, 1)

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessResourceAudienceUpdate)
	require.NoError(t, err)
	require.Equal(t, auditsBefore+2, auditsAfter)
}
