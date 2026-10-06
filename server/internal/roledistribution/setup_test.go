package roledistribution_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

// Extend the existing setup pipeline seam: fixture repositories arrange grants
// predating setup, and the real handler runs the setup transaction.
func TestRoleDistributionSetup_PopulatesExistingGrants(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	org := f.event.GetOrganizationId()
	backing, err := toolsetsrepo.New(f.db).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: org, ProjectID: f.project, Name: "Existing server", Slug: "existing-server", McpEnabled: true,
		McpSlug: pgtype.Text{String: "existing-server", Valid: true},
	})
	require.NoError(t, err)
	serverID := backing.ID
	principal, err := urn.ParsePrincipal(f.roleURN)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, serverID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.db).InsertPrincipalGrantIfAbsent(ctx, accessrepo.InsertPrincipalGrantIfAbsentParams{OrganizationID: org, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	manual, err := pluginsrepo.New(f.db).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: org, ProjectID: f.project, Name: "Manual distribution", Slug: "manual-distribution"})
	require.NoError(t, err)
	_, err = pluginsrepo.New(f.db).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{OrganizationID: org, PluginID: manual.ID, PrincipalUrn: f.roleURN})
	require.NoError(t, err)
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "existing-grants"}))
	for _, action := range []audit.Action{audit.ActionPluginCreate, audit.ActionPluginAssignmentsSet, audit.ActionPluginServerAdd} {
		record, err := audittest.LatestAuditLogByAction(ctx, f.db, action)
		require.NoError(t, err)
		require.Equal(t, "Gram", record.ActorDisplay, "actor label for %s", action)
		require.Equal(t, "system", record.ActorType)
		require.Equal(t, "automatic-role-distribution", record.ActorID)
	}
	automatic, err := testrepo.New(f.db).PipelineEngineeringPlugin(ctx, f.project)
	require.NoError(t, err)
	for _, pluginID := range []uuid.UUID{automatic, manual.ID} {
		servers, err := pluginsrepo.New(f.db).ListPluginServers(ctx, pluginID)
		require.NoError(t, err)
		require.Len(t, servers, 1, "setup populates every matching plugin")
		require.Equal(t, serverID, servers[0].ToolsetID.UUID)
	}
	// Redelivery leaves stable membership identities intact.
	before, err := pluginsrepo.New(f.db).ListPluginServers(ctx, manual.ID)
	require.NoError(t, err)
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "replay"}))
	after, err := pluginsrepo.New(f.db).ListPluginServers(ctx, manual.ID)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Len(t, after, 1)
	require.Equal(t, before[0].ID, after[0].ID, "replay preserves the membership identity")
	require.True(t, after[0].ToolsetID.Valid)
	require.Equal(t, serverID, after[0].ToolsetID.UUID, "replay preserves the granted server")
	_, err = pluginsrepo.New(f.db).RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{PluginID: manual.ID, ID: before[0].ID})
	require.NoError(t, err)
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "after-removal"}))
	after, err = pluginsrepo.New(f.db).ListPluginServers(ctx, manual.ID)
	require.NoError(t, err)
	require.Empty(t, after, "setup replay preserves administrator content removals")
	assignments, err := pluginsrepo.New(f.db).ListPluginAssignments(ctx, pluginsrepo.ListPluginAssignmentsParams{PluginID: manual.ID, OrganizationID: org, ProjectID: f.project})
	require.NoError(t, err)
	require.Len(t, assignments, 1, "content removal does not remove the role audience")
	require.Equal(t, f.roleURN, assignments[0].PrincipalUrn)

}

func TestRoleDistributionSetup_PopulatesOtherProject(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	org := f.event.GetOrganizationId()
	projectID := uuid.New()
	_, err := testrepo.New(f.db).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: projectID, OrganizationID: org, Name: "Other project", Slug: "other-project"})
	require.NoError(t, err)
	backing, err := toolsetsrepo.New(f.db).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{OrganizationID: org, ProjectID: projectID, Name: "Other server", Slug: "other-server", McpEnabled: true, McpSlug: pgtype.Text{String: "other-server", Valid: true}})
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(f.roleURN)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, backing.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.db).InsertPrincipalGrantIfAbsent(ctx, accessrepo.InsertPrincipalGrantIfAbsentParams{OrganizationID: org, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	plugin, err := pluginsrepo.New(f.db).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: org, ProjectID: projectID, Name: "Other distribution", Slug: "other-distribution"})
	require.NoError(t, err)
	_, err = pluginsrepo.New(f.db).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{OrganizationID: org, PluginID: plugin.ID, PrincipalUrn: f.roleURN})
	require.NoError(t, err)
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "other-project"}))
	contents, err := pluginsrepo.New(f.db).ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Len(t, contents, 1)
	require.Equal(t, backing.ID, contents[0].ToolsetID.UUID)
	automatic, err := testrepo.New(f.db).PipelineEngineeringPlugin(ctx, f.project)
	require.NoError(t, err)
	contents, err = pluginsrepo.New(f.db).ListPluginServers(ctx, automatic)
	require.NoError(t, err)
	require.Empty(t, contents, "cross-project grant must not add content to the setup project")
}
