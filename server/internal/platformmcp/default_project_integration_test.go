package platformmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// defaultableSetupTools are the golden-path tools a new organization can call
// without naming its only, default project. Their authorization, audiences
// and discovery scopes are unchanged by defaulting the project.
var defaultableSetupTools = map[string]ToolMeta{
	"register_catalog_mcp":                  {Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable},
	"register_remote_mcp":                   {Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable},
	"attach_platform_mcp_identity_provider": {Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable},
	"get_mcp_readiness":                     {Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable},
	"get_my_install_instructions":           {Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeDefaultable, DiscoveryScopes: discoveryOrgReadOrMCPConnect},
}

func TestDefaultableSetupToolsKeepAuthorizationAndMakeTheProjectOptional(t *testing.T) {
	t.Parallel()

	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registrations := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, &recordingRegistrationStore{})
	registerCatalogRegistrationTool(live, registrations, nil)
	registerRemoteRegistrationTool(live, registrations, nil)
	registerIdentityProviderTool(live, registrations)
	registerReadinessTools(live, &ReadinessService{})
	registerPluginTools(live, nil)

	_, unavailable := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})

	for name, want := range defaultableSetupTools {
		for _, reg := range []*Registrar{live, unavailable} {
			descriptor := descriptorByName(t, reg, name)
			require.Equal(t, want, descriptor.Meta, name)
		}

		var schema struct {
			Required []string `json:"required"`
		}
		require.NoError(t, json.Unmarshal(descriptorByName(t, live, name).InputSchema, &schema), name)
		require.NotContains(t, schema.Required, "project_slug", name)
		require.NotContains(t, schema.Required, "project_id", name)
	}
}

type projectRecordingAttachment struct {
	mu       sync.Mutex
	projects []string
}

func (a *projectRecordingAttachment) Attach(_ context.Context, _ Principal, project ResolvedProject, _ uuid.UUID) (CatalogIdentityProviderAttachmentResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.projects = append(a.projects, project.Slug)
	return CatalogIdentityProviderAttachmentResult{Attached: true, ProviderURL: "https://idp.example.test"}, nil
}

func (a *projectRecordingAttachment) attachedProjects() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.projects...)
}

// addDefaultProject gives an organization its literal default project, set up
// like the seeded one so MCP servers can be added to it.
func addDefaultProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) ResolvedProject {
	t.Helper()

	row, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Default",
		Slug:           defaultProjectSlug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	seedRegistrationEligibleCohort(t, ctx, conn, row.ID)
	return ResolvedProject{ID: row.ID, Name: row.Name, Slug: row.Slug}
}

func invokeTool[Out any](t *testing.T, ctx context.Context, reg *Registrar, name string, arguments map[string]any) (Out, error) {
	t.Helper()

	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	output, err := descriptorByName(t, reg, name).Invoke(ctx, encoded)
	if err != nil {
		var zero Out
		return zero, err
	}
	typed, ok := output.(Out)
	require.True(t, ok, "%s returned %T", name, output)
	return typed, nil
}

