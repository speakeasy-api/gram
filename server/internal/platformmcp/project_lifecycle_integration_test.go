package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/projects"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type projectLifecycleFixture struct {
	principal Principal
	project   ResolvedProject
	service   *ProjectLifecycleService
	engine    *authz.Engine
	conn      *pgxpool.Pool
}

// seedProjectLifecycleFixture builds a live organization administrator. The
// tools re-check org:admin against the database, so the caller has to be a
// real administrator rather than merely hold the grant in a context.
func seedProjectLifecycleFixture(t *testing.T, ctx context.Context, name string) (context.Context, projectLifecycleFixture) {
	t.Helper()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, conn, principal.OrganizationID))
	seedPlatformMCPAuthorizationMember(t, ctx, conn, principal.OrganizationID, principal.UserID, authz.SystemRoleAdmin)

	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{
		authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID),
		authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource),
		authz.NewGrant(authz.ScopeProjectWrite, project.ID.String()),
	})
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service, err := NewProjectLifecycleService(
		testenv.NewLogger(t), conn,
		projects.NewCore(testenv.NewLogger(t), audit.NewLogger(), nil, false),
		engine, NewLiveOrgAdminAuthorizer(conn, engine), testOperationBudget(),
	)
	require.NoError(t, err)
	return ctx, projectLifecycleFixture{principal: principal, project: project, service: service, engine: engine, conn: conn}
}

func (f projectLifecycleFixture) create(ctx context.Context, name, key string) (ProjectMutationOutput, error) {
	return f.service.CreateProject(ctx, f.principal, CreateProjectInput{Name: name, IdempotencyKey: key, Confirmed: true})
}

func (f projectLifecycleFixture) organizationProjects(t *testing.T, ctx context.Context) []projectsrepo.Project {
	t.Helper()
	rows, err := projectsrepo.New(f.conn).ListProjectsByOrganization(ctx, f.principal.OrganizationID)
	require.NoError(t, err)
	return rows
}

func auditCount(t *testing.T, ctx context.Context, conn *pgxpool.Pool, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(ctx, conn, action)
	require.NoError(t, err)
	return count
}

// requireProjectLifecycleRefusal asserts a readable refusal with the given
// code and returns the message the caller would be shown.
func requireProjectLifecycleRefusal(t *testing.T, err error, code string) string {
	t.Helper()
	var refusal *ProjectLifecycleError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, code, refusal.Code)
	return refusal.Message
}

// An agent-created project must be indistinguishable from a dashboard-created
// one: the same derived slug, the same Default environment and Default plugin,
// the same audit entries, attributed to the same user, and visible in
// list_projects straight away.
func TestCreateProjectWritesTheDashboardSideRecordsAndIsListed(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project")
	projectCreates := auditCount(t, ctx, fixture.conn, audit.ActionProjectCreate)
	pluginCreates := auditCount(t, ctx, fixture.conn, audit.ActionPluginCreate)

	output, err := fixture.create(ctx, "Support Team", uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, "created", output.Outcome)
	require.Equal(t, "Support Team", output.Project.Name)
	require.Equal(t, "support-team", output.Project.Slug, "the slug is derived exactly as the dashboard derives it")
	require.Equal(t, "fresh_read_after_commit", output.SnapshotScope)
	require.False(t, output.Receipt.Replayed)
	projectID, err := uuid.Parse(output.Project.ID)
	require.NoError(t, err)

	row, err := projectsrepo.New(fixture.conn).GetProjectByID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, fixture.principal.OrganizationID, row.OrganizationID)

	environments, err := envrepo.New(fixture.conn).ListEnvironments(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, environments, 1, "the project gets exactly the Default environment")
	require.Equal(t, "Default", environments[0].Name)
	require.Equal(t, "default", environments[0].Slug)
	require.Equal(t, "Default project for organization", environments[0].Description.String)

	plugin, err := pluginsrepo.New(fixture.conn).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{OrganizationID: fixture.principal.OrganizationID, ProjectID: projectID})
	require.NoError(t, err, "the project gets its Default plugin")

	require.Equal(t, projectCreates+1, auditCount(t, ctx, fixture.conn, audit.ActionProjectCreate))
	require.Equal(t, pluginCreates+1, auditCount(t, ctx, fixture.conn, audit.ActionPluginCreate), "the Default plugin's creation is audited too")
	projectAudit, err := audittest.LatestAuditLogByAction(ctx, fixture.conn, audit.ActionProjectCreate)
	require.NoError(t, err)
	require.Equal(t, projectID.String(), projectAudit.SubjectID)
	require.Equal(t, fixture.principal.UserID, projectAudit.ActorID, "the creator is the Platform MCP user, as the dashboard records its session user")
	require.Equal(t, string(urn.PrincipalTypeUser), projectAudit.ActorType)
	pluginAudit, err := audittest.LatestAuditLogByAction(ctx, fixture.conn, audit.ActionPluginCreate)
	require.NoError(t, err)
	require.Equal(t, plugin.ID.String(), pluginAudit.SubjectID)
	require.Equal(t, fixture.principal.UserID, pluginAudit.ActorID)

	listed, err := NewPostgresReader(testenv.NewLogger(t), fixture.conn).WithAuthorization(fixture.engine).ListProjects(ctx, fixture.principal, ListProjectsInput{Limit: 100})
	require.NoError(t, err)
	require.Contains(t, listed.Projects, Project{ID: output.Project.ID, Name: "Support Team", Slug: "support-team"})
}

