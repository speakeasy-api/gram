package toolsets_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"bytes"
	"io"

	agen "github.com/speakeasy-api/gram/gen/assets"
	dgen "github.com/speakeasy-api/gram/gen/deployments"
	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/background"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/deployments"
	packages "github.com/speakeasy-api/gram/internal/packages"
	"github.com/speakeasy-api/gram/internal/testenv"
	"github.com/speakeasy-api/gram/internal/toolsets"
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
	service        *toolsets.Service
	deployments    *deployments.Service
	assets         *assets.Service
	packages       *packages.Service
	conn           *pgxpool.Pool
	temporal       client.Client
	sessionManager *sessions.Manager
	assetStorage   assets.BlobStore
}

func newTestToolsetsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	// Create a test blob store for testing
	assetStorage := assetstest.NewTestBlobStore(t)

	temporal, devserver := infra.NewTemporalClient(t)
	worker := background.NewTemporalWorker(temporal, logger, background.ForDeploymentProcessing(conn, assetStorage))
	t.Cleanup(func() {
		worker.Stop()
		temporal.Close()
		require.NoError(t, devserver.Stop(), "shutdown temporal")
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	sessionManager, err := sessions.NewUnsafeManager(logger, redisClient, cache.Suffix("gram-local"), "")
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	svc := toolsets.NewService(logger, conn, sessionManager)
	deploymentsSvc := deployments.NewService(logger, conn, temporal, sessionManager, assetStorage)
	assetsSvc := assets.NewService(logger, conn, sessionManager, assetStorage)
	packagesSvc := packages.NewService(logger, conn, sessionManager)

	return ctx, &testInstance{
		service:        svc,
		deployments:    deploymentsSvc,
		assets:         assetsSvc,
		packages:       packagesSvc,
		conn:           conn,
		temporal:       temporal,
		sessionManager: sessionManager,
		assetStorage:   assetStorage,
	}
}

func createPetstoreDeployment(t *testing.T, ctx context.Context, ti *testInstance) *dgen.CreateDeploymentResult {
	t.Helper()

	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))

	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload petstore openapi v3 asset")

	dep, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-petstore-idempotency-key",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "petstore-doc",
				Slug:    "petstore-doc",
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
	require.NoError(t, err, "create petstore deployment")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	return dep
}
