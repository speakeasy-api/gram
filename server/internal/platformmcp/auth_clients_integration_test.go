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
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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
	require.Equal(t, authclients.StatusConfigured, settings.UpstreamAuthentication.Status, "the server offers enabled clients")
	require.True(t, settings.UpstreamAuthentication.RequiresUpstreamAuth)
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

func TestPlatformMCPOffersUpstreamCredentialOnAnotherServer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_offer_credential")
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

	shared, err := testrepo.New(conn).CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		ClientType: authz.AuthClientTypeServiceAccount, Name: "Operations service account",
	})
	require.NoError(t, err)
	// Another server already offers the credential, as get_mcp_connection_settings reports it.
	remote, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{ID: uuid.New(), ProjectID: project.ID, TransportType: "sse", Url: "https://source.example.com/mcp"})
	require.NoError(t, err)
	source, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: project.ID, Name: conv.ToPGText("Source"), Slug: conv.ToPGText("source-" + uuid.NewString()[:8]),
		RemoteMcpServerID: conv.ToNullUUID(remote.ID), Visibility: "private",
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).AssociateAuthClientFixture(ctx, testrepo.AssociateAuthClientFixtureParams{ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: source.ID, AuthClientID: shared}))

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "access-role-integration-key")
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard())
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", manager)
	require.NoError(t, err)
	service.WithAuthClientAudit(audit.NewLogger())

	input := OfferMCPUpstreamCredentialInput{ProjectID: project.ID.String(), MCPID: target.McpServerID.String(), AuthClientID: shared.String(), Confirmed: false}
	var mutationErr *AccessRoleMutationError
	_, err = service.OfferUpstreamCredential(ctx, principal, input)
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, "confirmation_required", mutationErr.Code)

	input.Confirmed = true
	got, err := service.OfferUpstreamCredential(ctx, principal, input)
	require.NoError(t, err)
	require.Len(t, got.UpstreamAuthentication.Clients, 1)
	require.Equal(t, shared.String(), got.UpstreamAuthentication.Clients[0].ID)

	sourceAuthentication, err := upstreamAuthentication(ctx, conn, principal.OrganizationID, source.ID)
	require.NoError(t, err)
	require.Len(t, sourceAuthentication.Clients, 1, "the source server keeps offering the credential")
	require.Equal(t, shared.String(), sourceAuthentication.Clients[0].ID)

	_, err = service.OfferUpstreamCredential(ctx, principal, input)
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, "conflict", mutationErr.Code, "a server already offering the credential is refused")

	missing := input
	missing.AuthClientID = uuid.NewString()
	_, err = service.OfferUpstreamCredential(ctx, principal, missing)
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, "not_found", mutationErr.Code)

	audits, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAuthClientAttachMcpServer)
	require.NoError(t, err)
	require.Equal(t, int64(1), audits)
}

func TestPlatformMCPChoosesAHeadlessCallersCredential(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_headless_credential")
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
	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: target.McpServerID, ProjectID: project.ID})
	require.NoError(t, err)
	require.True(t, server.UserSessionIssuerID.Valid, "the fixture server signs in through its own issuer")
	client, err := usersessionsrepo.New(conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		ClientID: "headless-" + uuid.NewString()[:8], ClientName: "Nightly job", RedirectUris: []string{}, TokenEndpointAuthMethod: "none", UserSessionIssuerID: server.UserSessionIssuerID.UUID,
	})
	require.NoError(t, err)
	shared, err := testrepo.New(conn).CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		ClientType: authz.AuthClientTypeServiceAccount, Name: "Operations service account",
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).AssociateAuthClientFixture(ctx, testrepo.AssociateAuthClientFixtureParams{ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: target.McpServerID, AuthClientID: shared}))

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "access-role-integration-key")
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard())
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", manager)
	require.NoError(t, err)
	service.WithAuthClientAudit(audit.NewLogger())

	input := ChooseMCPHeadlessCallerCredentialInput{ProjectID: project.ID.String(), MCPID: target.McpServerID.String(), UserSessionClientID: client.ID.String(), AuthClientID: shared.String(), Name: "Nightly job"}
	preview, err := service.ChooseHeadlessCallerCredential(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, []MCPHeadlessCallerChoice{{Name: "Nightly job", UserSessionClientID: client.ID.String()}}, preview.Callers, "a registered app needing its first choice is listed, and a preview changes nothing")

	unconfigured := input
	unconfigured.AuthClientID, unconfigured.Remove, unconfigured.Confirmed = "", true, true
	got, err := service.ChooseHeadlessCallerCredential(ctx, principal, unconfigured)
	require.NoError(t, err, "removing a choice a caller never had changes nothing")
	require.Len(t, got.Callers, 1)
	require.Empty(t, got.Callers[0].ProfileID)

	input.Confirmed = true
	got, err = service.ChooseHeadlessCallerCredential(ctx, principal, input)
	require.NoError(t, err)
	require.Len(t, got.Callers, 1, "a configured app is listed once")
	require.NotEmpty(t, got.Callers[0].ProfileID)
	require.True(t, got.Callers[0].Chosen)
	require.Equal(t, shared.String(), got.Callers[0].AuthClientID)
	require.Equal(t, client.ID.String(), got.Callers[0].UserSessionClientID)

	var mutationErr *AccessRoleMutationError
	both := input
	both.NoAuth = true
	_, err = service.ChooseHeadlessCallerCredential(ctx, principal, both)
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, "invalid_request", mutationErr.Code)

	noCaller := unconfigured
	noCaller.UserSessionClientID = ""
	_, err = service.ChooseHeadlessCallerCredential(ctx, principal, noCaller)
	require.ErrorAs(t, err, &mutationErr, "a removal must name exactly one caller")
	require.Equal(t, "invalid_request", mutationErr.Code)

	twoCallers := unconfigured
	twoCallers.WorkloadIssuerID, twoCallers.WorkloadSubject = uuid.NewString(), "job"
	_, err = service.ChooseHeadlessCallerCredential(ctx, principal, twoCallers)
	require.ErrorAs(t, err, &mutationErr, "a removal naming two callers is refused")
	require.Equal(t, "invalid_request", mutationErr.Code)

	remove := input
	remove.AuthClientID, remove.Remove = "", true
	got, err = service.ChooseHeadlessCallerCredential(ctx, principal, remove)
	require.NoError(t, err)
	require.Len(t, got.Callers, 1, "the caller keeps its profile without a choice for this server")
	require.False(t, got.Callers[0].Chosen)

	audits, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionIssuanceSelectionProfileCreate)
	require.NoError(t, err)
	require.Equal(t, int64(1), audits)
}
