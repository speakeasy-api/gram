package deployments_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	packages "github.com/speakeasy-api/gram/server/internal/packages"
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
	service        *deployments.Service
	feature        *feature.InMemory
	assets         *assets.Service
	packages       *packages.Service
	conn           *pgxpool.Pool
	temporal       client.Client
	sessionManager *sessions.Manager
}

func newTestDeploymentService(t *testing.T, assetStorage assets.BlobStore) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	f := &feature.InMemory{}

	temporal, devserver := infra.NewTemporalClient(t)
	worker := background.NewTemporalWorker(temporal, logger, tracerProvider, meterProvider, background.ForDeploymentProcessing(conn, f, assetStorage))
	t.Cleanup(func() {
		worker.Stop()
		temporal.Close()
		require.NoError(t, devserver.Stop(), "shutdown temporal")
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-local"), "", billingClient)
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	svc := deployments.NewService(logger, tracerProvider, conn, temporal, sessionManager, assetStorage)
	assetsSvc := assets.NewService(logger, conn, sessionManager, assetStorage)
	packagesSvc := packages.NewService(logger, conn, sessionManager)

	return ctx, &testInstance{
		service:        svc,
		feature:        f,
		assets:         assetsSvc,
		packages:       packagesSvc,
		conn:           conn,
		temporal:       temporal,
		sessionManager: sessionManager,
	}
}
