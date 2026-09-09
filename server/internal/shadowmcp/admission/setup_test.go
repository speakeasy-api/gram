package admission

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcprepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type admissionFixture struct {
	conn      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
}

func newAdmissionFixture(t *testing.T) admissionFixture {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := "shadow-admission-org-" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Shadow admission test",
		Slug:           "shadow-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	return admissionFixture{conn: conn, orgID: orgID, projectID: project.ID}
}

func seedAdmissionRemote(t *testing.T, fixture admissionFixture) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	remote, err := remoterepo.New(fixture.conn).CreateServer(ctx, remoterepo.CreateServerParams{
		ID: uuid.New(), ProjectID: fixture.projectID, Name: conv.ToPGText("Admission remote"), Slug: conv.ToPGText("admission-remote"), TransportType: "streamable-http", Url: "https://mcp.example.test/server",
	})
	require.NoError(t, err)
	server, err := mcprepo.New(fixture.conn).CreateMCPServer(ctx, mcprepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: fixture.projectID, Name: conv.ToPGText("Admission MCP"), Slug: conv.ToPGText("admission-mcp"), Visibility: "private",
		RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: uuid.NullUUID{},
		TunneledMcpServerID: uuid.NullUUID{}, ToolsetID: uuid.NullUUID{}, UnproxiedMcpServerID: uuid.NullUUID{}, ToolVariationsGroupID: uuid.NullUUID{}, NetworkAccessMode: pgtype.Text{},
	})
	require.NoError(t, err)
	registration, err := platformrepo.New(fixture.conn).CreatePlatformMCPCatalogRegistration(ctx, platformrepo.CreatePlatformMCPCatalogRegistrationParams{
		OrganizationID: fixture.orgID, ProjectID: fixture.projectID, SourceKind: "remote", CatalogProvider: "direct-remote-url-v1", CatalogReference: remote.Url, Status: "registered",
		ConnectionID: uuid.NullUUID{}, ConnectionGeneration: uuid.NullUUID{}, UserID: pgtype.Text{}, ActingSurface: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = platformrepo.New(fixture.conn).UpdatePlatformMCPCatalogRegistrationComponents(ctx, platformrepo.UpdatePlatformMCPCatalogRegistrationComponentsParams{
		ID: registration.ID, OrganizationID: fixture.orgID, ProjectID: fixture.projectID, Status: "registered",
		RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, RemoteMcpServerOwned: true,
		McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true}, McpServerOwned: true,
		UserSessionIssuerID: uuid.NullUUID{}, UserSessionIssuerOwned: false, McpEndpointID: uuid.NullUUID{}, McpEndpointOwned: false,
	})
	require.NoError(t, err)
	return server.ID, remote.ID
}
