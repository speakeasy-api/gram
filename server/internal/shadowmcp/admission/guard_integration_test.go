package admission

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"

	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestGatewayMemberAdmissionChecksEveryPluginAudience(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, remoteID := seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	ctx := t.Context()
	gateway, err := metamcprepo.New(fixture.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Name: "Gateway", Visibility: "private",
	})
	require.NoError(t, err)
	plugin, err := pluginsrepo.New(fixture.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Name: "Gateway plugin", Slug: "gateway", Description: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).AddGatewayPluginServer(ctx, pluginsrepo.AddGatewayPluginServerParams{
		PluginID: plugin.ID, ProjectID: fixture.projectID, MetaMcpServerID: uuid.NullUUID{UUID: gateway.ID, Valid: true},
		DisplayName: "Gateway", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: fixture.orgID, PluginID: plugin.ID, PrincipalUrn: "role:developers",
	})
	require.NoError(t, err)

	guard := NewGuard()
	tx := testenv.BeginTx(t, ctx, fixture.conn)
	require.ErrorIs(t, guard.CheckGatewayMemberAddition(ctx, tx, fixture.orgID, fixture.projectID, gateway.ID, serverID), ErrApprovalRequired)
	require.NoError(t, tx.Rollback(ctx))

	seedDecision(t, fixture, "https://mcp.example.test/server", "approved", []string{"role:developers"})
	tx = testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, guard.CheckGatewayMemberAddition(ctx, tx, fixture.orgID, fixture.projectID, gateway.ID, serverID))
	require.NoError(t, tx.Rollback(ctx))

	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: fixture.orgID, PluginID: plugin.ID, PrincipalUrn: "role:operators",
	})
	require.NoError(t, err)
	tx = testenv.BeginTx(t, ctx, fixture.conn)
	require.ErrorIs(t, guard.CheckGatewayMemberAddition(ctx, tx, fixture.orgID, fixture.projectID, gateway.ID, serverID), ErrApprovalRequired)
	require.NoError(t, tx.Rollback(ctx))

	_, err = metamcprepo.New(fixture.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID: fixture.projectID, MetaMcpServerID: gateway.ID, McpServerID: serverID,
	})
	require.NoError(t, err)
	tx = testenv.BeginTx(t, ctx, fixture.conn)
	require.ErrorIs(t, (*Guard)(nil).CheckGatewayAttachment(ctx, tx, fixture.orgID, fixture.projectID, plugin.ID, gateway.ID), ErrUnavailable)
	require.ErrorIs(t, guard.CheckGatewayAttachment(ctx, tx, fixture.orgID, fixture.projectID, plugin.ID, gateway.ID), ErrApprovalRequired)
	require.ErrorIs(t, guard.CheckPluginAudience(ctx, tx, fixture.orgID, fixture.projectID, plugin.ID, []string{"role:developers", "role:operators"}), ErrApprovalRequired)
	require.ErrorIs(t, guard.CheckRemoteTarget(ctx, tx, fixture.orgID, fixture.projectID, remoteID, "https://mcp.example.test/changed"), ErrApprovalRequired)
}

func TestPrivateGatewayAudienceBlocksEveryone(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	ctx := t.Context()
	gateway, err := metamcprepo.New(fixture.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Name: "Private gateway", Visibility: "private",
		NetworkAccessMode: pgtype.Text{String: "private_only", Valid: true},
	})
	require.NoError(t, err)
	plugin, err := pluginsrepo.New(fixture.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Name: "Gateway plugin", Slug: "gateway-private", Description: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).AddGatewayPluginServer(ctx, pluginsrepo.AddGatewayPluginServerParams{
		PluginID: plugin.ID, ProjectID: fixture.projectID, MetaMcpServerID: uuid.NullUUID{UUID: gateway.ID, Valid: true},
		DisplayName: "Gateway", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)
	guard := NewGuard()
	tx := testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, guard.CheckGatewayAttachment(ctx, tx, fixture.orgID, fixture.projectID, plugin.ID, gateway.ID))
	require.ErrorIs(t, guard.CheckPluginAudience(ctx, tx, fixture.orgID, fixture.projectID, plugin.ID, []string{urn.PrincipalWildcard}), ErrPrivateGatewayAudience)
	require.NoError(t, tx.Rollback(ctx))

	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: fixture.orgID, PluginID: plugin.ID, PrincipalUrn: urn.PrincipalWildcard,
	})
	require.NoError(t, err)
	tx = testenv.BeginTx(t, ctx, fixture.conn)
	require.ErrorIs(t, guard.CheckGatewayAttachment(ctx, tx, fixture.orgID, fixture.projectID, plugin.ID, gateway.ID), ErrPrivateGatewayAudience)
	require.ErrorIs(t, CheckGatewayNetworkMode(ctx, tx, fixture.orgID, fixture.projectID, gateway.ID, networkaccess.ModePrivateOnly), ErrPrivateGatewayAudience)
	require.NoError(t, CheckGatewayNetworkMode(ctx, tx, fixture.orgID, fixture.projectID, gateway.ID, networkaccess.ModePublicOnly))
}

