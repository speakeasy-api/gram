package aiintegrations

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("failed to launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func newStoreTestDB(t *testing.T) (context.Context, *pgxpool.Pool, *Store, string) {
	t.Helper()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "aiintegrationstestdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "AI Integrations Test",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: orgID, Valid: true},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	_, err = projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "AI Integrations Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	store := NewStore(testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t))
	return ctx, conn, store, orgID
}