func TestCreateProjectReplaysARetryInsteadOfCreatingASecondProject(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project_replay")
	key := uuid.NewString()

	first, err := fixture.create(ctx, "Support Team", key)
	require.NoError(t, err)
	before := len(fixture.organizationProjects(t, ctx))
	projectCreates := auditCount(t, ctx, fixture.conn, audit.ActionProjectCreate)

	replay, err := fixture.create(ctx, "Support Team", key)
	require.NoError(t, err)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, first.Receipt.ID, replay.Receipt.ID)
	require.Equal(t, first.Project.ID, replay.Project.ID, "the retry returns the project the first call made")
	require.Len(t, fixture.organizationProjects(t, ctx), before, "no second project row")
	require.Equal(t, projectCreates, auditCount(t, ctx, fixture.conn, audit.ActionProjectCreate), "no second creation is audited")

	_, err = fixture.create(ctx, "Another Team", key)
	refusal := requireProjectLifecycleRefusal(t, err, "conflict")
	require.Contains(t, refusal, "different name")
	require.Len(t, fixture.organizationProjects(t, ctx), before)
}

func TestCreateProjectRefusesANameWhoseSlugIsTaken(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project_slug_taken")
	_, err := fixture.create(ctx, "Support Team", uuid.NewString())
	require.NoError(t, err)
	before := len(fixture.organizationProjects(t, ctx))
	projectCreates := auditCount(t, ctx, fixture.conn, audit.ActionProjectCreate)

	// A different display name that derives the same slug, which the dashboard
	// refuses as a conflict too.
	_, err = fixture.create(ctx, "support team!", uuid.NewString())
	refusal := requireProjectLifecycleRefusal(t, err, "conflict")
	require.Contains(t, refusal, `"support-team"`)
	require.Len(t, fixture.organizationProjects(t, ctx), before)
	require.Equal(t, projectCreates, auditCount(t, ctx, fixture.conn, audit.ActionProjectCreate))
}

func TestCreateProjectRefusesANameWithNoLettersOrDigits(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project_empty_slug")
	before := len(fixture.organizationProjects(t, ctx))

	_, err := fixture.create(ctx, "!!! ???", uuid.NewString())
	refusal := requireProjectLifecycleRefusal(t, err, "invalid_request")
	require.Contains(t, refusal, "letter or number")
	require.Len(t, fixture.organizationProjects(t, ctx), before)
}

