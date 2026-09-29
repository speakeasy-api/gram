package roleprovisioning_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	pluginrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	provisioningrepo "github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/stretchr/testify/require"
)

func statusRole(t *testing.T, s roleprovisioning.Status, role string) roleprovisioning.RoleStatus {
	t.Helper()
	for _, r := range s.Roles {
		if r.RoleURN == role {
			return r
		}
	}
	t.Fatalf("role %s absent", role)
	return roleprovisioning.RoleStatus{}
}

func TestSettingsServiceConfiguresWithoutReconciliationDependencies(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	settings := roleprovisioning.NewSettings(f.db, audit.NewLogger())
	saved, err := settings.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.False(t, saved.Enabled)
	require.Zero(t, saved.Version)
	in := roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, ExpectedVersion: saved.Version, Enabled: true, ProjectID: &f.project, Roles: nil}
	version, err := settings.Configure(t.Context(), in)
	require.NoError(t, err)
	require.EqualValues(t, 1, version)
	_, err = settings.Configure(t.Context(), in)
	require.ErrorIs(t, err, roleprovisioning.ErrConflict)
	saved, err = settings.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.True(t, saved.Enabled)
	shared, err := f.service.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, saved, shared)
	status, err := settings.Status(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, version, status.Version)
	require.False(t, statusRole(t, status, f.role).PluginID.Valid, "saving intent must not reconcile")
	result := f.reconcile(f.role)
	status, err = settings.Status(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, result.PluginID, statusRole(t, status, f.role).PluginID.UUID)
	require.True(t, statusRole(t, status, f.role).PluginID.Valid)
}

func TestStatusDefaultOffAndTenantIsolation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := "org_status_other"
	_, err := orgrepo.New(f.db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: other, Name: "Other", Slug: "status-other", WorkosID: pgtype.Text{String: "", Valid: false}, Whitelisted: pgtype.Bool{Bool: false, Valid: false}, CreationSource: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	foreignProject := f.addProject(other, "foreign")
	foreignRole := f.addRole(other, "Foreign")
	status, err := f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	require.False(t, status.Enabled)
	require.Zero(t, status.Version)
	require.False(t, status.ProjectID.Valid, "read must not silently save a default")
	role := statusRole(t, status, f.role)
	require.True(t, role.Enabled, "first selection defaults to all roles")
	require.False(t, role.Configured)
	require.Equal(t, "not_provisioned", role.OriginAudience)
	require.Empty(t, role.PendingReason)
	for _, r := range status.Roles {
		require.NotEqual(t, foreignRole, r.RoleURN)
	}
	for _, p := range status.Projects {
		require.NotEqual(t, foreignProject, p.ID)
	}
	saved, err := f.service.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.Empty(t, saved.Roles, "status reads must not persist initial selections")
	require.Zero(t, saved.Version)
}

func TestStatusDesiredAppliedAndDisable(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	version := f.configure(0, true)
	status, err := f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	role := statusRole(t, status, f.role)
	require.True(t, role.Configured)
	require.Equal(t, "reconciliation_pending", role.PendingReason)
	require.False(t, role.PluginID.Valid)
	plugin := f.reconcile(f.role).PluginID
	status, err = f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	role = statusRole(t, status, f.role)
	require.Equal(t, plugin, role.PluginID.UUID)
	require.Equal(t, "assigned", role.OriginAudience)
	require.Equal(t, "not_connected", role.PublicationStatus)
	require.Empty(t, role.PendingReason)
	target := f.addProject(f.org, "new-destination")
	version, err = f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{OrganizationID: f.org, Actor: f.actor, ExpectedVersion: version, Enabled: true, Roles: []roleprovisioning.Selection{{RoleURN: f.role, Enabled: true, ProjectID: &target}}})
	require.NoError(t, err)
	status, err = f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	role = statusRole(t, status, f.role)
	require.Equal(t, target, role.ProjectID.UUID)
	require.Equal(t, f.project, role.AppliedProjectID.UUID)
	require.Equal(t, plugin, role.PluginID.UUID)
	require.Equal(t, "reconciliation_pending", role.PendingReason)
	f.configure(version, false)
	status, err = f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	role = statusRole(t, status, f.role)
	require.False(t, status.Enabled)
	require.True(t, role.Enabled, "organization disable preserves per-role selection")
	require.Equal(t, "assigned", role.OriginAudience)
	require.Equal(t, plugin, role.PluginID.UUID)
	require.Empty(t, role.PendingReason)
}