func TestSetupToolsActOnTheDefaultProjectWhenNoneIsNamed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_default_project_setup")
	require.NoError(t, err)

	// The first organization has a named project and its default project.
	principal, namedProject := seedRegistrationLifecycle(t, ctx, conn)
	defaultProject := addDefaultProject(t, ctx, conn, principal.OrganizationID)
	// The second organization has no default project.
	withoutDefault, _ := seedRegistrationLifecycle(t, ctx, conn)

	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	dashboardURL, err := url.Parse("https://dashboard.example.test")
	require.NoError(t, err)
	attachment := &projectRecordingAttachment{}
	registrations := newRegistrationService(testCatalog{details: CatalogDetails{
		CatalogCandidate: CatalogCandidate{Name: "Reviewed MCP", ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"},
		Transport:        "streamable-http",
		remoteURL:        "https://reviewed.example.test/mcp",
	}}, gate, store).
		WithDirectRemoteInspector(&testDirectRemoteInspector{inspection: DirectRemoteInspection{CanonicalURL: "https://remote.example.test/mcp", Transport: "streamable-http", Trust: "user_supplied_unreviewed"}}).
		WithIdentityProviderAttachment(attachment).
		WithDashboardURL(dashboardURL)

	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerCatalogRegistrationTool(reg, registrations, nil)
	registerRemoteRegistrationTool(reg, registrations, nil)
	registerIdentityProviderTool(reg, registrations)
	registerReadinessTools(reg, NewReadinessService(store, gate, nil, nil, allowBudget()))

	callerCtx := ContextWithPrincipal(ctx, principal)
	withoutDefaultCtx := ContextWithPrincipal(ctx, withoutDefault)

	// An omitted project uses the default project.
	{
		catalog, err := invokeTool[RegisterCatalogMCPToolOutput](t, callerCtx, reg, "register_catalog_mcp", map[string]any{"provider_key": "provider", "catalog_ref": "reviewed/mcp", "idempotency_key": "default-catalog"})
		require.NoError(t, err)
		require.Equal(t, defaultProject.Slug, catalog.ProjectSlug)

		remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, callerCtx, reg, "register_remote_mcp", map[string]any{"remote_url": "https://remote.example.test/mcp", "idempotency_key": "default-remote"})
		require.NoError(t, err)
		require.Equal(t, defaultProject.Slug, remote.ProjectSlug)

		readiness, err := invokeTool[GetMCPReadinessToolOutput](t, callerCtx, reg, "get_mcp_readiness", map[string]any{"registration_id": remote.RegistrationID})
		require.NoError(t, err)
		require.Equal(t, defaultProject.Slug, readiness.ProjectSlug)
		require.Equal(t, remote.RegistrationID, readiness.RegistrationID)

		attached, err := invokeTool[AttachPlatformMCPIdentityProviderToolOutput](t, callerCtx, reg, "attach_platform_mcp_identity_provider", map[string]any{"registration_id": remote.RegistrationID, "confirmed": true})
		require.NoError(t, err)
		require.Equal(t, defaultProject.Slug, attached.ProjectSlug)
		require.True(t, attached.Attached)
		require.Equal(t, []string{defaultProject.Slug}, attachment.attachedProjects())
	}

	// A named project is still used.
	{
		catalog, err := invokeTool[RegisterCatalogMCPToolOutput](t, callerCtx, reg, "register_catalog_mcp", map[string]any{"project_slug": namedProject.Slug, "provider_key": "provider", "catalog_ref": "reviewed/mcp", "idempotency_key": "named-catalog"})
		require.NoError(t, err)
		require.Equal(t, namedProject.Slug, catalog.ProjectSlug)

		remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, callerCtx, reg, "register_remote_mcp", map[string]any{"project_slug": namedProject.Slug, "remote_url": "https://remote.example.test/mcp", "idempotency_key": "named-remote"})
		require.NoError(t, err)
		require.Equal(t, namedProject.Slug, remote.ProjectSlug)

		readiness, err := invokeTool[GetMCPReadinessToolOutput](t, callerCtx, reg, "get_mcp_readiness", map[string]any{"project_slug": namedProject.Slug, "registration_id": remote.RegistrationID})
		require.NoError(t, err)
		require.Equal(t, namedProject.Slug, readiness.ProjectSlug)

		// Over MCP, the attach result carries a sign-in card for its
		// authorization URL next to the unchanged structured output.
		reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
		reg.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
				return next(contextWithPrincipal(ctx, principal), method, req)
			}
		})
		result, err := connectTestClient(t, reg.server).CallTool(ctx, &mcp.CallToolParams{Name: "attach_platform_mcp_identity_provider", Arguments: map[string]any{"project_slug": namedProject.Slug, "registration_id": remote.RegistrationID, "confirmed": true}})
		require.NoError(t, err)
		require.False(t, result.IsError)
		structured, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		var attached AttachPlatformMCPIdentityProviderToolOutput
		require.NoError(t, json.Unmarshal(structured, &attached))
		require.NotEmpty(t, attached.AuthorizationURL)
		card, ok := result.Meta[cardMetaKey].(string)
		require.True(t, ok)
		require.Contains(t, card, `href="`+attached.AuthorizationURL+`"`)
		require.Contains(t, card, "Sign in with https://remote.example.test/mcp")
		require.Contains(t, card, ">Sign in</a>")
	}

	// An omitted project in an organization without a default project is refused.
	{
		registrationID := uuid.NewString()
		for name, arguments := range map[string]map[string]any{
			"register_catalog_mcp":                  {"provider_key": "provider", "catalog_ref": "reviewed/mcp", "idempotency_key": "missing-default"},
			"register_remote_mcp":                   {"remote_url": "https://remote.example.test/mcp", "idempotency_key": "missing-default"},
			"get_mcp_readiness":                     {"registration_id": registrationID},
			"attach_platform_mcp_identity_provider": {"registration_id": registrationID, "confirmed": true},
		} {
			encoded, err := json.Marshal(arguments)
			require.NoError(t, err)
			_, err = descriptorByName(t, reg, name).Invoke(withoutDefaultCtx, encoded)
			require.Equal(t, "default_project_not_found", refusalCode(t, err), name)
		}
	}
}

