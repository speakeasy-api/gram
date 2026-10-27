package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestPlatformMCPReportsUpstreamAuthClients(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_auth_clients")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	rows, err := platformrepo.New(conn).ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{
		OrganizationID: principal.OrganizationID, ConnectionID: uuid.NullUUID{}, ConnectionGeneration: uuid.NullUUID{}, SkipAuthorizationFilter: true,
		UserID: inventoryText(principal.UserID), ActingSurface: inventoryText(string(principal.surface())),
		ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, AfterMcpID: uuid.NullUUID{}, QueryText: "", ReadinessState: pgtype.Text{}, LimitValue: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	target := rows[0]

	fixtures := testrepo.New(conn)
	shared, err := fixtures.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		ClientType: authz.AuthClientTypeServiceAccount, Name: "Operations service account",
	})
	require.NoError(t, err)
	personal, err := fixtures.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		ClientType: authz.AuthClientTypeRemoteSession, Name: "Each person's account",
	})
	require.NoError(t, err)
	for _, client := range []uuid.UUID{shared, personal} {
		require.NoError(t, fixtures.AssociateAuthClientFixture(ctx, testrepo.AssociateAuthClientFixtureParams{
			ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: target.McpServerID, AuthClientID: client,
		}))
	}

	settingsCtx := authz.GrantsToContext(ctx, []authz.Grant{
		authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID),
		authz.NewGrant(authz.ScopeMCPRead, target.McpServerID.String()),
	})
	settings, err := NewMCPConnectionSettingsService(conn).Get(settingsCtx, principal, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: target.McpServerID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, settings.UpstreamAuthentication)
	require.Equal(t, authclients.StatusUndecided, settings.UpstreamAuthentication.Status)
	require.Len(t, settings.UpstreamAuthentication.Clients, 2)

	role := seedAccessRole(t, ctx, conn, principal.OrganizationID, "ops", "Operations")
	selector, err := authz.AuthClientSelector(shared.String(), "").MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: principal.OrganizationID, PrincipalUrn: role, Scope: string(authz.ScopeMCPAuth), Selectors: selector,
	})
	require.NoError(t, err)

	blockedRole := seedAccessRole(t, ctx, conn, principal.OrganizationID, "contractors", "Contractors")
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: principal.OrganizationID, PrincipalUrn: blockedRole, Scope: string(authz.ScopeMCPBlockedAuth), Selectors: selector,
	})
	require.NoError(t, err)

	narrowedRole := seedAccessRole(t, ctx, conn, principal.OrganizationID, "narrowed", "Narrowed")
	narrowed := authz.AuthClientSelector(shared.String(), "")
	narrowed[authz.SelectorKeyUpstreamClient] = uuid.NewString()
	narrowedSelector, err := narrowed.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: principal.OrganizationID, PrincipalUrn: narrowedRole, Scope: string(authz.ScopeMCPAuth), Selectors: narrowedSelector,
	})
	require.NoError(t, err)

	unevaluatedRole := seedAccessRole(t, ctx, conn, principal.OrganizationID, "unevaluated", "Unevaluated")
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: principal.OrganizationID, PrincipalUrn: unevaluatedRole, Scope: string(authz.ScopeMCPAuth),
		Selectors: []byte(`{"resource_kind":"mcp_auth_client","resource_id":"` + shared.String() + `","tool":"not-an-auth-client-dimension"}`),
	})
	require.NoError(t, err)

	reads := NewAccessReadService(testenv.NewLogger(t), conn, allowBudget(), "access-read-key")
	for _, visibility := range []string{"private", "public", "disabled"} {
		require.NoError(t, testrepo.New(conn).SetMCPServerVisibilityFixture(ctx, testrepo.SetMCPServerVisibilityFixtureParams{Visibility: visibility, ID: target.McpServerID, ProjectID: project.ID}))
		output, err := reads.GetMCPAccess(ctx, principal, GetMCPAccessInput{ProjectID: project.ID.String(), MCPID: target.McpServerID.String()})
		require.NoError(t, err)
		byRole := make(map[string][]MCPRoleAuthClientAccess, len(output.SharedAuthClientRoles))
		unevaluated := make(map[string]bool, len(output.SharedAuthClientRoles))
		for _, role := range output.SharedAuthClientRoles {
			require.NotEmpty(t, role.Reference)
			byRole[role.Name] = role.AuthClients
			unevaluated[role.Name] = role.UnevaluatedGrants
		}
		require.True(t, unevaluated["Unevaluated"], "%s: a role whose credential rule cannot be evaluated is reported as incomplete", visibility)
		require.False(t, unevaluated["Operations"], visibility)
		require.Equal(t, []MCPRoleAuthClientAccess{{
			AuthClientID: shared.String(), Name: "Operations service account", ClientType: authz.AuthClientTypeServiceAccount, Access: "allowed",
		}}, byRole["Operations"], "%s: a role with only an auth client rule is reported and personal clients are not", visibility)
		require.Equal(t, "blocked", byRole["Contractors"][0].Access, "%s: a block with no grant is reported as blocked", visibility)
		require.NotContains(t, byRole, "Narrowed", "%s: a grant narrowed to one upstream client does not cover the shared client", visibility)
		for _, role := range output.Roles {
			require.NotContains(t, []string{"Operations", "Contractors", "Narrowed"}, role.Name, "%s: credential rules alone do not give a role server coverage", visibility)
		}
		if visibility != "private" {
			require.Empty(t, output.Roles, "%s: no role decides entry", visibility)
		}
	}
}

