package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/projects"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
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

// A create receipt expires, after which the same key no longer replays. That
// must not become a second project: the slug is derived from the name and is
// unique among the organization's live projects, so a late retry of the same
// name is refused as a conflict — and still is after the first project was
// renamed, because a rename keeps the slug.
func TestCreateProjectRetryAfterReceiptExpiryCannotDuplicate(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project_expired_receipt")
	// Written already expired, as if the receipt lifetime had passed.
	fixture.service.now = func() time.Time { return time.Now().Add(-2 * receiptLifetime) }
	key := uuid.NewString()

	first, err := fixture.create(ctx, "Support Team", key)
	require.NoError(t, err)
	before := len(fixture.organizationProjects(t, ctx))

	_, err = fixture.create(ctx, "Support Team", key)
	require.Contains(t, requireProjectLifecycleRefusal(t, err, "conflict"), `"support-team"`)
	require.Len(t, fixture.organizationProjects(t, ctx), before)

	grants, _ := authz.GrantsFromContext(ctx)
	ctx = authz.GrantsToContext(ctx, append(grants, authz.NewGrant(authz.ScopeProjectWrite, first.Project.ID)))
	_, err = fixture.service.RenameProject(ctx, fixture.principal, RenameProjectInput{ProjectID: first.Project.ID, Name: "Customer Support", IdempotencyKey: uuid.NewString(), Confirmed: true})
	require.NoError(t, err)
	_, err = fixture.create(ctx, "Support Team", key)
	require.Contains(t, requireProjectLifecycleRefusal(t, err, "conflict"), `"support-team"`)
	require.Len(t, fixture.organizationProjects(t, ctx), before, "the renamed project still holds its slug")
}

// A replay writes nothing, so it must not spend the write allowance: an agent
// retrying a lost response after the allowance ran out still gets the result
// it is owed, while a genuinely new write is throttled.
func TestProjectLifecycleReplaysAreNotCharged(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_project_lifecycle_replay_budget")
	createKey, renameKey := uuid.NewString(), uuid.NewString()
	rename := RenameProjectInput{ProjectID: fixture.project.ID.String(), Name: "Customer Support", IdempotencyKey: renameKey, Confirmed: true}

	_, err := fixture.create(ctx, "Support Team", createKey)
	require.NoError(t, err)
	_, err = fixture.service.RenameProject(ctx, fixture.principal, rename)
	require.NoError(t, err)

	fixture.service.changes = OperationBudget{Connection: denyOperationLimiter{}, Organization: denyOperationLimiter{}}
	replayed, err := fixture.create(ctx, "Support Team", createKey)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	renamed, err := fixture.service.RenameProject(ctx, fixture.principal, rename)
	require.NoError(t, err)
	require.True(t, renamed.Receipt.Replayed)

	_, err = fixture.create(ctx, "Another Team", uuid.NewString())
	requireProjectLifecycleRefusal(t, err, "rate_limited")
	_, err = fixture.service.RenameProject(ctx, fixture.principal, RenameProjectInput{ProjectID: fixture.project.ID.String(), Name: "Renamed Again", IdempotencyKey: uuid.NewString(), Confirmed: true})
	requireProjectLifecycleRefusal(t, err, "rate_limited")
}

// poolInspectingLimiter allows every charge and records how many connections
// its pool had checked out at that moment. A charge made inside an open
// transaction always sees at least the transaction's own connection.
type poolInspectingLimiter struct {
	pool     *pgxpool.Pool
	acquired *[]int32
}

func (l poolInspectingLimiter) Allow(ctx context.Context, key string) (ratelimit.Result, error) {
	return l.AllowN(ctx, key, 1)
}

func (l poolInspectingLimiter) AllowN(context.Context, string, int) (ratelimit.Result, error) {
	*l.acquired = append(*l.acquired, l.pool.Stat().AcquiredConns())
	return ratelimit.Result{Allowed: true, Remaining: 1, RetryAfter: 0}, nil
}

// The limiter is a network call. Charging it while a transaction is open would
// hold a PostgreSQL connection and the receipt lock for as long as the limiter
// takes, so a slow limiter could pin connections on every write. Every charge
// must therefore happen with no connection checked out.
func TestProjectLifecycleChargesTheBudgetOutsideAnyTransaction(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_project_lifecycle_budget_no_tx")
	acquired := &[]int32{}
	limiter := poolInspectingLimiter{pool: fixture.conn, acquired: acquired}
	fixture.service.changes = OperationBudget{Connection: limiter, Organization: limiter}

	_, err := fixture.create(ctx, "Support Team", uuid.NewString())
	require.NoError(t, err)
	_, err = fixture.service.RenameProject(ctx, fixture.principal, RenameProjectInput{ProjectID: fixture.project.ID.String(), Name: "Customer Support", IdempotencyKey: uuid.NewString(), Confirmed: true})
	require.NoError(t, err)

	require.NotEmpty(t, *acquired, "both writes are charged")
	for i, count := range *acquired {
		require.Zero(t, count, "charge %d ran with %d connections checked out", i, count)
	}
}

