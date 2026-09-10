package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestShadowDistributionReadReportsRepairForPluginAndTarget(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_shadow_distribution_read")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	inventory, err := platformrepo.New(conn).ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{OrganizationID: principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, LimitValue: 10})
	require.NoError(t, err)
	require.NotEmpty(t, inventory)
	target := inventory[0]
	remote, err := remoterepo.New(conn).GetServerByID(ctx, remoterepo.GetServerByIDParams{ID: target.RemoteMcpServerID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	remote, err = remoterepo.New(conn).UpdateServer(ctx, remoterepo.UpdateServerParams{Name: pgtype.Text{}, Slug: pgtype.Text{}, TransportType: remote.TransportType, Url: "https://cohort.example.test/", ID: remote.ID, ProjectID: project.ID})
	require.NoError(t, err)
	canonicalTarget, ok := shadowmcp.CanonicalizeInventoryURL(remote.Url)
	require.True(t, ok)
	require.NotEqual(t, remote.Url, canonicalTarget.CanonicalURL)

	registration, err := platformrepo.New(conn).CreatePlatformMCPCatalogRegistration(ctx, platformrepo.CreatePlatformMCPCatalogRegistrationParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID, SourceKind: "remote", CatalogProvider: "direct-remote-url-v1", CatalogReference: remote.Url, Status: "registered", ConnectionID: uuid.NullUUID{}, ConnectionGeneration: uuid.NullUUID{}, UserID: pgtype.Text{}, ActingSurface: pgtype.Text{}})
	require.NoError(t, err)
	_, err = platformrepo.New(conn).UpdatePlatformMCPCatalogRegistrationComponents(ctx, platformrepo.UpdatePlatformMCPCatalogRegistrationComponentsParams{ID: registration.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID, Status: "registered", RemoteMcpServerID: uuid.NullUUID{UUID: target.RemoteMcpServerID.UUID, Valid: true}, McpServerID: uuid.NullUUID{UUID: target.McpServerID, Valid: true}})
	require.NoError(t, err)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Repair", "repair")
	_, err = pluginsrepo.New(conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{PluginID: plugin.ID, ToolsetID: uuid.NullUUID{}, McpServerID: uuid.NullUUID{UUID: target.McpServerID, Valid: true}, DisplayName: "Repair target", Policy: "required", SortOrder: 0})
	require.NoError(t, err)
	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: "role:developers"})
	require.NoError(t, err)
	_, err = riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{ID: uuid.New(), ProjectID: project.ID, OrganizationID: principal.OrganizationID, Name: "Blocking Shadow MCP", PolicyType: "standard", Sources: []string{shadowmcp.SourceShadowMCP}, PresidioEntities: nil, AnalyzerConfig: nil, PromptInjectionRules: nil, DisabledRules: nil, CustomRuleIds: nil, MessageTypes: nil, ScopeInclude: pgtype.Text{}, ScopeExempt: pgtype.Text{}, Enabled: true, Action: "block", AudienceType: "everyone", ShadowMcpDisposition: pgtype.Text{}, AutoName: false, UserMessage: pgtype.Text{}, Prompt: pgtype.Text{}, ModelConfig: nil, Score: pgtype.Float8{}})
	require.NoError(t, err)

	flags := newFeatureRollout(principal.OrganizationID)
	service := NewShadowDistributionReadService(testenv.NewLogger(t), conn, admission.NewGuard(flags, nil), NewPostgresOrganizationSlugResolver(conn))
	pluginResult := service.ForPlugin(ctx, principal.OrganizationID, project.ID, plugin.ID)
	targetResult := service.ForTarget(ctx, principal.OrganizationID, project.ID, canonicalTarget.CanonicalURL)

	want := admission.MissingAudienceCounts{Everyone: 0, Roles: 1, Groups: 0, Attributes: 0, Users: 0}
	require.Equal(t, DistributionAdmissionRepairRequired, pluginResult.State)
	require.Equal(t, want, pluginResult.MissingAudienceCounts)
	require.True(t, pluginResult.Complete)
	require.Equal(t, DistributionAdmissionRepairRequired, targetResult.State)
	require.Equal(t, want, targetResult.MissingAudienceCounts)
	require.True(t, targetResult.Complete)

	request, err := approvalrepo.New(conn).UpsertApprovalRequest(ctx, approvalrepo.UpsertApprovalRequestParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID, TargetKind: "server_url", TargetRaw: canonicalTarget.CanonicalURL, TargetKey: canonicalTarget.CanonicalURL, ArtifactRef: pgtype.Text{}, VersionPinned: true, Status: "requested", RiskPolicyBypassRequestID: uuid.NullUUID{}})
	require.NoError(t, err)
	_, err = approvalrepo.New(conn).CreateApprovalDecision(ctx, approvalrepo.CreateApprovalDecisionParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID, McpApprovalRequestID: request.ID, Decision: "approved", DecidedBy: principal.UserID, Rationale: pgtype.Text{String: "approved for regression test", Valid: true}, EvidenceSnapshot: []byte(`{}`), EvidenceVersion: 1, GrantedPrincipalUrns: []string{"role:developers"}, McpResearchReportID: uuid.NullUUID{}})
	require.NoError(t, err)

	pluginResult = service.ForPlugin(ctx, principal.OrganizationID, project.ID, plugin.ID)
	targetResult = service.ForTarget(ctx, principal.OrganizationID, project.ID, canonicalTarget.CanonicalURL)
	require.Equal(t, DistributionAdmissionCovered, pluginResult.State)
	require.Equal(t, admission.MissingAudienceCounts{}, pluginResult.MissingAudienceCounts)
	require.True(t, pluginResult.Complete)
	require.Equal(t, DistributionAdmissionCovered, targetResult.State)
	require.Equal(t, admission.MissingAudienceCounts{}, targetResult.MissingAudienceCounts)
	require.True(t, targetResult.Complete)
}

func newFeatureRollout(organizationID string) *feature.InMemory {
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, organizationID, true)
	flags.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, organizationID, []byte(`{"mode":"enforce"}`))
	flags.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, organizationID, false)
	return flags
}
