package mcptoolexecution

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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
	err := orgrepo.New(conn).CreateOrganizationMetadata(t.Context(), orgrepo.CreateOrganizationMetadataParams{
		ID: organizationID, Name: "Test Organization", Slug: organizationID,
	})
	require.NoError(t, err)
}

func insertUser(t *testing.T, conn *pgxpool.Pool, userID string, deleted bool) {
	t.Helper()
	queries := testrepo.New(conn)
	err := queries.InsertUserFixture(t.Context(), testrepo.InsertUserFixtureParams{ID: userID, Email: userID + "@example.test", DisplayName: "Test User"})
	require.NoError(t, err)
	if deleted {
		require.NoError(t, queries.ForceSoftDeleteUser(t.Context(), userID))
	}
}

func insertMembership(t *testing.T, conn *pgxpool.Pool, organizationID, userID string, deleted bool) {
	t.Helper()
	queries := testrepo.New(conn)
	userText := pgtype.Text{String: userID, Valid: true}
	err := queries.CreateOrganizationUserRelationshipFixture(t.Context(), testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: organizationID, UserID: userText})
	require.NoError(t, err)
	if deleted {
		err = queries.ForceSoftDeleteOrganizationUserRelationship(t.Context(), testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{OrganizationID: organizationID, UserID: userText})
		require.NoError(t, err)
	}
}

func insertProject(t *testing.T, conn *pgxpool.Pool, organizationID, slug string, deleted bool) uuid.UUID {
	t.Helper()
	id, err := testrepo.New(conn).CreateProjectFixture(t.Context(), testrepo.CreateProjectFixtureParams{
		ID: uuid.Must(uuid.NewV7()), Name: slug, Slug: slug, OrganizationID: organizationID,
	})
	require.NoError(t, err)
	if deleted {
		_, err = projectsrepo.New(conn).DeleteProject(t.Context(), id)
		require.NoError(t, err)
	}
	return id
}

// insertMCPServer creates a toolset-backed mcp_servers row; the table
// requires exactly one backend reference.
func insertMCPServer(t *testing.T, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, deleted bool) uuid.UUID {
	t.Helper()
	queries := testrepo.New(conn)
	slug := "ts-" + uuid.NewString()[:26]
	toolsetID, err := queries.CreateToolsetFixture(t.Context(), testrepo.CreateToolsetFixtureParams{
		ID: uuid.Must(uuid.NewV7()), OrganizationID: organizationID, ProjectID: projectID, Name: slug, Slug: slug,
	})
	require.NoError(t, err)

	id, err := queries.CreateRemoteMCPServerFixture(t.Context(), testrepo.CreateRemoteMCPServerFixtureParams{
		ID: uuid.Must(uuid.NewV7()), ProjectID: projectID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	if deleted {
		_, err = mcpserversrepo.New(conn).DeleteMCPServer(t.Context(), mcpserversrepo.DeleteMCPServerParams{ID: id, ProjectID: projectID})
		require.NoError(t, err)
	}
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
		//nolint:glint // notestingrawsql: cleans up across a dynamic table list; SQLc cannot parameterize table names
		_, err := conn.Exec(t.Context(), "DELETE FROM "+table+" WHERE organization_id = $1", organizationID)
		require.NoError(t, err)
	}
}

type prescriptionFixture struct {
	ID            uuid.UUID
	PrincipalKey  string
	PrincipalKind killswitches.PrincipalKind
	Scope         string
	Resources     []string
	ExternalNote  string
}

// insertPrescription creates an immediately active mcp_tool_execution
// prescription with the concrete user principal and mcp_server resource kind.
func insertPrescription(t *testing.T, conn *pgxpool.Pool, organizationID string, fixture prescriptionFixture) {
	t.Helper()

	kind := fixture.PrincipalKind
	if kind == "" {
		kind = PrincipalKindUser
	}
	err := testrepo.New(conn).InsertKillswitchPrescriptionFixture(t.Context(), testrepo.InsertKillswitchPrescriptionFixtureParams{
		PrescriptionID: fixture.ID,
		OrganizationID: organizationID,
		DefinitionKey:  string(DefinitionKeyMCPToolExecution),
		PrincipalKind:  string(kind),
		PrincipalKey:   fixture.PrincipalKey,
		ResourceKind:   string(ResourceKindMCPServer),
		ResourceScope:  fixture.Scope,
		InternalNote:   "test fixture context",
		ExternalNote:   fixture.ExternalNote,
		ResourceKeys:   fixture.Resources,
	})
	require.NoError(t, err)
}
