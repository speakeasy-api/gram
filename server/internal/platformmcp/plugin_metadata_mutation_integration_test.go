package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type pluginMetadataFixture struct {
	conn      *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	service   *PluginsService
}

// seedPluginMetadataFixture composes the plugin service with the same metadata
// core the dashboard uses. The returned context carries org:admin, which
// satisfies plugin write the way it does for the dashboard.
func seedPluginMetadataFixture(t *testing.T, name string) (context.Context, pluginMetadataFixture) {
	t.Helper()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := testPluginTargets(conn).
		WithAuthorization(engine).
		WithMetadataMutations(plugindelivery.NewPluginMetadataCore(audit.NewLogger(), plugindelivery.PublicationRequests{Enabled: false}), nil, testOperationBudget())
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)})
	return ctx, pluginMetadataFixture{conn: conn, principal: principal, project: project, service: service}
}

func (f pluginMetadataFixture) pluginCount(t *testing.T, ctx context.Context, projectID uuid.UUID) int {
	t.Helper()
	page, err := f.service.ListPlugins(ctx, f.principal, ListPluginsInput{ProjectID: projectID.String()})
	require.NoError(t, err)
	return len(page.Plugins)
}

func (f pluginMetadataFixture) descriptor(t *testing.T, name string) Descriptor {
	t.Helper()
	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "plugin-metadata", Version: "0.0.1"}, nil))
	registerPluginMetadataTools(registrar, f.service)
	return descriptorByName(t, registrar, name)
}

func TestCreatePluginCreatesAnEmptyPluginThatListPluginsReturns(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_create_plugin")
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginCreate)
	require.NoError(t, err)

	created, err := fixture.service.CreatePlugin(ctx, fixture.principal, CreatePluginInput{
		ProjectID: fixture.project.ID.String(), Name: "Support Tools", Description: "Tools for the support team",
		IdempotencyKey: "create-support", Confirmed: true,
	})
	require.NoError(t, err)
	require.False(t, created.Receipt.Replayed)
	require.Equal(t, "created", created.ResultCategory)
	require.Equal(t, "Support Tools", created.Plugin.Name)
	require.Equal(t, "support-tools", created.Plugin.Slug, "an omitted slug is derived from the name")
	require.Equal(t, "Tools for the support team", created.Plugin.Description)
	require.Zero(t, created.Plugin.ServerCount)
	require.Zero(t, created.Plugin.SkillCount)
	// The seeded project is the organization's only, and therefore default,
	// project, where the dashboard delivers a new plugin to everyone.
	require.True(t, created.DeliveredToEveryone)
	require.True(t, created.Plugin.Assignments.AllMembers)

	page, err := fixture.service.ListPlugins(ctx, fixture.principal, ListPluginsInput{ProjectID: fixture.project.ID.String()})
	require.NoError(t, err)
	require.Len(t, page.Plugins, 1)
	require.Equal(t, created.Plugin.ID, page.Plugins[0].ID)
	require.Equal(t, "Support Tools", page.Plugins[0].Name)

	afterAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginCreate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit+1, afterAudit)

	// Outside the default project a new plugin reaches no one until assigned.
	other, err := projectsrepo.New(fixture.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Second project", Slug: "second-" + uuid.NewString()[:8], OrganizationID: fixture.principal.OrganizationID,
	})
	require.NoError(t, err)
	scoped, err := fixture.service.CreatePlugin(ctx, fixture.principal, CreatePluginInput{
		ProjectID: other.ID.String(), Name: "Scoped", Slug: "scoped-tools", IdempotencyKey: "create-scoped", Confirmed: true,
	})
	require.NoError(t, err)
	require.False(t, scoped.DeliveredToEveryone)
	require.Equal(t, "scoped-tools", scoped.Plugin.Slug)
	require.Equal(t, PluginAssignmentSummary{}, scoped.Plugin.Assignments)
}