func TestGuardPublicVisibilityCannotUseOrganisationApproval(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, _ := seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	seedDecision(t, fixture, "https://mcp.example.test/server", "approved", []string{urn.PrincipalWildcard})
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	guard := NewGuard()
	require.ErrorIs(t, guard.CheckPublicVisibility(t.Context(), tx, fixture.orgID, fixture.projectID, serverID), ErrApprovalRequired)
	require.NoError(t, guard.CheckPublicVisibility(t.Context(), tx, fixture.orgID, fixture.projectID, uuid.New()))
}

// TestGuardRefusalNamesTheTarget proves a caller can recover the exact URL that
// lacked approval, which is what lets the Platform MCP file a review for it.
func TestGuardRefusalNamesTheTarget(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, _ := seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	plugin, err := pluginsrepo.New(fixture.conn).CreatePlugin(t.Context(), pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Name: "Named", Slug: "named", Description: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(t.Context(), pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: fixture.orgID, PluginID: plugin.ID, PrincipalUrn: "role:developers",
	})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	err = NewGuard().CheckAttachment(t.Context(), tx, fixture.orgID, fixture.projectID, plugin.ID, serverID)
	require.ErrorIs(t, err, ErrApprovalRequired)
	var refusal *ApprovalRequiredError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "https://mcp.example.test/server", refusal.CanonicalURL)
}

// TestGuardWithoutBlockingPolicyAdmitsEverything proves the policy, not a flag,
// is what decides: with no enabled block policy nothing is refused.
func TestGuardWithoutBlockingPolicyAdmitsEverything(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, remoteID := seedAdmissionRemote(t, fixture)
	plugin, err := pluginsrepo.New(fixture.conn).CreatePlugin(t.Context(), pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Name: "Open", Slug: "open", Description: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(t.Context(), pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: fixture.orgID, PluginID: plugin.ID, PrincipalUrn: urn.PrincipalWildcard,
	})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	guard := NewGuard()
	require.NoError(t, guard.CheckAttachment(t.Context(), tx, fixture.orgID, fixture.projectID, plugin.ID, serverID))
	require.NoError(t, guard.CheckPluginAudience(t.Context(), tx, fixture.orgID, fixture.projectID, plugin.ID, []string{urn.PrincipalWildcard}))
	require.NoError(t, guard.CheckRemoteTarget(t.Context(), tx, fixture.orgID, fixture.projectID, remoteID, "https://mcp.example.test/changed"))
}

func TestGuardProspectiveDefaultUsesReservedSlugAudience(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, _ := seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	plugin, err := pluginsrepo.New(fixture.conn).CreatePlugin(t.Context(), pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Name: "Reserved", Slug: "default", Description: pgtype.Text{},
	})
	require.NoError(t, err)
	guard := NewGuard()
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	require.NoError(t, guard.CheckProspectiveDefaultAttachment(t.Context(), tx, fixture.orgID, fixture.projectID, serverID))
	require.NoError(t, tx.Commit(t.Context()))
	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(t.Context(), pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: fixture.orgID, PluginID: plugin.ID, PrincipalUrn: "role:developers",
	})
	require.NoError(t, err)
	seedDecision(t, fixture, "https://mcp.example.test/server", "approved", []string{"role:developers"})
	tx = testenv.BeginTx(t, t.Context(), fixture.conn)
	require.NoError(t, guard.CheckProspectiveDefaultAttachment(t.Context(), tx, fixture.orgID, fixture.projectID, serverID))
}

func TestGuardProspectiveMissingDefaultSeedsOnlyDefaultProject(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, _ := seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	guard := NewGuard()
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	require.ErrorIs(t, guard.CheckProspectiveDefaultAttachment(t.Context(), tx, fixture.orgID, fixture.projectID, serverID), ErrApprovalRequired)
	require.NoError(t, tx.Rollback(t.Context()))
	project, err := projectsrepo.New(fixture.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: fixture.orgID, Name: "Other", Slug: "other"})
	require.NoError(t, err)
	fixture.projectID = project.ID
	serverID, _ = seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	tx = testenv.BeginTx(t, t.Context(), fixture.conn)
	require.NoError(t, guard.CheckProspectiveDefaultAttachment(t.Context(), tx, fixture.orgID, fixture.projectID, serverID))
}