// Nothing between these tools and an external MCP client sanitizes an error:
// an unclassified one reaches the caller as its text. Every path through
// create, rename and the preview must therefore end in a readable refusal,
// with the underlying cause kept in the server log instead.
func TestProjectLifecycleNeverReturnsDatabaseErrorText(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_project_lifecycle_db_failure")
	broken, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_project_lifecycle_db_failure_closed")
	require.NoError(t, err)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	// Authorization still answers from the live database, so each call gets
	// past it and fails on the closed pool, which is what an outage looks
	// like from here.
	service, err := NewProjectLifecycleService(logger, broken, projects.NewCore(logger, audit.NewLogger(), nil, false),
		fixture.engine, NewLiveOrgAdminAuthorizer(fixture.conn, fixture.engine), testOperationBudget())
	require.NoError(t, err)
	broken.Close()

	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "project-lifecycle-db-failure", Version: "0.0.1"}, nil))
	registerProjectLifecycleTools(registrar, service)
	descriptors := map[string]Descriptor{}
	for _, descriptor := range registrar.Descriptors() {
		descriptors[descriptor.Name] = descriptor
	}
	ctx = ContextWithPrincipal(ctx, fixture.principal)
	invoke := func(name string, arguments map[string]any) string {
		t.Helper()
		encoded, err := json.Marshal(arguments)
		require.NoError(t, err)
		_, err = descriptors[name].Invoke(ctx, encoded)
		require.Error(t, err, name)
		return err.Error()
	}

	created := invoke(createProjectToolName, map[string]any{"name": "Support Team", "idempotency_key": uuid.NewString(), "confirmed": true})
	renamed := invoke(renameProjectToolName, map[string]any{"project_id": fixture.project.ID.String(), "name": "Renamed", "idempotency_key": uuid.NewString(), "confirmed": true})
	previewed := invoke(createProjectToolName, map[string]any{"name": "Support Team", "idempotency_key": uuid.NewString(), "confirmed": false})
	require.Contains(t, created, unavailableCode)
	require.Contains(t, renamed, unavailableCode)
	require.Contains(t, previewed, "confirmation_required", "the preview needs no database and still answers")
	for _, text := range []string{created, renamed, previewed} {
		for _, leaked := range []string{"pool", "closed", "sql", "pgx", "postgres", "relation", "constraint"} {
			require.NotContains(t, strings.ToLower(text), leaked, "the refusal must not carry database error text: %s", text)
		}
	}
	require.Contains(t, logs.String(), "closed pool", "the server log keeps the underlying cause")
}

// The projects table caps the slug at 40 characters. A name at the limit
// creates; one past it is a readable refusal naming the limit, never a
// constraint violation surfacing as an unavailable error.
func TestCreateProjectEnforcesTheLengthLimitReadably(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project_length")

	atLimit := strings.Repeat("a", projects.ProjectSlugMaxLength)
	output, err := fixture.create(ctx, atLimit, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, atLimit, output.Project.Slug)
	before := len(fixture.organizationProjects(t, ctx))

	_, err = fixture.create(ctx, strings.Repeat("b", projects.ProjectSlugMaxLength+1), uuid.NewString())
	require.Contains(t, requireProjectLifecycleRefusal(t, err, "invalid_request"), "40")
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

	// A preview is free: it must work with the write allowance spent.
	fixture.service.changes = OperationBudget{Connection: denyOperationLimiter{}, Organization: denyOperationLimiter{}}
	projectCreates := auditCount(t, ctx, fixture.conn, audit.ActionProjectCreate)

	_, err := fixture.service.CreateProject(ctx, fixture.principal, CreateProjectInput{Name: "Support Team", IdempotencyKey: uuid.NewString(), Confirmed: false})
	var preview *ProjectCreatePreviewError
	require.ErrorAs(t, err, &preview)
	require.Equal(t, "Support Team", preview.Name)
	require.Equal(t, "support-team", preview.Slug)
	require.Len(t, fixture.organizationProjects(t, ctx), before)
	require.Equal(t, projectCreates, auditCount(t, ctx, fixture.conn, audit.ActionProjectCreate))

	// Over the tool surface the slug is a field of the refusal, not only prose.
	result, ok := projectLifecycleToolResult(err)
	require.True(t, ok)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var payload projectLifecycleRefusal
	require.NoError(t, json.Unmarshal([]byte(text.Text), &payload))
	require.Equal(t, "confirmation_required", payload.Code)
	require.Equal(t, "support-team", payload.Slug)
	require.Equal(t, "Support Team", payload.Name)
	require.Contains(t, payload.Message, `"support-team"`)
}