func TestStatusPendingProjectAndAdmissionEvidence(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	pending := uuid.Nil
	version, err := f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{OrganizationID: f.org, Actor: f.actor, Enabled: true, ProjectID: &pending})
	require.NoError(t, err)
	status, err := f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, "choose_project", statusRole(t, status, f.role).PendingReason)
	_, err = f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{OrganizationID: f.org, Actor: f.actor, ExpectedVersion: version, Enabled: true, ProjectID: &f.project})
	require.NoError(t, err)
	plugin := f.reconcile(f.role).PluginID
	_, err = pluginrepo.New(f.db).RemoveAllPluginAssignments(t.Context(), pluginrepo.RemoveAllPluginAssignmentsParams{PluginID: plugin, OrganizationID: f.org, ProjectID: f.project})
	require.NoError(t, err)
	require.NoError(t, provisioningrepo.New(f.db).RecordAttempt(t.Context(), provisioningrepo.RecordAttemptParams{OrganizationID: conv.ToPGText(f.org), RoleUrn: f.role, ErrorCode: conv.ToPGText("audience_approval_required")}))
	status, err = f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	role := statusRole(t, status, f.role)
	require.Equal(t, "not_assigned", role.OriginAudience)
	require.Equal(t, "audience_approval_required", role.PendingReason)
	require.Equal(t, plugin, role.PluginID.UUID)
	require.Equal(t, "not_connected", role.PublicationStatus)
	// An error from an earlier configuration is not evidence that the new
	// destination requires approval; only reconciliation can establish that.
	target := f.addProject(f.org, "after-approval")
	_, err = f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{OrganizationID: f.org, Actor: f.actor, ExpectedVersion: version + 1, Enabled: true, Roles: []roleprovisioning.Selection{{RoleURN: f.role, Enabled: true, ProjectID: &target}}})
	require.NoError(t, err)
	status, err = f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, "reconciliation_pending", statusRole(t, status, f.role).PendingReason)
}

func TestStatusPublicationEvidenceIsIndependent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.configure(0, true)
	plugin := f.reconcile(f.role).PluginID
	q := pluginrepo.New(f.db)
	connection := pluginrepo.UpsertGitHubConnectionParams{ProjectID: f.project, InstallationID: 1, RepoOwner: "fixture", RepoName: "fixture", MarketplaceToken: pgtype.Text{String: "", Valid: false}, PublishedMcpFingerprints: nil, PublishedHooksVersion: pgtype.Text{String: "", Valid: false}, PublishedHooksConfig: nil}
	_, err := q.UpsertGitHubConnection(t.Context(), connection)
	require.NoError(t, err)
	status, err := f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, "not_published", statusRole(t, status, f.role).PublicationStatus)
	p, err := q.GetPlugin(t.Context(), pluginrepo.GetPluginParams{ID: plugin, OrganizationID: f.org, ProjectID: f.project})
	require.NoError(t, err)
	connection.PublishedMcpFingerprints, err = json.Marshal(map[string]string{p.Slug: "old-content-hash"})
	require.NoError(t, err)
	_, err = q.UpsertGitHubConnection(t.Context(), connection)
	require.NoError(t, err)
	status, err = f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	role := statusRole(t, status, f.role)
	require.Equal(t, "published_before", role.PublicationStatus, "historical fingerprint cannot prove current freshness")
	require.Equal(t, "assigned", role.OriginAudience)
	require.NoError(t, q.DeletePlugin(t.Context(), pluginrepo.DeletePluginParams{ID: plugin, OrganizationID: f.org, ProjectID: f.project}))
	status, err = f.service.Status(t.Context(), f.org)
	require.NoError(t, err)
	role = statusRole(t, status, f.role)
	require.False(t, role.PluginID.Valid)
	require.Equal(t, "not_provisioned", role.PublicationStatus)
	require.Equal(t, "reconciliation_pending", role.PendingReason)
}