func TestCreateProjectRequiresConfirmation(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project_unconfirmed")
	before := len(fixture.organizationProjects(t, ctx))

	_, err := fixture.service.CreateProject(ctx, fixture.principal, CreateProjectInput{Name: "Support Team", IdempotencyKey: uuid.NewString(), Confirmed: false})
	refusal := requireProjectLifecycleRefusal(t, err, "invalid_request")
	require.Contains(t, refusal, "Confirm")
	require.Len(t, fixture.organizationProjects(t, ctx), before)
}

func TestProjectLifecycleRefusesANonAdministrator(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_project_lifecycle_member")
	member := fixture.principal
	member.UserID = "user_" + uuid.NewString()
	seedPlatformMCPAuthorizationMember(t, ctx, fixture.conn, member.OrganizationID, member.UserID, authz.SystemRoleMember)
	before := len(fixture.organizationProjects(t, ctx))

	_, err := fixture.service.CreateProject(ctx, member, CreateProjectInput{Name: "Support Team", IdempotencyKey: uuid.NewString(), Confirmed: true})
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, string(authz.ScopeOrgAdmin), denied.RequiredScope)
	require.Len(t, fixture.organizationProjects(t, ctx), before)

	_, err = fixture.service.RenameProject(ctx, member, RenameProjectInput{ProjectID: fixture.project.ID.String(), Name: "Renamed", IdempotencyKey: uuid.NewString(), Confirmed: true})
	require.ErrorAs(t, err, &denied)
	require.Equal(t, string(authz.ScopeOrgAdmin), denied.RequiredScope)
	row, err := projectsrepo.New(fixture.conn).GetProjectByID(ctx, fixture.project.ID)
	require.NoError(t, err)
	require.Equal(t, fixture.project.Name, row.Name)
}

// A rename is metadata only: the slug that dashboard links and the
// Gram-Project header address the project by must not move.
func TestRenameProjectChangesTheNameAndNothingElse(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_rename_project")
	before, err := projectsrepo.New(fixture.conn).GetProjectByID(ctx, fixture.project.ID)
	require.NoError(t, err)
	projectCount := len(fixture.organizationProjects(t, ctx))
	updates := auditCount(t, ctx, fixture.conn, audit.ActionProjectUpdate)
	key := uuid.NewString()

	output, err := fixture.service.RenameProject(ctx, fixture.principal, RenameProjectInput{ProjectID: fixture.project.ID.String(), Name: "  Customer Support  ", IdempotencyKey: key, Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, "renamed", output.Outcome)
	require.Equal(t, before.Name, output.PreviousName)
	require.Equal(t, Project{ID: before.ID.String(), Name: "Customer Support", Slug: before.Slug}, output.Project)
	require.Equal(t, "fresh_read_after_commit", output.SnapshotScope)

	after, err := projectsrepo.New(fixture.conn).GetProjectByID(ctx, fixture.project.ID)
	require.NoError(t, err)
	require.Equal(t, "Customer Support", after.Name)
	require.Equal(t, before.Slug, after.Slug, "a rename never re-slugs")
	require.Equal(t, before.OrganizationID, after.OrganizationID)
	require.Equal(t, before.LogoAssetID, after.LogoAssetID)
	require.Equal(t, before.FunctionsRunnerVersion, after.FunctionsRunnerVersion)
	require.Equal(t, before.CreatedAt, after.CreatedAt)
	require.Equal(t, before.DeletedAt, after.DeletedAt)
	require.Len(t, fixture.organizationProjects(t, ctx), projectCount)

	require.Equal(t, updates+1, auditCount(t, ctx, fixture.conn, audit.ActionProjectUpdate))
	entry, err := audittest.LatestAuditLogByAction(ctx, fixture.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	require.Equal(t, fixture.principal.UserID, entry.ActorID)
	snapshotBefore, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	snapshotAfter, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, before.Name, snapshotBefore["Name"])
	require.Equal(t, "Customer Support", snapshotAfter["Name"])
	require.Equal(t, before.Slug, snapshotAfter["Slug"])

	replay, err := fixture.service.RenameProject(ctx, fixture.principal, RenameProjectInput{ProjectID: fixture.project.ID.String(), Name: "  Customer Support  ", IdempotencyKey: key, Confirmed: true})
	require.NoError(t, err)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, output.Receipt.ID, replay.Receipt.ID)
	require.Equal(t, updates+1, auditCount(t, ctx, fixture.conn, audit.ActionProjectUpdate), "a replay writes nothing")
}

