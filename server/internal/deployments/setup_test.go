package deployments_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/externalmcptest"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functionstest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	packages "github.com/speakeasy-api/gram/server/internal/packages"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, Temporal: true})
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

type testInstance struct {
	service        *deployments.Service
	feature        *feature.InMemory
	assets         *assets.Service
	packages       *packages.Service
	conn           *pgxpool.Pool
	temporalEnv    *temporal.Environment
	sessionManager *sessions.Manager
}

func newTestDeploymentService(t *testing.T, assetStorage assets.BlobStore) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	funcs := functionstest.NewOrchestrator(t, assetStorage)
	mcpRegistryClient := externalmcptest.NewRegistryClient(t, logger, tracerProvider)
	temporalEnv, _ := infra.NewTemporalEnv(t)
	auditLogger := audit.NewLogger()
	f := &feature.InMemory{}

	worker := background.NewTemporalWorker(temporalEnv, logger, tracerProvider, meterProvider, background.ForDeploymentProcessing(guardianPolicy, conn, f, assetStorage, enc, funcs, mcpRegistryClient, auditLogger))
	t.Cleanup(func() {
		worker.Stop()
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")
	authzEngine := authz.NewEngine(logger, conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	svc := deployments.NewService(logger, tracerProvider, conn, temporalEnv, sessionManager, assetStorage, posthog, testenv.DefaultSiteURL(t), mcpRegistryClient, authzEngine, auditLogger)
	assetsSvc := assets.NewService(logger, tracerProvider, guardianPolicy, conn, sessionManager, chatSessionsManager, assetStorage, "test-jwt-secret", authzEngine, auditLogger)
	packagesSvc := packages.NewService(logger, tracerProvider, conn, sessionManager, authzEngine)

	return ctx, &testInstance{
		service:        svc,
		feature:        f,
		assets:         assetsSvc,
		packages:       packagesSvc,
		conn:           conn,
		temporalEnv:    temporalEnv,
		sessionManager: sessionManager,
	}
}
