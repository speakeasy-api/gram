package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mcpserversgen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// tunneledAccessFixture is a project with private MCP servers fronting one
// tunnel, created and given tool metadata through the same management service
// the dashboard uses.
type tunneledAccessFixture struct {
	principal Principal
	project   ResolvedProject
	reads     *AccessReadService
	roles     *AccessRoleMutationService
	manager   *access.RoleManager
	servers   *mcpservers.Service
	// dashboardCtx is a project member's dashboard request context.
	dashboardCtx context.Context
	tunnelID     uuid.UUID
}

func newTunneledAccessFixture(t *testing.T, ctx context.Context, database string) tunneledAccessFixture {
	t.Helper()

	conn, err := platformMCPInfra.CloneTestDatabase(t, database)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: principal.OrganizationID, Name: organization.Name, Slug: organization.Slug,
		WorkosID: conv.ToPGText("workos-" + uuid.NewString()), Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "access-tunneled-key")
	admissionFlags := &roleAdmissionOutsideTransactionFlags{Provider: flags, t: t, db: conn, evaluated: false}
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard(admissionFlags, nil))
	roles, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-tunneled-key", manager)
	require.NoError(t, err)

	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	dispositions := mcpservers.NewToolDispositionCache(logger, conn, cache.NewRedisCacheAdapter(redisClient))
	servers := mcpservers.NewService(logger, tracerProvider, conn, sessionManager, engine, audit.NewLogger(), nil, dispositions, false, nil, nil, networkaccess.DenyAllChecker{})

	dashboardCtx := contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: principal.OrganizationID, OrganizationSlug: organization.Slug,
		UserID: principal.UserID, ProjectID: &project.ID, ProjectSlug: &project.Slug,
	})

	tunnel, err := tunneledmcprepo.New(conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          project.ID,
		Name:               "access-tunnel-" + uuid.NewString()[:8],
		KeyHash:            "hash-" + uuid.NewString(),
		KeyPrefix:          "gram_tunnel_test",
		ResourceIdentifier: pgtype.Text{},
	})
	require.NoError(t, err)

	return tunneledAccessFixture{
		principal: principal, project: project, reads: reads, roles: roles, manager: manager,
		servers: servers, dashboardCtx: dashboardCtx, tunnelID: tunnel.ID,
	}
}

// createServer fronts the fixture's tunnel with a new private MCP server.
func (f tunneledAccessFixture) createServer(t *testing.T, name string) uuid.UUID {
	t.Helper()

	tunnelID := f.tunnelID.String()
	server, err := f.servers.CreateMcpServer(f.dashboardCtx, &mcpserversgen.CreateMcpServerPayload{
		Name:                name,
		TunneledMcpServerID: &tunnelID,
		Visibility:          types.McpServerVisibility(mcpservers.VisibilityPrivate),
	})
	require.NoError(t, err)
	return uuid.MustParse(server.ID)
}