// The skills pass one idempotency key on both the preview and the confirmed
// call. The preview must record nothing under it, or the confirmed call would
// replay a create that never happened instead of creating the project.
func TestCreateProjectPreviewAndConfirmShareOneKey(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project_shared_key")
	before := len(fixture.organizationProjects(t, ctx))
	key := uuid.NewString()

	_, err := fixture.service.CreateProject(ctx, fixture.principal, CreateProjectInput{Name: "Support Team", IdempotencyKey: key, Confirmed: false})
	var preview *ProjectCreatePreviewError
	require.ErrorAs(t, err, &preview)
	_, err = platformrepo.New(fixture.conn).GetPlatformMCPProjectCreationReceipt(ctx, platformrepo.GetPlatformMCPProjectCreationReceiptParams{
		OrganizationID: fixture.principal.OrganizationID, UserID: conv.ToPGText(fixture.principal.UserID),
		Operation: operationCreateProject, IdempotencyKey: key,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the preview stores nothing under the key")
	require.Len(t, fixture.organizationProjects(t, ctx), before)

	created, err := fixture.create(ctx, "Support Team", key)
	require.NoError(t, err)
	require.False(t, created.Receipt.Replayed, "the confirmed call creates rather than replaying the preview")
	require.Equal(t, preview.Slug, created.Project.Slug)
	require.Len(t, fixture.organizationProjects(t, ctx), before+1, "exactly one project")
}

// The preview and the confirmed call take the same key, so a key the confirmed
// call would refuse must be refused by the preview too, in the same words;
// otherwise a user confirms a create that can never go through.
func TestCreateProjectPreviewRefusesAnInvalidKeyLikeTheConfirmedCall(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project_preview_key")
	before := len(fixture.organizationProjects(t, ctx))

	for name, key := range map[string]string{"empty": "", "too long": strings.Repeat("k", maxIdempotencyKeyLength+1)} {
		_, previewErr := fixture.service.CreateProject(ctx, fixture.principal, CreateProjectInput{Name: "Support Team", IdempotencyKey: key, Confirmed: false})
		_, confirmErr := fixture.service.CreateProject(ctx, fixture.principal, CreateProjectInput{Name: "Support Team", IdempotencyKey: key, Confirmed: true})
		previewMessage := requireProjectLifecycleRefusal(t, previewErr, "invalid_request")
		require.Equal(t, requireProjectLifecycleRefusal(t, confirmErr, "invalid_request"), previewMessage, name)
		require.Contains(t, previewMessage, "idempotency key", name)
	}
	require.Len(t, fixture.organizationProjects(t, ctx), before)
}

// The preview is the only place an agent learns the slug, so it must be
// exactly the slug the confirmed call creates, including for inputs a
// hand-written rule gets wrong: non-ASCII letters are dropped, runs of
// spaces and hyphens collapse, and a hyphen left at the end is trimmed.
func TestCreateProjectPreviewSlugIsTheSlugCreated(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_create_project_preview")

	for _, name := range []string{
		"Café Team",
		"Support  --  Team",
		strings.Repeat("a", 38) + " -",
	} {
		_, err := fixture.service.CreateProject(ctx, fixture.principal, CreateProjectInput{Name: name, IdempotencyKey: uuid.NewString(), Confirmed: false})
		var preview *ProjectCreatePreviewError
		require.ErrorAs(t, err, &preview, "%q", name)

		created, err := fixture.create(ctx, name, uuid.NewString())
		require.NoError(t, err, "%q", name)
		require.Equal(t, created.Project.Slug, preview.Slug, "%q: the preview must show the slug that was created", name)
	}
}

func TestProjectLifecycleRefusesANonAdministrator(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_project_lifecycle_member")
	member := fixture.principal
	member.UserID = "user_" + uuid.NewString()
	seedPlatformMCPAuthorizationMember(t, ctx, fixture.conn, member.OrganizationID, member.UserID, authz.SystemRoleMember)
	before := len(fixture.organizationProjects(t, ctx))

	// Creating a project stays an organization administrator's action, as in
	// the dashboard, even for a member holding project write grants.
	_, err := fixture.service.CreateProject(ctx, member, CreateProjectInput{Name: "Support Team", IdempotencyKey: uuid.NewString(), Confirmed: true})
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, string(authz.ScopeOrgAdmin), denied.RequiredScope)
	require.Len(t, fixture.organizationProjects(t, ctx), before)

}

// memberContext is a context for a live member who is not an organization
// administrator, holding exactly the given grants.
func (f projectLifecycleFixture) memberContext(t *testing.T, ctx context.Context, grants ...authz.Grant) (context.Context, Principal) {
	t.Helper()
	member := f.principal
	member.UserID = "user_" + uuid.NewString()
	seedPlatformMCPAuthorizationMember(t, ctx, f.conn, member.OrganizationID, member.UserID, authz.SystemRoleMember)
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: member.OrganizationID, UserID: member.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, member.UserID))
	return authz.GrantsToContext(ctx, grants), member
}

