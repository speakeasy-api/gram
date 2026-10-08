package platformmcp

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Arrange membership through the real audience event so audit provenance
// identifies automatic delivery.
func seedExposureMutationMemberships(t *testing.T, ctx context.Context, fixture toolExposureFixture, toolsetID uuid.UUID) (uuid.UUID, uuid.UUID, string) {
	t.Helper()
	ac := &contextvalues.AuthContext{ActiveOrganizationID: fixture.principal.OrganizationID, ProjectID: &fixture.project.ID}
	q := pluginsrepo.New(fixture.conn)
	automatic, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Automatic", Slug: "automatic"})
	require.NoError(t, err)
	manual, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Manual", Slug: "manual"})
	require.NoError(t, err)
	role, err := accessrepo.New(fixture.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "content-edit", WorkosName: "Content edit", WorkosCreatedAt: conv.ToPGTimestamptz(time.Now()), WorkosUpdatedAt: conv.ToPGTimestamptz(time.Now())})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, fixture.conn)
	_, err = authz.PatchRoleGrantsTx(ctx, tx, ac.ActiveOrganizationID, "content-edit", role.RoleUrn, []*authz.RoleGrant{{Scope: string(authz.ScopeMCPConnect), Selectors: []authz.Selector{authz.NewGrant(authz.ScopeMCPConnect, toolsetID.String()).Selector}}}, nil)
	require.NoError(t, err)
	_, err = pluginsrepo.New(tx).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: role.RoleUrn, PluginID: automatic.ID})
	require.NoError(t, err)
	changed, err := roledelivery.AudienceChanged(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, automatic.ID, nil, []string{role.RoleUrn}, nil)
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, tx.Commit(ctx))
	_, err = q.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{PluginID: manual.ID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, DisplayName: "Manual server", Policy: "optional"})
	require.NoError(t, err)
	return automatic.ID, manual.ID, role.RoleUrn
}

// Platform tools are not generated inventory, so AddTools cannot introduce one.
// Seed a historical mixed version, then make a supported generated-tool edit.
func TestChangeMCPToolsReportsAndReplaysRemovedAutomaticPlugins(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_content_cleanup")
	_, err := mcpendpointsrepo.New(fixture.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: fixture.project.ID, McpServerID: uuid.NullUUID{UUID: fixture.toolsetID, Valid: true}, Slug: "cleanup-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	automatic, manual, _ := seedExposureMutationMemberships(t, ctx, fixture, fixture.toolsetID)
	_, err = toolsetsrepo.New(fixture.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{ToolsetID: fixture.toolsetID, Version: 1, ResourceUrns: []urn.Resource{}, ToolUrns: []urn.Tool{urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs")}})
	require.NoError(t, err)
	current, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	input := ChangeMCPToolsInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.toolsetID.String(), ToolURNs: []string{fixture.tools[0]}, ExpectedVersion: current.ExposureVersion, IdempotencyKey: uuid.NewString(), Confirmed: true}
	first, err := fixture.service.AddTools(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.Equal(t, []string{automatic.String()}, first.RemovedPluginIDs)
	require.False(t, first.Receipt.Replayed)
	payload, err := json.Marshal(first)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"removed_plugin_ids":["`+automatic.String()+`"]`)
	rows, err := pluginsrepo.New(fixture.conn).ListPluginServers(ctx, automatic)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = pluginsrepo.New(fixture.conn).ListPluginServers(ctx, manual)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	replay, err := fixture.service.AddTools(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, first.Receipt.ID, replay.Receipt.ID)
	require.Equal(t, first.RemovedPluginIDs, replay.RemovedPluginIDs)
	require.Equal(t, first.Exposure, replay.Exposure)
}
