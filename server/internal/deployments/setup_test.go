package deployments_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/background"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/deployments"
	packages "github.com/speakeasy-api/gram/internal/packages"
	"github.com/speakeasy-api/gram/internal/testenv"
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

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	temporal := infra.NewTemporalClient(t)
	worker := background.NewTemporalWorker(temporal, logger, background.ForDeploymentProcessing(conn, assetStorage))
	go func() {
		if err := worker.Start(); err != nil {
			panic(fmt.Sprintf("failed to start temporal worker: %v", err))
		}
	}()

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	sessionManager, err := sessions.NewUnsafeManager(logger, redisClient, cache.Suffix("gram-local"), "")
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	t.Cleanup(func() {
		worker.Stop()
		temporal.Close()
	})

	svc := deployments.NewService(logger, conn, temporal, sessionManager, assetStorage)
	assetsSvc := assets.NewService(logger, conn, sessionManager, assetStorage)
	packagesSvc := packages.NewService(logger, conn, sessionManager)

	return ctx, &testInstance{
		service:        svc,
		assets:         assetsSvc,
		packages:       packagesSvc,
		conn:           conn,
		temporal:       temporal,
		sessionManager: sessionManager,
	}
}