func TestCreatePluginReplaysOneIdempotencyKeyWithoutASecondPlugin(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_create_plugin_replay")
	input := CreatePluginInput{ProjectID: fixture.project.ID.String(), Name: "Replayed", IdempotencyKey: "create-once", Confirmed: true}
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginCreate)
	require.NoError(t, err)

	first, err := fixture.service.CreatePlugin(ctx, fixture.principal, input)
	require.NoError(t, err)
	retry, err := fixture.service.CreatePlugin(ctx, fixture.principal, input)
	require.NoError(t, err)

	require.True(t, retry.Receipt.Replayed)
	require.Equal(t, first.Receipt.ID, retry.Receipt.ID)
	require.Equal(t, first.Plugin, retry.Plugin)
	require.Equal(t, 1, fixture.pluginCount(t, ctx, fixture.project.ID), "a retry must not create a second plugin")
	afterAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginCreate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit+1, afterAudit, "a replay must not write a second audit entry")

	// The same key with different input is a conflict, not a second create.
	changed := input
	changed.Name = "Something else"
	_, err = fixture.service.CreatePlugin(ctx, fixture.principal, changed)
	var refusal *PluginMetadataMutationError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "conflict", refusal.Code)
	require.Equal(t, 1, fixture.pluginCount(t, ctx, fixture.project.ID))
}

func TestCreatePluginRefusesASlugAnotherPluginHolds(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_create_plugin_conflict")
	_, err := fixture.service.CreatePlugin(ctx, fixture.principal, CreatePluginInput{
		ProjectID: fixture.project.ID.String(), Name: "Support Tools", IdempotencyKey: "first", Confirmed: true,
	})
	require.NoError(t, err)

	_, err = fixture.service.CreatePlugin(ctx, fixture.principal, CreatePluginInput{
		ProjectID: fixture.project.ID.String(), Name: "Another name", Slug: "support-tools", IdempotencyKey: "second", Confirmed: true,
	})
	var refusal *PluginMetadataMutationError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "conflict", refusal.Code)
	require.ErrorIs(t, err, plugindelivery.ErrPluginSlugConflict)
	require.Equal(t, 1, fixture.pluginCount(t, ctx, fixture.project.ID))

	// The refused attempt left no receipt behind, so the same key can be used
	// again once the caller picks a free slug.
	_, err = platformrepo.New(fixture.conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: fixture.principal.OrganizationID, ProjectID: fixture.project.ID, Operation: operationCreatePlugin,
		IdempotencyKey: "second", UserID: conv.ToPGText(fixture.principal.UserID), SubjectUrn: userSubjectURN(fixture.principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// A supplied slug that is not already normalized is refused with the
	// dashboard's own wording.
	_, err = fixture.service.CreatePlugin(ctx, fixture.principal, CreatePluginInput{
		ProjectID: fixture.project.ID.String(), Name: "Bad slug", Slug: "Bad Slug", IdempotencyKey: "third", Confirmed: true,
	})
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Contains(t, refusal.Message, plugindelivery.ErrPluginSlugInvalid.Error())
}

func TestCreatePluginRefusesAnUnconfirmedRequest(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_create_plugin_unconfirmed")
	payload := invokePluginMetadataRefusal(t, fixture.descriptor(t, operationCreatePlugin), map[string]any{
		"project_id": fixture.project.ID.String(), "name": "Unconfirmed", "slug": "unconfirmed",
		"description": "", "idempotency_key": uuid.NewString(), "confirmed": false,
	}, ContextWithPrincipal(ctx, fixture.principal))
	var refusal pluginRefusalResult
	require.NoError(t, json.Unmarshal([]byte(payload), &refusal))
	require.Equal(t, "confirmation_required", refusal.Code)
	require.Zero(t, fixture.pluginCount(t, ctx, fixture.project.ID))
}

// A caller without plugin write gets the authorization refusal, and gets the
// same one for a project that does not exist, so the tool cannot be used to
// tell real project ids from invented ones.
func TestCreatePluginRefusesACallerWithoutPluginWrite(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_create_plugin_forbidden")
	reader := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, fixture.project.ID.String())})
	reader = ContextWithPrincipal(reader, fixture.principal)

	for _, projectID := range []string{fixture.project.ID.String(), uuid.NewString()} {
		payload := invokePluginMetadataRefusal(t, fixture.descriptor(t, operationCreatePlugin), map[string]any{
			"project_id": projectID, "name": "Forbidden", "slug": "forbidden",
			"description": "", "idempotency_key": uuid.NewString(), "confirmed": true,
		}, reader)
		var refusal externalAuthorizationRefusal
		require.NoError(t, json.Unmarshal([]byte(payload), &refusal))
		require.Equal(t, "permission_denied", refusal.Code)
		require.Equal(t, string(authz.ScopePluginWrite), refusal.RequiredScope)
	}
	require.Zero(t, fixture.pluginCount(t, ctx, fixture.project.ID))
}

