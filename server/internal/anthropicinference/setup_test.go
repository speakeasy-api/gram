package anthropicinference

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/chattest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	environment, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = environment
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("clean up test infrastructure: %v", err)
	}
	os.Exit(code)
}

func newTestStore(t *testing.T) (*postgresStore, *pgxpool.Pool, Config) {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, "anthropicinferencetest")
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Inference Example", Slug: orgID, WorkosID: pgtype.Text{String: orgID, Valid: true}, Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Inference Example", Slug: "example", OrganizationID: orgID})
	require.NoError(t, err)
	writer := chattest.NewMessageWriter(t, infra, db)
	return &postgresStore{db: db, writer: writer, titles: &recordingTitleGenerator{}, logger: testenv.NewLogger(t)}, db, Config{ID: "example", OrganizationID: orgID, ProjectID: project.ID, TenantID: "tenant-example", SigningSecrets: nil}
}