// recordingApprover stands in for the MCP approval service: it records what
// the guard asked for and seeds the decision the real approver would write.
type recordingApprover struct {
	fixture admissionFixture
	t       *testing.T
	inputs  []ApproveDirectRemoteInput
}

func (a *recordingApprover) ApproveDirectRemote(_ context.Context, _ pgx.Tx, input ApproveDirectRemoteInput) error {
	a.inputs = append(a.inputs, input)
	seedDecision(a.t, a.fixture, input.CanonicalURL, "approved", input.PrincipalURNs)
	return nil
}

func adminContext(ctx context.Context, userID string, admin bool) context.Context {
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: userID, IsAdmin: admin})
}

// An organization administrator's write is the approval: the guard records it
// for the write's audience and admits the write. Anyone else is still refused,
// and so is an administrator when no approver is wired.
func TestGuardRecordsAnAdministratorsWriteAsTheApproval(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, _ := seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	plugin, err := pluginsrepo.New(fixture.conn).CreatePlugin(t.Context(), pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Name: "Support", Slug: "support", Description: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(t.Context(), pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: fixture.orgID, PluginID: plugin.ID, PrincipalUrn: "role:developers",
	})
	require.NoError(t, err)

	// A non-administrator, and an administrator without an approver, are refused.
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	require.ErrorIs(t, NewGuard().CheckAttachment(adminContext(t.Context(), "member", false), tx, fixture.orgID, fixture.projectID, plugin.ID, serverID), ErrApprovalRequired)
	require.ErrorIs(t, NewGuard().CheckAttachment(adminContext(t.Context(), "admin", true), tx, fixture.orgID, fixture.projectID, plugin.ID, serverID), ErrApprovalRequired)
	approver := &recordingApprover{fixture: fixture, t: t}
	require.ErrorIs(t, NewGuard().WithApprover(approver).CheckAttachment(adminContext(t.Context(), "member", false), tx, fixture.orgID, fixture.projectID, plugin.ID, serverID), ErrApprovalRequired)
	require.ErrorIs(t, NewGuard().WithApprover(approver).CheckAttachment(t.Context(), tx, fixture.orgID, fixture.projectID, plugin.ID, serverID), ErrApprovalRequired, "no actor in context is not an administrator")
	require.Empty(t, approver.inputs)
	require.NoError(t, tx.Rollback(t.Context()))

	// An administrator is admitted, and the approval names the audience.
	tx = testenv.BeginTx(t, t.Context(), fixture.conn)
	require.NoError(t, NewGuard().WithApprover(approver).CheckAttachment(adminContext(t.Context(), "admin", true), tx, fixture.orgID, fixture.projectID, plugin.ID, serverID))
	require.NoError(t, tx.Commit(t.Context()))
	require.Len(t, approver.inputs, 1)
	require.Equal(t, "https://mcp.example.test/server", approver.inputs[0].CanonicalURL)
	require.Equal(t, []string{"role:developers"}, approver.inputs[0].PrincipalURNs)
	require.Equal(t, "admin", approver.inputs[0].ActorUserID)
	require.Equal(t, fixture.projectID, approver.inputs[0].ProjectID)

	// The approval is standing: a later write by anyone is covered.
	tx = testenv.BeginTx(t, t.Context(), fixture.conn)
	require.NoError(t, NewGuard().CheckAttachment(adminContext(t.Context(), "member", false), tx, fixture.orgID, fixture.projectID, plugin.ID, serverID))
	require.NoError(t, tx.Rollback(t.Context()))
}

// An approver that does not actually cover the audience must not let the
// write through on trust.
func TestGuardRefusesWhenTheRecordedApprovalDoesNotCoverTheAudience(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, _ := seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	plugin, err := pluginsrepo.New(fixture.conn).CreatePlugin(t.Context(), pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Name: "Ops", Slug: "ops", Description: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(t.Context(), pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: fixture.orgID, PluginID: plugin.ID, PrincipalUrn: "role:operators",
	})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	noop := approverFunc(func(context.Context, pgx.Tx, ApproveDirectRemoteInput) error { return nil })
	err = NewGuard().WithApprover(noop).CheckAttachment(adminContext(t.Context(), "admin", true), tx, fixture.orgID, fixture.projectID, plugin.ID, serverID)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NotErrorIs(t, err, ErrApprovalRequired)
}

type approverFunc func(context.Context, pgx.Tx, ApproveDirectRemoteInput) error

func (f approverFunc) ApproveDirectRemote(ctx context.Context, tx pgx.Tx, input ApproveDirectRemoteInput) error {
	return f(ctx, tx, input)
}
