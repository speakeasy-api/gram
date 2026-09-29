package roleprovisioning_test

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	pluginrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	rolerepo "github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"log"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

var cloneDB testenv.PostgresDBCloneFunc

func TestMain(m *testing.M) {
	container, clone, err := testenv.NewTestPostgres(context.Background())
	if err != nil {
		log.Fatalf("start test postgres: %v", err)
	}
	cloneDB = clone
	code := m.Run()
	if err := container.Terminate(context.Background()); err != nil {
		log.Printf("terminate test postgres: %v", err)
	}
	os.Exit(code)
}

type fixture struct {
	t       *testing.T
	db      *pgxpool.Pool
	service *roleprovisioning.Service
	org     string
	project uuid.UUID
	role    string
	actor   roleprovisioning.Actor
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := cloneDB(t, "roleprovisioning")
	require.NoError(t, err)
	f := &fixture{t: t, db: db, org: "org_role_fixture", actor: roleprovisioning.Actor{Principal: urn.NewSystemPrincipal("role-provisioning-test")}}
	f.service = roleprovisioning.New(db, audit.NewLogger(), admission.NewGuard(nil, nil), plugins.PublicationRequests{Enabled: false})
	f.addOrganization(f.org)
	f.project = f.addProject(f.org, "default")
	f.role = f.addRole(f.org, "Engineers")
	return f
}
func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	_, err := f.db.Exec(f.t.Context(), sql, args...) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
	require.NoError(f.t, err)
}
func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	require.NoError(f.t, f.db.QueryRow(f.t.Context(), sql, args...).Scan(&n)) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
	return n
}
func (f *fixture) addProject(org, slug string) uuid.UUID {
	f.t.Helper()
	p, err := projectrepo.New(f.db).CreateProject(f.t.Context(), projectrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: org})
	require.NoError(f.t, err)
	return p.ID
}
func (f *fixture) addOrganization(org string) {
	f.t.Helper()
	_, err := orgrepo.New(f.db).UpsertOrganizationMetadata(f.t.Context(), orgrepo.UpsertOrganizationMetadataParams{ID: org, Name: org, Slug: org, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{}})
	require.NoError(f.t, err)
}
func (f *fixture) addRole(org, name string) string {
	f.t.Helper()
	slug := uuid.NewString()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	if org == "" {
		id, err := accessrepo.New(f.db).SeedGlobalRole(f.t.Context(), accessrepo.SeedGlobalRoleParams{WorkosSlug: slug, WorkosName: name, WorkosDescription: pgtype.Text{}, WorkosCreatedAt: now, WorkosUpdatedAt: now, WorkosLastEventID: pgtype.Text{}})
		require.NoError(f.t, err)
		return "role:global:" + id.String()
	}
	role, err := accessrepo.New(f.db).CreateOrganizationRole(f.t.Context(), accessrepo.CreateOrganizationRoleParams{OrganizationID: org, WorkosSlug: slug, WorkosName: name, WorkosDescription: pgtype.Text{}, WorkosCreatedAt: now, WorkosUpdatedAt: now, WorkosLastEventID: pgtype.Text{}})
	require.NoError(f.t, err)
	return "role:organization:" + role.ID.String()
}
func (f *fixture) configure(version int64, enabled bool, selections ...roleprovisioning.Selection) int64 {
	f.t.Helper()
	v, err := f.service.Configure(f.t.Context(), roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, ExpectedVersion: version, Enabled: enabled, Roles: selections})
	require.NoError(f.t, err)
	require.Equal(f.t, version+1, v)
	return v
}
func (f *fixture) reconcile(role string) roleprovisioning.Result {
	f.t.Helper()
	result, err := f.service.Reconcile(f.t.Context(), f.org, role, f.actor)
	require.NoError(f.t, err)
	return result
}
func (f *fixture) audiences(id uuid.UUID) []string {
	f.t.Helper()
	plugin := f.plugin(id)
	rows, err := rolerepo.New(f.db).ListCleanupPluginPrincipals(f.t.Context(), rolerepo.ListCleanupPluginPrincipalsParams{PluginID: id, OrganizationID: plugin.OrganizationID, ProjectID: plugin.ProjectID})
	require.NoError(f.t, err)
	slices.Sort(rows)
	return rows
}
func (f *fixture) assertEmpty(id uuid.UUID) {
	f.t.Helper()
	require.NotEqual(f.t, uuid.Nil, id)
	rows, err := pluginrepo.New(f.db).ListPluginServers(f.t.Context(), id)
	require.NoError(f.t, err)
	require.Empty(f.t, rows)
}
func (f *fixture) plugin(id uuid.UUID) testrepo.Plugin {
	f.t.Helper()
	row, err := testrepo.New(f.db).GetRoleLifecyclePluginFixture(f.t.Context(), testrepo.GetRoleLifecyclePluginFixtureParams{PluginID: id, OrganizationID: f.org})
	require.NoError(f.t, err)
	return row
}
func (f *fixture) addAssignment(id uuid.UUID, principal string) {
	f.t.Helper()
	_, err := pluginrepo.New(f.db).AddPluginAssignment(f.t.Context(), pluginrepo.AddPluginAssignmentParams{PluginID: id, OrganizationID: f.org, PrincipalUrn: principal})
	require.NoError(f.t, err)
}
func (f *fixture) deletePlugin(id, project uuid.UUID) {
	f.t.Helper()
	require.NoError(f.t, pluginrepo.New(f.db).DeletePlugin(f.t.Context(), pluginrepo.DeletePluginParams{ID: id, OrganizationID: f.org, ProjectID: project}))
}
func (f *fixture) removeAssignments(id, project uuid.UUID) {
	f.t.Helper()
	_, err := pluginrepo.New(f.db).RemoveAllPluginAssignments(f.t.Context(), pluginrepo.RemoveAllPluginAssignmentsParams{PluginID: id, OrganizationID: f.org, ProjectID: project})
	require.NoError(f.t, err)
}
func (f *fixture) associations() []testrepo.RolePluginAssociation {
	f.t.Helper()
	rows, err := testrepo.New(f.db).ListRoleLifecycleAssociationsFixture(f.t.Context(), testrepo.ListRoleLifecycleAssociationsFixtureParams{OrganizationID: pgtype.Text{String: f.org, Valid: true}, RoleUrn: f.role})
	require.NoError(f.t, err)
	return rows
}
func (f *fixture) association(id uuid.UUID) testrepo.RolePluginAssociation {
	f.t.Helper()
	for _, row := range f.associations() {
		if row.PluginID.UUID == id {
			return row
		}
	}
	f.t.Fatalf("missing association for plugin %s", id)
	return testrepo.RolePluginAssociation{}
}
func (f *fixture) roleSetting() rolerepo.RoleProvisioningSetting {
	f.t.Helper()
	row, err := rolerepo.New(f.db).GetRoleSetting(f.t.Context(), rolerepo.GetRoleSettingParams{OrganizationID: pgtype.Text{String: f.org, Valid: true}, RoleUrn: f.role})
	require.NoError(f.t, err)
	return row
}
func (f *fixture) projectPlugins(project uuid.UUID) []pluginrepo.ListPluginsRow {
	f.t.Helper()
	rows, err := pluginrepo.New(f.db).ListPlugins(f.t.Context(), pluginrepo.ListPluginsParams{OrganizationID: f.org, ProjectID: project})
	require.NoError(f.t, err)
	return rows
}
