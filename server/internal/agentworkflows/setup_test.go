package agentworkflows_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/access/accesstest"
	"github.com/speakeasy-api/gram/server/internal/agentworkflows"
	"github.com/speakeasy-api/gram/server/internal/agentworkflows/agents"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/environments"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	service        *agentworkflows.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	temporalEnv    *temporal.Environment
}

func newTestAgentsAPIService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)

	conn, err := infra.CloneTestDatabase(t, "agentsapitest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-test"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	enc := testenv.NewEncryptionClient(t)

	// Create environment entries
	mcpMetadataRepo := mcpmetadata_repo.New(conn)
	env := environments.NewEnvironmentEntries(logger, conn, enc, mcpMetadataRepo)

	// Create cache
	cacheImpl := cache.NewRedisCacheAdapter(redisClient)

	// Create stub functions caller (Orchestrator implements ToolCaller)
	assetStorage := assetstest.NewTestBlobStore(t)
	funcs := testenv.NewFunctionsTestOrchestrator(t, assetStorage)

	// Create auth service
	authService := auth.New(logger, conn, sessionManager, access.NewManager(logger, conn, accesstest.AlwaysEnabledFeatureChecker{}))

	// Create agents service for the background worker
	agentsService := agents.NewService(
		logger,
		tracerProvider,
		meterProvider,
		conn,
		env,
		enc,
		cacheImpl,
		nil, // guardian policy - nil is acceptable for testing
		funcs,
		nil, // openrouter provisioner - nil is acceptable for testing
		nil, // chat client - nil is acceptable for testing
	)

	// Start temporal client and worker
	temporalEnv, _ := infra.NewTemporalEnv(t)
	worker := background.NewTemporalWorker(temporalEnv, logger, tracerProvider, meterProvider, &background.WorkerOptions{
		DB:               conn,
		EncryptionClient: enc,
		AgentsService:    agentsService,
	})
	t.Cleanup(func() {
		worker.Stop()
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	svc := agentworkflows.NewService(
		logger,
		tracerProvider,
		meterProvider,
		conn,
		env,
		enc,
		cacheImpl,
		nil, // guardian policy
		funcs,
		nil, // openrouter provisioner
		nil, // chat client
		authService,
		temporalEnv,
	)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		temporalEnv:    temporalEnv,
	}
}
