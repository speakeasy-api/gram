package skills_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/access/accesstest"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var (
	infra *testenv.Environment
)

type testInstance struct {
	service        *skills.Service
	conn           *pgxpool.Pool
	storage        assets.BlobStore
	sessionManager *sessions.Manager
	repo           *assetsrepo.Queries
}

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func newTestSkillsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	storage := assetstest.NewTestBlobStore(t)
	svc := skills.NewService(logger, tracerProvider, conn, sessionManager, storage, access.NewManager(logger, conn, accesstest.AlwaysEnabledFeatureChecker{}))

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		storage:        storage,
		sessionManager: sessionManager,
		repo:           assetsrepo.New(conn),
	}
}

func newCapturePayload(contentType string, contentLength int64, contentSHA256 string) *gen.CaptureSkillForm {
	return &gen.CaptureSkillForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "golang",
		Scope:            "project",
		DiscoveryRoot:    "project_agents",
		SourceType:       "local_filesystem",
		ContentSha256:    contentSHA256,
		AssetFormat:      "zip",
		ResolutionStatus: "resolved",
		SkillID:          nil,
		SkillVersionID:   nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}
}
