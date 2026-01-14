package functions_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	dgen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
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

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *functions.Service
	deployments    *deployments.Service
	assets         *assets.Service
	conn           *pgxpool.Pool
	temporal       client.Client
	tigrisStore    *assets.TigrisStore
	assetStorage   assets.BlobStore
	sessionManager *sessions.Manager
}

func newTestFunctionsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	funcs := testenv.NewFunctionsTestOrchestrator(t)

	f := &feature.InMemory{}

	assetStorage := assetstest.NewTestBlobStore(t)
	tigrisStore := assets.NewTigrisStore(assetStorage)
	mcpRegistryClient := testenv.NewMCPRegistryClient(t, logger, tracerProvider)

	temporal, devserver := infra.NewTemporalClient(t)
	worker := background.NewTemporalWorker(temporal, logger, tracerProvider, meterProvider, background.ForDeploymentProcessing(conn, f, assetStorage, enc, funcs, mcpRegistryClient))
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

	ph := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	svc := functions.NewService(logger, tracerProvider, conn, enc, tigrisStore)
	deploymentsSvc := deployments.NewService(logger, tracerProvider, conn, temporal, sessionManager, assetStorage, ph, testenv.DefaultSiteURL(t), mcpRegistryClient)
	assetsSvc := assets.NewService(logger, conn, sessionManager, assetStorage)

	return ctx, &testInstance{
		service:        svc,
		deployments:    deploymentsSvc,
		assets:         assetsSvc,
		conn:           conn,
		temporal:       temporal,
		tigrisStore:    tigrisStore,
		assetStorage:   assetStorage,
		sessionManager: sessionManager,
	}
}

func createFunctionsDeployment(t *testing.T, ctx context.Context, ti *testInstance) *dgen.CreateDeploymentResult {
	t.Helper()
	return createFunctionsDeploymentWithKey(t, ctx, ti, "test-functions-"+t.Name())
}

func createFunctionsDeploymentWithKey(t *testing.T, ctx context.Context, ti *testInstance, idempotencyKey string) *dgen.CreateDeploymentResult {
	t.Helper()

	zipBytes, err := os.ReadFile("fixtures/valid.zip")
	require.NoError(t, err, "read functions zip fixture")

	fres, err := ti.assets.UploadFunctions(ctx, &agen.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/zip",
		ContentLength:    int64(len(zipBytes)),
	}, io.NopCloser(bytes.NewBuffer(zipBytes)))
	require.NoError(t, err, "upload functions")

	dep, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey:  idempotencyKey,
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*dgen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "test-functions",
				Slug:    "test-functions",
				Runtime: "nodejs:22",
			},
		},
		Packages:         []*dgen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create functions deployment")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	return dep
}
