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
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	tsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

func (f *fixture) createToolsetWithHTTPTool(t *testing.T, slug string, toolName string, destructiveHint *bool) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	toolsetID := f.createToolset(t, slug)

	deploymentID := f.createCompletedDeployment(t)
	toolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", uuid.NewString()[:8])
	var destructive pgtype.Bool
	if destructiveHint != nil {
		destructive = pgtype.Bool{Bool: *destructiveHint, Valid: true}
	}
	_, err := deploymentsrepo.New(f.conn).CreateOpenAPIv3ToolDefinition(ctx, deploymentsrepo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:           f.projectID,
		DeploymentID:        deploymentID,
		Openapiv3DocumentID: uuid.NullUUID{},
		ToolUrn:             toolURN,
		Name:                toolName,
		UntruncatedName:     pgtype.Text{String: "", Valid: true},
		Openapiv3Operation:  pgtype.Text{},
		Summary:             "Test tool",
		Description:         "A test tool",
		Tags:                []string{},
		Confirm:             pgtype.Text{},
		ConfirmPrompt:       pgtype.Text{},
		XGram:               pgtype.Bool{},
		OriginalName:        pgtype.Text{},
		OriginalSummary:     pgtype.Text{},
		OriginalDescription: pgtype.Text{},
		Security:            []byte("[]"),
		HttpMethod:          "POST",
		Path:                "/test",
		SchemaVersion:       "3.0.0",
		Schema:              []byte("{}"),
		HeaderSettings:      []byte("{}"),
		QuerySettings:       []byte("{}"),
		PathSettings:        []byte("{}"),
		ServerEnvVar:        "TEST_SERVER_URL",
		DefaultServerUrl:    pgtype.Text{},
		RequestContentType:  pgtype.Text{},
		ResponseFilter:      nil,
		ReadOnlyHint:        pgtype.Bool{},
		DestructiveHint:     destructive,
		IdempotentHint:      pgtype.Bool{},
		OpenWorldHint:       pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = tsrepo.New(f.conn).CreateToolsetVersion(ctx, tsrepo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return toolsetID
}

func (f *fixture) createCompletedDeployment(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	deployments := deploymentsrepo.New(f.conn)
	idempotencyKey := "test-" + uuid.NewString()

	_, err := deployments.CreateDeployment(ctx, deploymentsrepo.CreateDeploymentParams{
		IdempotencyKey: idempotencyKey,
		UserID:         "test-user",
		OrganizationID: f.orgID,
		ProjectID:      f.projectID,
		GithubRepo:     pgtype.Text{},
		GithubPr:       pgtype.Text{},
		GithubSha:      pgtype.Text{},
		ExternalID:     pgtype.Text{},
		ExternalUrl:    pgtype.Text{},
	})
	require.NoError(t, err)

	deployment, err := deployments.GetDeploymentByIdempotencyKey(ctx, deploymentsrepo.GetDeploymentByIdempotencyKeyParams{
		IdempotencyKey: idempotencyKey,
		ProjectID:      f.projectID,
	})
	require.NoError(t, err)

	for _, status := range []string{"created", "pending", "completed"} {
		_, err = deployments.TransitionDeployment(ctx, deploymentsrepo.TransitionDeploymentParams{
			DeploymentID: deployment.Deployment.ID,
			Status:       status,
			ProjectID:    f.projectID,
			Event:        "test",
			Message:      "test deployment status",
		})
		require.NoError(t, err)
	}

	return deployment.Deployment.ID
}
