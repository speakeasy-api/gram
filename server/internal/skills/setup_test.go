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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
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
	skillsRepo     *skillsrepo.Queries
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

	defaultMode := "project_and_user"
	return newTestSkillsServiceWithCaptureModeAndFeature(t, &defaultMode, true)
}

func newTestSkillsServiceWithCaptureMode(t *testing.T, mode *string) (context.Context, *testInstance) {
	t.Helper()

	return newTestSkillsServiceWithCaptureModeAndFeature(t, mode, true)
}

func newTestSkillsServiceWithCaptureModeAndFeature(t *testing.T, mode *string, featureEnabled bool) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	featureRedisDB := 12
	if !featureEnabled {
		featureRedisDB = 13
	}
	featureRedisClient, err := infra.NewRedisClient(t, featureRedisDB)
	require.NoError(t, err)

	storage := assetstest.NewTestBlobStore(t)
	accessManager := access.NewManager(logger, conn, accesstest.AlwaysEnabledFeatureChecker{}, workos.NewStubClient(), cache.NoopCache)
	featuresClient := productfeatures.NewClient(logger, tracerProvider, conn, featureRedisClient)
	svc := skills.NewService(logger, tracerProvider, conn, sessionManager, storage, accessManager, featuresClient)

	ti := &testInstance{
		service:        svc,
		conn:           conn,
		storage:        storage,
		sessionManager: sessionManager,
		repo:           assetsrepo.New(conn),
		skillsRepo:     skillsrepo.New(conn),
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	if featureEnabled {
		_, err = productfeaturesrepo.New(conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    string(productfeatures.FeatureSkillsCapture),
		})
		require.NoError(t, err)
	}

	if mode != nil {
		_, err = ti.skillsRepo.UpsertOrganizationCapturePolicy(ctx, skillsrepo.UpsertOrganizationCapturePolicyParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			Mode:           *mode,
		})
		require.NoError(t, err)
	}

	return ctx, ti
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