func TestRenamePluginChangesTheNameAndNothingElse(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_rename_plugin")
	plugin, err := pluginsrepo.New(fixture.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.principal.OrganizationID, ProjectID: fixture.project.ID,
		Name: "Old Name", Slug: "stable-install-name", Description: pgtype.Text{String: "Kept as is", Valid: true},
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		PluginID: plugin.ID, OrganizationID: fixture.principal.OrganizationID, PrincipalUrn: urn.PrincipalWildcard,
	})
	require.NoError(t, err)
	before, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)

	input := RenamePluginInput{ProjectID: fixture.project.ID.String(), Plugin: "Old Name", Name: "New Name", IdempotencyKey: "rename", Confirmed: true}
	renamed, err := fixture.service.RenamePlugin(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.Equal(t, "renamed", renamed.ResultCategory)
	require.Equal(t, "Old Name", renamed.PreviousName)
	require.Equal(t, "New Name", renamed.Plugin.Name)

	after, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, "New Name", after.Plugin.Name)
	expected := before.Plugin
	expected.Name = "New Name"
	require.Equal(t, expected, after.Plugin, "only the name changes: slug, description, contents, assignments, and publication stay put")
	require.Equal(t, before.AssignmentVersion, after.AssignmentVersion)
	require.Equal(t, before.MembershipVersion, after.MembershipVersion)

	afterAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit+1, afterAudit)

	// The retry named the plugin by its old name, which no longer resolves; the
	// receipt replays it anyway rather than reporting not found.
	retry, err := fixture.service.RenamePlugin(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.True(t, retry.Receipt.Replayed)
	require.Equal(t, renamed.Plugin, retry.Plugin)
	replayAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	require.Equal(t, afterAudit, replayAudit)
}

func TestPluginMetadataUnavailableRegistrationMatchesLiveManifest(t *testing.T) {
	t.Parallel()
	_, fixture := seedPluginMetadataFixture(t, "platform_mcp_plugin_metadata_manifest")
	require.True(t, fixture.service.metadataMutationValid(), "the live side must be a composed service")

	describe := func(service *PluginsService) map[string]Descriptor {
		registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "plugin-metadata-manifest", Version: "0.0.1"}, nil))
		registerPluginMetadataTools(registrar, service)
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

	payload := invokePluginMetadataRefusal(t, unavailable[operationCreatePlugin], map[string]any{
		"project_id": fixture.project.ID.String(), "name": "Unavailable", "slug": "unavailable",
		"description": "", "idempotency_key": uuid.NewString(), "confirmed": true,
	}, t.Context())
	require.Contains(t, payload, pluginMetadataFeature)
}

// invokePluginMetadataRefusal calls a tool the way the transport does, with
// every required field present so a refusal is the tool's own and not a
// schema rejection, and returns the refusal payload.
func invokePluginMetadataRefusal(t *testing.T, descriptor Descriptor, arguments map[string]any, ctx context.Context) string {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	_, err = descriptor.Invoke(ctx, encoded)
	require.Error(t, err)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	return refusal.Payload
}
