package telemetry_test

import (
	"context"
	"log"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background())
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err = cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *telemetry.Service
	logger         *slog.Logger
	conn           *pgxpool.Pool
	chClient       *repo.Queries
	featClient     *productfeatures.Client
	sessionManager *sessions.Manager
}

func newTestLogsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, testenv.NewTracerProvider(t))

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-test"), "", billingClient)
	require.NoError(t, err)

	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	tracerProvider := testenv.NewTracerProvider(t)

	chClient := repo.New(logger, tracerProvider, chConn)

	featClient := productfeatures.NewClient(logger, conn, redisClient)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context should be set")

	pfRepo :=  productfeaturesrepo.New(conn)

	_, err = pfRepo.EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureLogs),
	})
	require.NoError(t, err, "failed to enable logs feature for test organization")

	posthogClient := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	svc := telemetry.NewService(logger, conn, sessionManager, chatSessionsManager, chClient, featClient, posthogClient)

	return ctx, &testInstance{
		service:        svc,
		logger:         logger,
		conn:           conn,
		chClient:       chClient,
		featClient:     featClient,
		sessionManager: sessionManager,
	}
}

func setProjectID(t *testing.T, ctx context.Context, projectID string) context.Context {
	t.Helper()

	authContext, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	id := uuid.MustParse(projectID)
	authContext.ProjectID = &id

	return contextvalues.SetAuthContext(ctx, authContext)
}

func switchOrganizationInCtx(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context should be set")

	// Use a different org ID which won't have logs enabled
	// This org won't have the logs feature enabled
	authCtx.ActiveOrganizationID = uuid.Must(uuid.NewV7()).String()
	authCtx.OrganizationSlug = "organization-456"

	return contextvalues.SetAuthContext(ctx, authCtx)
}
