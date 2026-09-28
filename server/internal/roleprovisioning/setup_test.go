package roleprovisioning_test

import (
	"context"
	"log"
	"os"
	"testing"

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
	f.exec(`INSERT INTO organization_metadata (id,name,slug) VALUES ($1,'Fixture','fixture')`, f.org)
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
	id := uuid.New()
	f.exec(`INSERT INTO projects (id,organization_id,name,slug) VALUES ($1,$2,$3,$3)`, id, org, slug)
	return id
}
func (f *fixture) addRole(org, name string) string {
	f.t.Helper()
	id := uuid.New()
	if org == "" {
		f.exec(`INSERT INTO global_roles (id,workos_slug,workos_name,workos_created_at,workos_updated_at) VALUES ($1,$2,$3,now(),now())`, id, id.String(), name)
		return "role:global:" + id.String()
	}
	f.exec(`INSERT INTO organization_roles (id,organization_id,workos_slug,workos_name,workos_created_at,workos_updated_at) VALUES ($1,$2,$3,$4,now(),now())`, id, org, id.String(), name)
	return "role:organization:" + id.String()
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
	rows, err := f.db.Query(f.t.Context(), `SELECT principal_urn FROM plugin_assignments WHERE plugin_id=$1 ORDER BY principal_urn`, id) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
	require.NoError(f.t, err)
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var principal string
		require.NoError(f.t, rows.Scan(&principal))
		result = append(result, principal)
	}
	require.NoError(f.t, rows.Err())
	return result
}
func (f *fixture) assertEmpty(id uuid.UUID) {
	f.t.Helper()
	require.NotEqual(f.t, uuid.Nil, id)
	require.Zero(f.t, f.count(`SELECT count(*) FROM plugin_servers WHERE plugin_id=$1 AND NOT deleted`, id))
}
