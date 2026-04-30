package shadowmcp_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	tsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
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

type fixture struct {
	conn        *pgxpool.Pool
	redisClient *redis.Client
	client      *shadowmcp.Client
	orgID       string
	projectID   uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	cacheImpl := cache.NewRedisCacheAdapter(redisClient)
	client := shadowmcp.NewClient(logger, conn, cacheImpl)

	orgID := "test-org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "test-project",
		Slug:           "test-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	return &fixture{
		conn:        conn,
		redisClient: redisClient,
		client:      client,
		orgID:       orgID,
		projectID:   project.ID,
	}
}

func (f *fixture) createPolicy(t *testing.T, name string, enabled bool, sources []string) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = riskrepo.New(f.conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:             id,
		ProjectID:      f.projectID,
		OrganizationID: f.orgID,
		Name:           name,
		Sources:        sources,
		Enabled:        enabled,
	})
	require.NoError(t, err)
	return id
}

func (f *fixture) createToolset(t *testing.T, slug string) uuid.UUID {
	t.Helper()
	toolset, err := tsrepo.New(f.conn).CreateToolset(t.Context(), tsrepo.CreateToolsetParams{
		OrganizationID: f.orgID,
		ProjectID:      f.projectID,
		Name:           slug,
		Slug:           slug,
	})
	require.NoError(t, err)
	return toolset.ID
}