// Tool metadata recorded for a tunneled server through the management service
// is the catalog the Platform MCP access tools read and validate role rules
// against, and the rules it writes name the fronting server, never the tunnel.
func TestPlatformAccessRolesUseTunneledMetadataFromTheManagementService(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newTunneledAccessFixture(t, ctx, "platform_mcp_access_tunneled")
	serverID := f.createServer(t, "JAMF")

	_, err := f.servers.AddToolMetadataBatch(f.dashboardCtx, &mcpserversgen.AddToolMetadataBatchPayload{
		McpServerID: serverID.String(),
		Tools: []*mcpserversgen.ToolMetadataForm{
			{ToolName: "list_devices", ReadOnlyHint: new(true)},
			{ToolName: "wipe_device", ReadOnlyHint: new(false), DestructiveHint: new(true)},
		},
	})
	require.NoError(t, err)

	read, err := f.reads.GetMCPAccess(ctx, f.principal, GetMCPAccessInput{ProjectID: f.project.ID.String(), MCPID: serverID.String()})
	require.NoError(t, err)
	require.Equal(t, "tunneled", read.MCP.Backend)
	require.Equal(t, "rbac", read.MCP.AuthorizationMode)
	require.Equal(t, "stored_metadata", read.MCP.ToolCatalog)
	require.Equal(t, []MCPAccessTool{{Name: "list_devices", Disposition: "read_only"}, {Name: "wipe_device", Disposition: "destructive"}}, read.MCP.Tools)

	_, err = f.roles.Create(ctx, f.principal, CreateMCPAccessRoleInput{
		ProjectID: f.project.ID.String(), Name: "Guessing role",
		Rules:          []MCPAccessRoleRule{{MCPID: serverID.String(), Tool: "erase_everything"}},
		IdempotencyKey: "create-guessing-role", Confirmed: true,
	})
	require.Error(t, err, "a tool missing from the stored catalog cannot be granted by name")

	created, err := f.roles.Create(ctx, f.principal, CreateMCPAccessRoleInput{
		ProjectID: f.project.ID.String(), Name: "Device readers",
		Rules:          []MCPAccessRoleRule{{MCPID: serverID.String(), Tool: "list_devices"}},
		IdempotencyKey: "create-device-readers", Confirmed: true,
	})
	require.NoError(t, err)

	roleID, err := f.reads.references.Decode(created.Role.Reference, f.principal, subjectKindAccessRole, f.reads.now())
	require.NoError(t, err)
	requireRoleToolGrants(t, ctx, f, roleID, serverID, []string{"list_devices"})

	updated, err := f.roles.Update(ctx, f.principal, UpdateMCPAccessRoleInput{
		ProjectID: f.project.ID.String(), RoleReference: created.Role.Reference, ExpectedVersion: created.Role.Version,
		AddRules:       []MCPAccessRoleRule{{MCPID: serverID.String(), Tool: "wipe_device"}},
		IdempotencyKey: "update-device-readers", Confirmed: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, updated.Role.Version)
	requireRoleToolGrants(t, ctx, f, roleID, serverID, []string{"list_devices", "wipe_device"})

	coverage, err := f.reads.GetMCPAccess(ctx, f.principal, GetMCPAccessInput{ProjectID: f.project.ID.String(), MCPID: serverID.String()})
	require.NoError(t, err)
	var role *MCPRoleCoverage
	for i := range coverage.Roles {
		if coverage.Roles[i].Name == "Device readers" {
			role = &coverage.Roles[i]
		}
	}
	require.NotNil(t, role)
	require.ElementsMatch(t, []string{"list_devices", "wipe_device"}, role.AllowedKnownTools)
}

// A tunneled server with nothing recorded has no catalog to check a tool name
// against, so exact-tool rules are refused rather than guessed.
func TestPlatformAccessRolesRefuseExactToolsWithoutTunneledMetadata(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newTunneledAccessFixture(t, ctx, "platform_mcp_access_tunneled_empty")
	serverID := f.createServer(t, "Unrecorded")

	read, err := f.reads.GetMCPAccess(ctx, f.principal, GetMCPAccessInput{ProjectID: f.project.ID.String(), MCPID: serverID.String()})
	require.NoError(t, err)
	require.Equal(t, "dynamic", read.MCP.ToolCatalog)
	require.Empty(t, read.MCP.Tools)

	_, err = f.roles.Create(ctx, f.principal, CreateMCPAccessRoleInput{
		ProjectID: f.project.ID.String(), Name: "Unrecorded tool role",
		Rules:          []MCPAccessRoleRule{{MCPID: serverID.String(), Tool: "list_devices"}},
		IdempotencyKey: "create-unrecorded", Confirmed: true,
	})
	require.Error(t, err)

	created, err := f.roles.Create(ctx, f.principal, CreateMCPAccessRoleInput{
		ProjectID: f.project.ID.String(), Name: "Whole server role",
		Rules:          []MCPAccessRoleRule{{MCPID: serverID.String()}},
		IdempotencyKey: "create-whole-server", Confirmed: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.Role.Reference)
}

// requireRoleToolGrants asserts the role's mcp:connect selectors name exactly
// these tools on the fronting server in its project.
func requireRoleToolGrants(t *testing.T, ctx context.Context, f tunneledAccessFixture, roleID string, serverID uuid.UUID, tools []string) {
	t.Helper()

	stored, err := f.manager.GetRoleByID(ctx, f.principal.OrganizationID, roleID)
	require.NoError(t, err)

	var named []string
	for _, grant := range stored.Grants {
		if grant.Scope != string(authz.ScopeMCPConnect) {
			continue
		}
		for _, selector := range grant.Selectors {
			require.Equal(t, serverID.String(), selector.ResourceID, "rules name the fronting server")
			require.NotEqual(t, f.tunnelID.String(), selector.ResourceID)
			require.NotNil(t, selector.ProjectID)
			require.Equal(t, f.project.ID.String(), *selector.ProjectID)
			require.NotNil(t, selector.Tool)
			named = append(named, *selector.Tool)
		}
	}
	require.ElementsMatch(t, tools, named)
}
