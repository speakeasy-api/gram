package agentworkflows_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/agentworkflows"
	"github.com/speakeasy-api/gram/server/internal/agentworkflows/agents"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/environments"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	temporalClient client.Client
	temporalServer *testsuite.DevServer
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

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-test"), "", billingClient)
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	enc := testenv.NewEncryptionClient(t)

	// Create environment entries
	mcpMetadataRepo := mcpmetadata_repo.New(conn)
	env := environments.NewEnvironmentEntries(logger, conn, enc, mcpMetadataRepo)

	// Create cache
	cacheImpl := cache.NewRedisCacheAdapter(redisClient)

	// Create stub functions caller (Orchestrator implements ToolCaller)
	funcs := testenv.NewFunctionsTestOrchestrator(t)

	// Create auth service
	authService := auth.New(logger, conn, sessionManager)

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
	temporalClient, devserver := infra.NewTemporalClient(t)
	worker := background.NewTemporalWorker(temporalClient, logger, tracerProvider, meterProvider, &background.WorkerOptions{
		DB:               conn,
		EncryptionClient: enc,
		AgentsService:    agentsService,
	})
	t.Cleanup(func() {
		worker.Stop()
		temporalClient.Close()
		_ = devserver.Stop() // Temporal devserver may exit with status 1 during shutdown
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
		temporalClient,
		"default",
	)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		temporalClient: temporalClient,
		temporalServer: devserver,
	}
}