func TestPlatformMCPSetsRoleAuthClientAccess(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_auth_client_access")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

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

	fixtures := testrepo.New(conn)
	shared, err := fixtures.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		ClientType: authz.AuthClientTypeServiceAccount, Name: "Operations service account",
	})
	require.NoError(t, err)
	personal, err := fixtures.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		ClientType: authz.AuthClientTypeRemoteSession, Name: "Each person's account",
	})
	require.NoError(t, err)
	for _, client := range []uuid.UUID{shared, personal} {
		require.NoError(t, fixtures.AssociateAuthClientFixture(ctx, testrepo.AssociateAuthClientFixtureParams{
			ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: target.McpServerID, AuthClientID: client,
		}))
	}
	seedAccessRole(t, ctx, conn, principal.OrganizationID, "ops", "Operations")

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "access-role-integration-key")
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard())
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", manager)
	require.NoError(t, err)

	roles, err := reads.ListRoles(ctx, principal)
	require.NoError(t, err)
	reference := ""
	for _, role := range roles.Roles {
		if role.Name == "Operations" {
			reference = role.Reference
		}
	}
	require.NotEmpty(t, reference)
	input := SetMCPRoleAuthClientAccessInput{
		ProjectID: project.ID.String(), MCPID: target.McpServerID.String(), RoleReference: reference,
		AuthClientID: shared.String(), Access: "blocked", Confirmed: true,
	}

	_, err = service.SetRoleAuthClientAccess(ctx, principal, input)
	var mutationErr *AccessRoleMutationError
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, unavailableCode, mutationErr.Code, "without an audit logger the tool stays unavailable")
	service.WithAuthClientAudit(audit.NewLogger())

	unconfirmed := input
	unconfirmed.Confirmed = false
	_, err = service.SetRoleAuthClientAccess(ctx, principal, unconfirmed)
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, "confirmation_required", mutationErr.Code)

	personalInput := input
	personalInput.AuthClientID = personal.String()
	_, err = service.SetRoleAuthClientAccess(ctx, principal, personalInput)
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, "invalid_request", mutationErr.Code, "personal connections are not set per role")

	for _, step := range []struct{ access, want string }{
		{"blocked", "blocked"},
		{"allowed", "allowed"},
		{"not_granted", "not_granted"},
	} {
		input.Access = step.access
		got, err := service.SetRoleAuthClientAccess(ctx, principal, input)
		require.NoError(t, err)
		require.Equal(t, "Operations", got.Role)
		require.Equal(t, step.want, got.AuthClient.Access)
	}

	audits, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAuthClientAudienceUpdate)
	require.NoError(t, err)
	require.Equal(t, int64(3), audits)
}
