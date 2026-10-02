package mcpservers_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestUpdateMcpServer_EnableRoleDistributionRequiresApproval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, remoteID := createDisabledRemoteServer(t, ctx, ti, *ac.ProjectID, "Role distribution approval")
	serverID := uuid.MustParse(created.ID)
	flags := seedBlockedDirectRemoteDistribution(t, ctx, ti, serverID)
	flags.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, ac.ActiveOrganizationID, false)
	seedEndpointFor(t, ctx, ti.conn, *ac.ProjectID, created.ID)

	// The Default plugin has no audience, so only the role plugin needs approval.
	defaultPlugin, err := pluginsrepo.New(ti.conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID,
	})
	require.NoError(t, err)
	_, err = riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID: uuid.New(), ProjectID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID,
		Name: "Blocking Shadow MCP", PolicyType: "standard", Sources: []string{shadowmcp.SourceShadowMCP},
		PresidioEntities: nil, AnalyzerConfig: nil, PromptInjectionRules: nil, DisabledRules: nil, CustomRuleIds: nil,
		Enabled: true, Action: "block", AudienceType: "everyone", ShadowMcpDisposition: pgtype.Text{},
		AutoName: false, UserMessage: pgtype.Text{}, Prompt: pgtype.Text{}, ModelConfig: nil, Score: pgtype.Float8{},
	})
	require.NoError(t, err)
	now := conv.ToPGTimestamptz(time.Now())
	role, err := accessrepo.New(ti.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "approval-role", WorkosName: "Approval role",
		WorkosDescription: conv.ToPGTextEmpty(""), WorkosCreatedAt: now, WorkosUpdatedAt: now, WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(role.RoleUrn)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, created.ID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors,
	})
	require.NoError(t, err)
	plugin, err := pluginsrepo.New(ti.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Approval role", Slug: "approval-role", Description: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(ti.conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: ac.ActiveOrganizationID, PluginID: plugin.ID, PrincipalUrn: role.RoleUrn,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("private"),
	})
	require.ErrorIs(t, err, admission.ErrApprovalRequired)
	requireOopsCode(t, err, oops.CodeConflict)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID: serverID, ProjectID: *ac.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, "disabled", server.Visibility)
	for _, pluginID := range []uuid.UUID{defaultPlugin.ID, plugin.ID} {
		servers, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, pluginID)
		require.NoError(t, err)
		require.Empty(t, servers, "denied distribution must roll back all attachments")
	}
}
