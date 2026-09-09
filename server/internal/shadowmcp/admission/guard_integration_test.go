package admission

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestGuardRemoteTargetKillSwitchWithoutAttachments(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	_, remoteID := seedAdmissionRemote(t, fixture)
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	guard := NewGuard(nil, nil)
	rollout := RolloutConfig{Mode: ModeLegacy, DirectRemoteDistributionDisabled: true}
	require.ErrorIs(t, guard.CheckRemoteTarget(t.Context(), tx, rollout, nil, fixture.orgID, fixture.projectID, remoteID, "https://mcp.example.test/changed"), ErrDistributionDisabled)
	require.NoError(t, guard.CheckRemoteTarget(t.Context(), tx, rollout, nil, fixture.orgID, fixture.projectID, uuid.New(), "https://mcp.example.test/changed"))
}

func TestGuardPublicVisibilityCannotUseOrganisationApproval(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, _ := seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	seedDecision(t, fixture, "https://mcp.example.test/server", "approved", []string{urn.PrincipalWildcard})
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	reports := &capturedReports{outcomes: nil}
	guard := NewGuard(nil, reports)
	require.ErrorIs(t, guard.CheckPublicVisibility(t.Context(), tx, RolloutConfig{Mode: ModeEnforce}, nil, fixture.orgID, fixture.projectID, serverID), ErrApprovalRequired)
	require.NoError(t, guard.CheckPublicVisibility(t.Context(), tx, RolloutConfig{Mode: ModeReport}, nil, fixture.orgID, fixture.projectID, serverID))
	require.Equal(t, []ReportOutcome{ReportApprovalRequired}, reports.outcomes)
	require.NoError(t, guard.CheckPublicVisibility(t.Context(), tx, RolloutConfig{Mode: ModeLegacy}, nil, fixture.orgID, fixture.projectID, serverID))
	require.ErrorIs(t, guard.CheckPublicVisibility(t.Context(), tx, RolloutConfig{Mode: ModeLegacy, DirectRemoteDistributionDisabled: true}, nil, fixture.orgID, fixture.projectID, serverID), ErrDistributionDisabled)
	require.NoError(t, guard.CheckPublicVisibility(t.Context(), tx, RolloutConfig{Mode: ModeEnforce}, nil, fixture.orgID, fixture.projectID, uuid.New()))
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
	guard := NewGuard(nil, nil)
	rollout := RolloutConfig{Mode: ModeEnforce}
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	require.NoError(t, guard.CheckProspectiveDefaultAttachment(t.Context(), tx, rollout, nil, fixture.orgID, fixture.projectID, serverID))
	require.NoError(t, tx.Commit(t.Context()))
	_, err = pluginsrepo.New(fixture.conn).AddPluginAssignment(t.Context(), pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: fixture.orgID, PluginID: plugin.ID, PrincipalUrn: "role:developers",
	})
	require.NoError(t, err)
	seedDecision(t, fixture, "https://mcp.example.test/server", "approved", []string{"role:developers"})
	tx = testenv.BeginTx(t, t.Context(), fixture.conn)
	require.NoError(t, guard.CheckProspectiveDefaultAttachment(t.Context(), tx, rollout, nil, fixture.orgID, fixture.projectID, serverID))
}

func TestGuardProspectiveMissingDefaultSeedsOnlyDefaultProject(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	serverID, _ := seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	guard := NewGuard(nil, nil)
	rollout := RolloutConfig{Mode: ModeEnforce}
	tx := testenv.BeginTx(t, t.Context(), fixture.conn)
	require.ErrorIs(t, guard.CheckProspectiveDefaultAttachment(t.Context(), tx, rollout, nil, fixture.orgID, fixture.projectID, serverID), ErrApprovalRequired)
	require.NoError(t, tx.Rollback(t.Context()))
	project, err := projectsrepo.New(fixture.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: fixture.orgID, Name: "Other", Slug: "other"})
	require.NoError(t, err)
	fixture.projectID = project.ID
	serverID, _ = seedAdmissionRemote(t, fixture)
	seedBlockingPolicy(t, fixture)
	tx = testenv.BeginTx(t, t.Context(), fixture.conn)
	require.NoError(t, guard.CheckProspectiveDefaultAttachment(t.Context(), tx, rollout, nil, fixture.orgID, fixture.projectID, serverID))
}