func TestRenameProjectRefusesBeforeWriting(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_rename_project_refusals")
	updates := auditCount(t, ctx, fixture.conn, audit.ActionProjectUpdate)

	_, err := fixture.service.RenameProject(ctx, fixture.principal, RenameProjectInput{ProjectID: fixture.project.ID.String(), Name: "Renamed", IdempotencyKey: uuid.NewString(), Confirmed: false})
	require.Contains(t, requireProjectLifecycleRefusal(t, err, "invalid_request"), "Confirm")

	_, err = fixture.service.RenameProject(ctx, fixture.principal, RenameProjectInput{ProjectID: fixture.project.ID.String(), Name: "   ", IdempotencyKey: uuid.NewString(), Confirmed: true})
	require.Contains(t, requireProjectLifecycleRefusal(t, err, "invalid_request"), "1 to 40 characters")

	// A project the caller holds no project:write on answers with the
	// permission denial whether or not it exists, so the tool is no oracle.
	_, err = fixture.service.RenameProject(ctx, fixture.principal, RenameProjectInput{ProjectID: uuid.NewString(), Name: "Renamed", IdempotencyKey: uuid.NewString(), Confirmed: true})
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, string(authz.ScopeProjectWrite), denied.RequiredScope)

	require.Equal(t, updates, auditCount(t, ctx, fixture.conn, audit.ActionProjectUpdate))
	row, err := projectsrepo.New(fixture.conn).GetProjectByID(ctx, fixture.project.ID)
	require.NoError(t, err)
	require.Equal(t, fixture.project.Name, row.Name)
}

// The unavailable registration must advertise exactly what a composed
// deployment advertises, so the tools never appear and disappear as a rollout
// flips. The live side is a real service, or both paths would install the
// same handler and the comparison would prove nothing.
func TestProjectLifecycleUnavailableRegistrationMatchesLiveManifest(t *testing.T) {
	t.Parallel()
	_, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_project_lifecycle_manifest")
	require.True(t, fixture.service.valid(), "the live side must be a composed service")

	describe := func(service *ProjectLifecycleService) map[string]Descriptor {
		registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "project-lifecycle-manifest", Version: "0.0.1"}, nil))
		registerProjectLifecycleTools(registrar, service)
		byName := map[string]Descriptor{}
		for _, descriptor := range registrar.Descriptors() {
			byName[descriptor.Name] = descriptor
		}
		return byName
	}

	live := describe(fixture.service)
	unavailable := describe(nil)
	require.Len(t, unavailable, 2)
	require.Len(t, live, len(unavailable))
	for name, descriptor := range unavailable {
		other, ok := live[name]
		require.True(t, ok, "tool %q is registered on both paths", name)
		require.Equal(t, other.Title, descriptor.Title)
		require.Equal(t, other.Description, descriptor.Description)
		require.Equal(t, other.Meta, descriptor.Meta)
		require.Equal(t, other.Annotations, descriptor.Annotations)
		require.Equal(t, other.InputSchema, descriptor.InputSchema)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences, "%s", name)
		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization, "%s", name)
		require.Contains(t, descriptor.Description, "confirmed: true", "%s", name)
	}
	require.Equal(t, ProjectScopeNone, unavailable[createProjectToolName].Meta.ProjectScope, "there is no project until create_project makes one")
	require.Equal(t, ProjectScopeExplicit, unavailable[renameProjectToolName].Meta.ProjectScope)

	refusal := invokeUnavailable(t, unavailable[createProjectToolName], map[string]any{
		"name": "Support Team", "idempotency_key": uuid.NewString(), "confirmed": true,
	})
	require.Contains(t, refusal, "Creating or renaming projects is not available")
}
