package tools_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/background"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/deployments"
	packages "github.com/speakeasy-api/gram/internal/packages"
	"github.com/speakeasy-api/gram/internal/testenv"
	"github.com/speakeasy-api/gram/internal/tools"
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
	service        *tools.Service
	deployments    *deployments.Service
	assets         *assets.Service
	packages       *packages.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
}

func newTestToolsService(t *testing.T, assetStorage assets.BlobStore) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-local"), "")
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	temporal, devserver := infra.NewTemporalClient(t)
	worker := background.NewTemporalWorker(temporal, logger, background.ForDeploymentProcessing(conn, assetStorage))
	t.Cleanup(func() {
		worker.Stop()
		temporal.Close()
		require.NoError(t, devserver.Stop(), "shutdown temporal")
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	toolsSvc := tools.NewService(logger, conn, sessionManager)
	deploymentsSvc := deployments.NewService(logger, conn, temporal, sessionManager, assetStorage)
	assetsSvc := assets.NewService(logger, conn, sessionManager, assetStorage)
	packagesSvc := packages.NewService(logger, conn, sessionManager)

	return ctx, &testInstance{
		service:        toolsSvc,
		deployments:    deploymentsSvc,
		assets:         assetsSvc,
		packages:       packagesSvc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}