func TestInstallInstructionsUseTheDefaultProjectWhenNoneIsNamed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_default_project_install")
	require.NoError(t, err)

	defaultPrincipal, _ := seedRegistrationLifecycle(t, ctx, conn)
	defaultProject := addDefaultProject(t, ctx, conn, defaultPrincipal.OrganizationID)
	namedPrincipal, _ := seedRegistrationLifecycle(t, ctx, conn)

	rows, err := platformrepo.New(conn).ListPlatformMCPInventoryAuthorizationCandidates(ctx, defaultPrincipal.OrganizationID)
	require.NoError(t, err)
	var mcpID uuid.UUID
	for _, row := range rows {
		if row.ProjectID == defaultProject.ID {
			mcpID = row.ID
			break
		}
	}
	require.NotEqual(t, uuid.Nil, mcpID)

	serverURL, err := url.Parse(testCanonicalBaseURL)
	require.NoError(t, err)
	service := NewPluginsService(conn, allowBudget(), "default-project-install-key").
		WithAuthorization(authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)).
		WithInstallLinks(serverURL, serverURL)
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerPluginTools(reg, service)

	actorCtx := func(principal Principal) context.Context {
		sessionID := "session-default-project-install-" + principal.UserID
		actor := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, OrganizationSlug: "example-org", SessionID: &sessionID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
		actor = authz.GrantsToContext(actor, []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, mcpID.String())})
		return ContextWithPrincipal(actor, principal)
	}

	omitted, err := invokeTool[GetMyInstallInstructionsOutput](t, actorCtx(defaultPrincipal), reg, "get_my_install_instructions", map[string]any{"mcp_id": mcpID.String(), "client_family": string(OnboardingClientClaudeCode)})
	require.NoError(t, err)
	require.True(t, omitted.Supported)

	named, err := invokeTool[GetMyInstallInstructionsOutput](t, actorCtx(defaultPrincipal), reg, "get_my_install_instructions", map[string]any{"project_id": defaultProject.ID.String(), "mcp_id": mcpID.String(), "client_family": string(OnboardingClientClaudeCode)})
	require.NoError(t, err)
	require.Equal(t, named, omitted)

	_, err = invokeTool[GetMyInstallInstructionsOutput](t, actorCtx(namedPrincipal), reg, "get_my_install_instructions", map[string]any{"mcp_id": mcpID.String(), "client_family": string(OnboardingClientClaudeCode)})
	require.Equal(t, "default_project_not_found", refusalCode(t, err))
}