// Rename authorizes exactly as the dashboard's project update does: write
// access to that project is enough, with no organization-admin requirement.
func TestRenameProjectNeedsOnlyProjectWrite(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_rename_project_member")
	memberCtx, member := fixture.memberContext(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, fixture.project.ID.String()))

	output, err := fixture.service.RenameProject(memberCtx, member, RenameProjectInput{ProjectID: fixture.project.ID.String(), Name: "Renamed By Member", IdempotencyKey: uuid.NewString(), Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, "renamed", output.Outcome)
	row, err := projectsrepo.New(fixture.conn).GetProjectByID(ctx, fixture.project.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed By Member", row.Name)
	entry, err := audittest.LatestAuditLogByAction(ctx, fixture.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	require.Equal(t, member.UserID, entry.ActorID)
}

// Without write access the refusal is identical for a real project and an
// invented one, so the tool cannot be used to probe which project ids exist.
func TestRenameProjectWithoutProjectWriteIsNoOracle(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_rename_project_no_write")
	memberCtx, member := fixture.memberContext(t, ctx, authz.NewGrant(authz.ScopeProjectRead, fixture.project.ID.String()))

	refuse := func(projectID string) *ExternalAuthorizationError {
		t.Helper()
		_, err := fixture.service.RenameProject(memberCtx, member, RenameProjectInput{ProjectID: projectID, Name: "Renamed", IdempotencyKey: uuid.NewString(), Confirmed: true})
		var denied *ExternalAuthorizationError
		require.ErrorAs(t, err, &denied, projectID)
		return denied
	}
	existing, invented := refuse(fixture.project.ID.String()), refuse(uuid.NewString())
	require.Equal(t, string(authz.ScopeProjectWrite), existing.RequiredScope)
	require.Equal(t, existing.RequiredScope, invented.RequiredScope)
	require.Equal(t, existing.Error(), invented.Error())

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
	require.Contains(t, requireProjectLifecycleRefusal(t, err, "confirmation_required"), "Confirm")

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
		require.Contains(t, descriptor.Description, "confirmed: true", "%s", name)
	}
	// Each tool is offered to exactly the people the dashboard lets do it:
	// creating a project needs organization admin, renaming one needs only
	// write access to it, which the handler checks on the exact project.
	require.Equal(t, ExternalAuthorizationOrgAdmin, unavailable[createProjectToolName].Meta.Authorization)
	require.Equal(t, ExternalAuthorizationMember, unavailable[renameProjectToolName].Meta.Authorization)
	require.Equal(t, []authz.Scope{authz.ScopeProjectWrite}, unavailable[renameProjectToolName].Meta.DiscoveryScopes)
	projectWriter := []authz.Grant{authz.NewGrant(authz.ScopeProjectWrite, uuid.NewString())}
	projectReader := []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, uuid.NewString())}
	require.True(t, externalToolDiscoverable(projectWriter, true, fixture.principal, unavailable[renameProjectToolName].Meta),
		"a member with write access to a project is offered rename_project")
	require.False(t, externalToolDiscoverable(projectReader, true, fixture.principal, unavailable[renameProjectToolName].Meta),
		"read access alone does not offer rename_project")
	require.False(t, externalToolDiscoverable(projectWriter, true, fixture.principal, unavailable[createProjectToolName].Meta),
		"project write access does not offer create_project, which needs organization admin")
	require.Equal(t, ProjectScopeNone, unavailable[createProjectToolName].Meta.ProjectScope, "there is no project until create_project makes one")
	require.Equal(t, ProjectScopeExplicit, unavailable[renameProjectToolName].Meta.ProjectScope)
	require.Contains(t, unavailable[createProjectToolName].Description, "Call first without confirmed: true",
		"the slug is derived and permanent, so the description must send the agent to the preview for it")
	require.Contains(t, unavailable[createProjectToolName].Description, "Do not work the slug out yourself")
	require.NotContains(t, unavailable[createProjectToolName].Description, "punctuation dropped",
		"a prose derivation drifts from the code; the preview is the only source of the slug")
	require.Contains(t, unavailable[renameProjectToolName].Description, "Needs write access to that exact project",
		"the description must state the permission a rename needs")
	require.NotContains(t, unavailable[renameProjectToolName].Description, "administrator",
		"a rename needs no organization-admin access, as in the dashboard")

	refusal := invokeUnavailable(t, unavailable[createProjectToolName], map[string]any{
		"name": "Support Team", "idempotency_key": uuid.NewString(), "confirmed": true,
	})
	require.Contains(t, refusal, "Creating or renaming projects is not available")
}
