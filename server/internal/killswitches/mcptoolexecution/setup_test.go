//nolint:glint // Integration fixtures intentionally create tenant rows with raw SQL in isolated databases.
package mcptoolexecution

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("clean up test infrastructure: %v", err)
	}

	os.Exit(code)
}

func newTestDatabase(t *testing.T, name string) (*pgxpool.Pool, string) {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	insertOrganization(t, conn, orgID)
	return conn, orgID
}

func insertOrganization(t *testing.T, conn *pgxpool.Pool, organizationID string) {
	t.Helper()
	_, err := conn.Exec(t.Context(), `
		INSERT INTO organization_metadata (id, name, slug)
		VALUES ($1, 'Test Organization', $1)
	`, organizationID)
	require.NoError(t, err)
}

func insertUser(t *testing.T, conn *pgxpool.Pool, userID string, deletedAt *time.Time) {
	t.Helper()
	_, err := conn.Exec(t.Context(), `
		INSERT INTO users (id, email, display_name, deleted_at)
		VALUES ($1, $1 || '@example.test', 'Test User', $2)
	`, userID, deletedAt)
	require.NoError(t, err)
}

func insertMembership(t *testing.T, conn *pgxpool.Pool, organizationID, userID string, deletedAt *time.Time) {
	t.Helper()
	_, err := conn.Exec(t.Context(), `
		INSERT INTO organization_user_relationships (organization_id, user_id, deleted_at)
		VALUES ($1, $2, $3)
	`, organizationID, userID, deletedAt)
	require.NoError(t, err)
}

func insertProject(t *testing.T, conn *pgxpool.Pool, organizationID, slug string, deletedAt *time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := conn.QueryRow(t.Context(), `
		INSERT INTO projects (name, slug, organization_id, deleted_at)
		VALUES ($1, $1, $2, $3)
		RETURNING id
	`, slug, organizationID, deletedAt).Scan(&id)
	require.NoError(t, err)
	return id
}

// insertMCPServer creates a toolset-backed mcp_servers row; the table
// requires exactly one backend reference.
func insertMCPServer(t *testing.T, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, deletedAt *time.Time) uuid.UUID {
	t.Helper()
	slug := "ts-" + uuid.NewString()[:26]
	var toolsetID uuid.UUID
	err := conn.QueryRow(t.Context(), `
		INSERT INTO toolsets (organization_id, project_id, name, slug)
		VALUES ($1, $2, $3, $3)
		RETURNING id
	`, organizationID, projectID, slug).Scan(&toolsetID)
	require.NoError(t, err)

	var id uuid.UUID
	err = conn.QueryRow(t.Context(), `
		INSERT INTO mcp_servers (project_id, toolset_id, visibility, deleted_at)
		VALUES ($1, $2, 'private', $3)
		RETURNING id
	`, projectID, toolsetID, deletedAt).Scan(&id)
	require.NoError(t, err)
	return id
}

// clearPrescriptions removes every prescription fixture for the organization
// so a test can stage a fresh scope scenario.
func clearPrescriptions(t *testing.T, conn *pgxpool.Pool, organizationID string) {
	t.Helper()
	for _, table := range []string{
		"killswitch_prescription_version_resources",
		"killswitch_prescription_versions",
		"killswitch_prescriptions",
	} {
		_, err := conn.Exec(t.Context(), "DELETE FROM "+table+" WHERE organization_id = $1", organizationID)
		require.NoError(t, err)
	}
}

func deletePrescription(t *testing.T, conn *pgxpool.Pool, organizationID string, prescriptionID uuid.UUID) {
	t.Helper()
	_, err := conn.Exec(t.Context(), "DELETE FROM killswitch_prescriptions WHERE organization_id = $1 AND id = $2", organizationID, prescriptionID)
	require.NoError(t, err)
}

type prescriptionFixture struct {
	ID            uuid.UUID
	DefinitionKey killswitches.DefinitionKey
	PrincipalKey  string
	ResourceKind  killswitches.ResourceKind
	Scope         string
	Resources     []string
	ExternalNote  string
}

// insertPrescription creates an immediately active prescription with the
// concrete user principal and mcp_server resource kind.
func insertPrescription(t *testing.T, conn *pgxpool.Pool, organizationID string, fixture prescriptionFixture) {
	t.Helper()

	definitionKey := fixture.DefinitionKey
	if definitionKey == "" {
		definitionKey = DefinitionKeyMCPToolExecution
	}
	resourceKind := fixture.ResourceKind
	if resourceKind == "" {
		resourceKind = ResourceKindMCPServer
	}
	err := testrepo.New(conn).InsertKillswitchPrescriptionFixture(t.Context(), testrepo.InsertKillswitchPrescriptionFixtureParams{
		PrescriptionID: fixture.ID,
		OrganizationID: organizationID,
		DefinitionKey:  string(definitionKey),
		PrincipalKind:  string(PrincipalKindUser),
		PrincipalKey:   fixture.PrincipalKey,
		ResourceKind:   string(resourceKind),
		ResourceScope:  fixture.Scope,
		InternalNote:   "test fixture context",
		ExternalNote:   fixture.ExternalNote,
		ResourceKeys:   fixture.Resources,
	})
	require.NoError(t, err)
}
