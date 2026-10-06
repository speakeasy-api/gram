package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestPluginAudienceDeliversExistingUseThroughMCP(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_audience_delivery")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Role delivery", "role-delivery")
	servers, err := platformrepo.New(conn).ListPlatformMCPServers(ctx, platformrepo.ListPlatformMCPServersParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID, LimitValue: 10})
	require.NoError(t, err)
	require.NotEmpty(t, servers)
	serverID := servers[0].ID.String()
	now := conv.ToPGTimestamptz(time.Now())
	role, err := accessrepo.New(conn).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{OrganizationID: principal.OrganizationID, WorkosSlug: "audience-delivery", WorkosName: "Audience delivery", WorkosCreatedAt: now, WorkosUpdatedAt: now})
	require.NoError(t, err)
	roleURN := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+role.ID.String())
	selector, err := authz.NewSelector(authz.ScopeMCPConnect, serverID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: principal.OrganizationID, PrincipalUrn: roleURN, Scope: string(authz.ScopeMCPConnect), Selectors: selector,
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{ProjectID: project.ID, InstallationID: 1, RepoOwner: "test-owner", RepoName: "test-marketplace", PublishedMcpFingerprints: []byte(`{}`), PublishedHooksVersion: pgtype.Text{}})
	require.NoError(t, err)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPPluginAssignmentMutations, principal.OrganizationID, true)
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	delivery := plugindelivery.NewService(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, &sessions.Manager{}, nil, engine, audit.NewLogger(), nil, "test", "https://example.test", nil, nil).WithPublicationRequests(true)
	service := testPluginTargets(conn).WithAuthorization(engine).WithServerRemoval(delivery).
		WithAssignmentMutations(flags, NewPostgresOrganizationSlugResolver(conn), audit.NewLogger(), testOperationBudget()).
		WithPublicationRequests(plugindelivery.PublicationRequests{Enabled: true})
	accessReads := NewAccessReadService(testenv.NewLogger(t), conn, allowBudget(), "audience-delivery-access-key")
	session := newPluginRemovalSession(t, service, principal, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)}, accessReads)
	readArgs := map[string]any{"project_id": project.ID.String(), "plugin": plugin.ID.String()}
	before := callSkillsTool[GetPluginOutput](t, ctx, session, "get_plugin", readArgs)
	require.Empty(t, before.Servers)
	choices := callSkillsTool[ListPluginAssignmentsOutput](t, ctx, session, "list_plugin_assignments", map[string]any{"project_id": project.ID.String()})
	var reference string
	for _, choice := range choices.Assignments {
		if choice.DisplayName == role.WorkosName {
			reference = choice.Reference
		}
	}
	require.NotEmpty(t, reference)
	args := map[string]any{"project_id": project.ID.String(), "plugin": plugin.ID.String(), "assignment_references": []string{reference}, "expected_assignment_version": before.AssignmentVersion, "idempotency_key": "assign-existing-use", "confirmed": true}
	type audienceResult struct {
		SetPluginAssignmentsOutput
		PublicationRequest string `json:"publication_request"`
	}
	assigned := callSkillsTool[audienceResult](t, ctx, session, "set_plugin_assignments", args)
	after := callSkillsTool[GetPluginOutput](t, ctx, session, "get_plugin", readArgs)
	require.Len(t, after.Servers, 1)
	require.Equal(t, serverID, after.Servers[0].TargetID)
	require.Equal(t, "enqueued", assigned.PublicationRequest)
	require.False(t, assigned.Receipt.Replayed)

	accessArgs := map[string]any{"project_id": project.ID.String(), "mcp_id": serverID}
	accessBefore := callSkillsTool[GetMCPAccessOutput](t, ctx, session, "get_mcp_access", accessArgs)
	require.Len(t, accessBefore.Roles, 1)
	require.Equal(t, role.WorkosName, accessBefore.Roles[0].Name)
	require.True(t, accessBefore.Roles[0].CanEnterServer)
	require.NotEmpty(t, accessBefore.Roles[0].Version)

	removed := callSkillsTool[RemovePluginServerOutput](t, ctx, session, "remove_plugin_server", map[string]any{"project_id": project.ID.String(), "plugin_id": plugin.ID.String(), "membership_id": after.Servers[0].MembershipID, "confirmed": true})
	require.True(t, removed.Removed)
	accessAfter := callSkillsTool[GetMCPAccessOutput](t, ctx, session, "get_mcp_access", accessArgs)
	require.Equal(t, project.ID.String(), accessAfter.ProjectID)
	require.Equal(t, serverID, accessAfter.MCP.ID)
	require.Len(t, accessAfter.Roles, 1)
	require.Equal(t, role.WorkosName, accessAfter.Roles[0].Name)
	require.True(t, accessAfter.Roles[0].CanEnterServer)
	// The role version covers its exact grants, not just effective server access.
	require.Equal(t, accessBefore.Roles[0].Version, accessAfter.Roles[0].Version)
	replay := callSkillsTool[audienceResult](t, ctx, session, "set_plugin_assignments", args)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, assigned.PublicationRequest, replay.PublicationRequest)
	// A new request with the same audience is also a no-op, not a reconciliation.
	args["idempotency_key"] = "unchanged-audience"
	args["expected_assignment_version"] = assigned.AssignmentVersion
	unchanged := callSkillsTool[audienceResult](t, ctx, session, "set_plugin_assignments", args)
	require.False(t, unchanged.Receipt.Replayed)
	require.Empty(t, unchanged.PublicationRequest)
	current := callSkillsTool[GetPluginOutput](t, ctx, session, "get_plugin", readArgs)
	require.Empty(t, current.Servers)
	require.Equal(t, assigned.AssignmentVersion, current.AssignmentVersion)
}
